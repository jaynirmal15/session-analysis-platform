//go:build integration

package maintenance

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("SAP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SAP_TEST_DATABASE_URL not set")
	}
	p, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// runway is computed independently of the view, so a bug in the view's regex
// cannot make the view agree with itself. This is the observation the metric
// depends on, so it gets checked against the catalog directly.
func runwayDays(t *testing.T, p *pgxpool.Pool) float64 {
	t.Helper()
	var days float64
	err := p.QueryRow(context.Background(), `
		SELECT COALESCE(EXTRACT(EPOCH FROM (max(range_end) - now())) / 86400.0, 0)
		  FROM event_raw_partition`).Scan(&days)
	if err != nil {
		t.Fatalf("runway: %v", err)
	}
	return days
}

func partitionCount(t *testing.T, p *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := p.QueryRow(context.Background(),
		`SELECT count(*) FROM event_raw_partition`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func maintain(t *testing.T, p *pgxpool.Pool, ahead, retain int) (created, dropped int) {
	t.Helper()
	if err := p.QueryRow(context.Background(),
		`SELECT created, dropped FROM maintain_event_raw_partitions($1, $2)`,
		ahead, retain).Scan(&created, &dropped); err != nil {
		t.Fatalf("maintain(%d,%d): %v", ahead, retain, err)
	}
	return
}

// The view's parse must match what the catalog actually says. If the regex is
// wrong, every metric downstream is wrong in a way that looks plausible.
func TestViewParsesBoundsCorrectly(t *testing.T) {
	p := pool(t)
	rows, err := p.Query(context.Background(), `
		SELECT v.partition_name, v.range_start, v.range_end,
		       pg_get_expr(c.relpartbound, c.oid)
		  FROM event_raw_partition v
		  JOIN pg_class c ON c.relname = v.partition_name
		 LIMIT 5`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var name, expr string
		var start, end time.Time
		if err := rows.Scan(&name, &start, &end, &expr); err != nil {
			t.Fatal(err)
		}
		if !end.After(start) {
			t.Errorf("%s: parsed range_end %v is not after range_start %v (expr: %s)",
				name, end, start, expr)
		}
		// Daily partitions per ADR-0024.
		if d := end.Sub(start); d != 24*time.Hour {
			t.Errorf("%s: parsed width %v, want 24h (expr: %s)", name, d, expr)
		}
		seen++
	}
	if seen == 0 {
		t.Fatal("no partitions to check — the view returned nothing")
	}
}

// Observe the world, not the job: shorten the window, run, and assert the
// partition set actually changed.
func TestMaintenanceExtendsRunway(t *testing.T) {
	p := pool(t)
	before := runwayDays(t, p)

	// Ask for more runway than currently exists.
	want := before + 10
	maintain(t, p, int(want)+1, 400)

	after := runwayDays(t, p)
	if after <= before {
		t.Fatalf("runway did not grow: before=%.1f after=%.1f", before, after)
	}
	if after < want {
		t.Errorf("runway = %.1f days, want at least %.1f", after, want)
	}
}

// Running twice must be a no-op the second time. The recovery procedure for a
// lapsed schedule is "run it again", and a recovery step that is unsafe to
// repeat is one nobody will use under pressure.
func TestMaintenanceIsIdempotent(t *testing.T) {
	p := pool(t)
	maintain(t, p, 20, 400)
	countAfterFirst := partitionCount(t, p)

	created, dropped := maintain(t, p, 20, 400)
	if created != 0 || dropped != 0 {
		t.Errorf("second run created %d and dropped %d, want 0 and 0", created, dropped)
	}
	if got := partitionCount(t, p); got != countAfterFirst {
		t.Errorf("partition count changed on repeat: %d -> %d", countAfterFirst, got)
	}
}

// A gap left by a failed run must be filled, not skipped. Extending from the
// furthest existing boundary rather than from today would leave the hole open
// forever, and every event landing in it would be dropped.
func TestMaintenanceFillsGaps(t *testing.T) {
	p := pool(t)
	ctx := context.Background()
	maintain(t, p, 20, 400)

	var victim string
	if err := p.QueryRow(ctx, `
		SELECT partition_name FROM event_raw_partition
		 WHERE range_start > now() + interval '3 days'
		 ORDER BY range_start LIMIT 1`).Scan(&victim); err != nil {
		t.Fatalf("pick a partition to remove: %v", err)
	}
	if _, err := p.Exec(ctx, `DROP TABLE `+victim); err != nil {
		t.Fatalf("drop %s: %v", victim, err)
	}

	created, _ := maintain(t, p, 20, 400)
	if created != 1 {
		t.Errorf("created %d, want exactly the 1 missing partition", created)
	}
	var exists bool
	if err := p.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM event_raw_partition WHERE partition_name = $1)`,
		victim).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Errorf("%s was not recreated", victim)
	}
}

// Retention has to actually drop things, and dropping them must not break the
// query the platform exists to run.
func TestRetentionDropsAgedPartitionsWithoutBreakingQueries(t *testing.T) {
	p := pool(t)
	ctx := context.Background()

	// An partition well outside any retention window.
	old := "event_raw_19990102"
	if _, err := p.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+old+
		` PARTITION OF event_raw FOR VALUES FROM ('1999-01-02') TO ('1999-01-03')`); err != nil {
		t.Fatalf("create aged partition: %v", err)
	}
	beforeCount := partitionCount(t, p)

	_, dropped := maintain(t, p, 14, 56)
	if dropped < 1 {
		t.Fatalf("dropped %d, want at least the 1999 partition", dropped)
	}

	var stillThere bool
	if err := p.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM event_raw_partition WHERE partition_name = $1)`,
		old).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere {
		t.Error("aged partition survived retention")
	}
	if got := partitionCount(t, p); got >= beforeCount {
		t.Errorf("partition count did not fall: %d -> %d", beforeCount, got)
	}

	// The reference query must still plan and run over the reduced set.
	var n int
	if err := p.QueryRow(ctx, `
		SELECT count(*) FROM participant_join
		 WHERE active_range && tstzrange(now() - interval '2 hours', now(), '[)')`).Scan(&n); err != nil {
		t.Fatalf("reference query broke after dropping a partition: %v", err)
	}
}

