#!/usr/bin/env bash
#
# Reference-query benchmark. Produces the tables cited in ADR-0004 and ADR-0024.
#
# DESTRUCTIVE: truncates and reseeds participant_join on every fraction.
# Point it at a development database only.
#
#   ./benchmarks/run.sh
#   ROWS=1000000 FRACTIONS="0.05 0.10" ./benchmarks/run.sh
#
# Environment:
#   ROWS       joins per run                     (default 200000)
#   FRACTIONS  open-join fractions to sweep      (default "0.01 0.05 0.10 0.30")
#   RUNS       timed runs per fraction           (default 9)
#   WARMUP     discarded runs before timing      (default 2)
#   SPAN_DAYS  days to spread started_at over    (default 14)
#   WINDOW_H   reference window, hours           (default 2)
#   GAP_S      reconnect gap threshold, seconds  (default 30)
#   PSQL       how to reach psql                 (default: via docker compose)

set -euo pipefail

ROWS="${ROWS:-200000}"
FRACTIONS="${FRACTIONS:-0.01 0.05 0.10 0.30}"
RUNS="${RUNS:-9}"
WARMUP="${WARMUP:-2}"
SPAN_DAYS="${SPAN_DAYS:-14}"
WINDOW_H="${WINDOW_H:-2}"
GAP_S="${GAP_S:-30}"
PSQL="${PSQL:-docker compose exec -T postgres psql -U sap -d sap}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

run_query() {  # -> execution time in ms
  { echo "EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF)"; cat "$HERE/reference_query.sql"; } \
    | $PSQL -tA -v window_hours="$WINDOW_H" -v gap_seconds="$GAP_S" 2>/dev/null \
    | grep -oE 'Execution Time: [0-9.]+' | grep -oE '[0-9.]+'
}

echo "reference-query benchmark"
echo "  rows=$ROWS  window=${WINDOW_H}h  gap=${GAP_S}s  span=${SPAN_DAYS}d  runs=$RUNS (+$WARMUP warmup)"
echo "  DESTRUCTIVE: participant_join is truncated and reseeded per fraction"
echo

printf '| open %% | matched | of which open | of which closed | median | min–max |\n'
printf '|---|---|---|---|---|---|\n'

for frac in $FRACTIONS; do
  $PSQL -q -v rows="$ROWS" -v frac="$frac" -v span="$SPAN_DAYS" \
        -f - < "$HERE/seed.sql" >/dev/null 2>&1

  # Sanity gate. A collapsed seed is the failure mode this whole directory
  # exists to prevent, so refuse to report timings taken on one.
  read -r distinct_pct window_closed <<<"$(
    $PSQL -tAF' ' -c "
      SELECT round(100.0*count(DISTINCT started_at)/count(*),2),
             (SELECT count(*) FROM participant_join
               WHERE active_range && tstzrange(now() - interval '$WINDOW_H hours', now(), '[)')
                 AND ended_at IS NOT NULL)
      FROM participant_join;" 2>/dev/null)"

  # Empty results default to 0 so a zero-row seed trips the gate rather than
  # crashing the shell on an empty comparison.
  distinct_pct="${distinct_pct:-0}"; window_closed="${window_closed:-0}"

  if [ "${distinct_pct%%.*}" -lt 90 ] || [ "$window_closed" -eq 0 ]; then
    echo "| $frac | SEED CHECK FAILED: distinct_started_at=${distinct_pct}%, window_closed=${window_closed} |"
    echo
    echo "Refusing to report timings. See the comment block in benchmarks/seed.sql." >&2
    exit 1
  fi

  read -r matched open closed <<<"$(
    $PSQL -tAF' ' -c "
      SELECT count(*),
             count(*) FILTER (WHERE ended_at IS NULL),
             count(*) FILTER (WHERE ended_at IS NOT NULL)
      FROM participant_join
      WHERE active_range && tstzrange(now() - interval '$WINDOW_H hours', now(), '[)');" 2>/dev/null)"

  for _ in $(seq 1 "$WARMUP"); do run_query >/dev/null; done

  times=()
  for _ in $(seq 1 "$RUNS"); do times+=("$(run_query)"); done

  sorted=$(printf '%s\n' "${times[@]}" | sort -n)
  median=$(echo "$sorted" | awk '{a[NR]=$1} END{printf "%.0f", a[int((NR+1)/2)]}')
  mn=$(echo "$sorted" | head -1 | awk '{printf "%.0f",$1}')
  mx=$(echo "$sorted" | tail -1 | awk '{printf "%.0f",$1}')

  pct=$(awk -v f="$frac" 'BEGIN{printf "%g", f*100}')
  printf '| %s%% | %s | %s | %s | %s ms | %s–%s |\n' \
    "$pct" "$matched" "$open" "$closed" "$median" "$mn" "$mx"
done

echo
echo "participant_join still holds the last seeded dataset. 'make migrate-reset &&"
echo "make migrate-up' clears it."
