package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Options tune a pool for the path that owns it. The write path wants a small
// pool with short statement timeouts and high insert concurrency; the read path
// wants a larger pool tolerant of long scans over old partitions. Keeping the
// knobs here, rather than a single shared default, is what makes that split
// real rather than aspirational.
type Options struct {
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	ConnectTimeout    time.Duration
	StatementTimeout  time.Duration
	HealthCheckPeriod time.Duration
}

// IngestOptions are the write path's defaults: few connections, short
// statement timeout. An ingest statement that takes longer than a second is
// not slow, it is wrong — the receiver's whole job is two indexed writes.
func IngestOptions() Options {
	return Options{
		MaxConns:          8,
		MinConns:          2,
		MaxConnLifetime:   30 * time.Minute,
		MaxConnIdleTime:   5 * time.Minute,
		ConnectTimeout:    5 * time.Second,
		StatementTimeout:  1 * time.Second,
		HealthCheckPeriod: 30 * time.Second,
	}
}

// Open builds a connection pool and verifies it can reach the database.
//
// It pings before returning: a pool that has never connected is
// indistinguishable from a healthy one until the first request, and finding out
// at startup is cheaper than finding out from a webhook.
func Open(ctx context.Context, url string, opts Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database: parse connection string: %w", err)
	}

	cfg.MaxConns = opts.MaxConns
	cfg.MinConns = opts.MinConns
	cfg.MaxConnLifetime = opts.MaxConnLifetime
	cfg.MaxConnIdleTime = opts.MaxConnIdleTime
	cfg.HealthCheckPeriod = opts.HealthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = opts.ConnectTimeout

	if opts.StatementTimeout > 0 {
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		cfg.ConnConfig.RuntimeParams["statement_timeout"] =
			fmt.Sprintf("%d", opts.StatementTimeout.Milliseconds())
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}