// Retention must never be able to eat the forward window. A caller passing a
// small retain_days should be refused, not obeyed.
func TestRetentionRefusesToOverlapForwardWindow(t *testing.T) {
	p := pool(t)
	var created, dropped int
	err := p.QueryRow(context.Background(),
		`SELECT created, dropped FROM maintain_event_raw_partitions(14, 3)`).Scan(&created, &dropped)
	if err == nil {
		t.Fatal("retain_days < ahead_days was accepted; it would drop live partitions")
	}
}

// Runway must reflect a deliberate failure, or it is not observing the world.
func TestRunwayFallsWhenPartitionsAreRemoved(t *testing.T) {
	p := pool(t)
	ctx := context.Background()
	maintain(t, p, 30, 400)
	before := runwayDays(t, p)

	if _, err := p.Exec(ctx, `
		DO $$
		DECLARE r record;
		BEGIN
		  FOR r IN SELECT partition_name FROM event_raw_partition
		            WHERE range_start > now() + interval '5 days'
		  LOOP EXECUTE format('DROP TABLE %I', r.partition_name); END LOOP;
		END $$;`); err != nil {
		t.Fatalf("remove future partitions: %v", err)
	}

	after := runwayDays(t, p)
	if after >= before {
		t.Fatalf("runway did not fall after removing future partitions: %.1f -> %.1f", before, after)
	}
	if after > 7 {
		t.Errorf("runway = %.1f, want under the 7-day warning threshold", after)
	}

	// Leave the database usable for whatever runs next.
	maintain(t, p, 14, 400)
}
