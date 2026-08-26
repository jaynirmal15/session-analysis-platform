// Package database owns PostgreSQL connection construction and lifecycle.
//
// This is one of the three packages both the write path and the read path may
// import (ADR-0009). Its scope is deliberately narrow: build a pool, apply
// timeouts, expose a health check, instrument the driver. It holds no queries
// and no knowledge of any table.
//
// Splitting connection construction from query code is what lets the write and
// read paths use the same database with different pool settings — the ingester
// wants a small pool with short statement timeouts and high write concurrency;
// the query API wants a larger pool tolerant of long analytical scans against
// old partitions.
//
// TODO(scope): pool construction lands here once there is a query to run. The
// schema now exists (ADR-0024), but correlation and the query API do not, so
// there is still nothing to execute — and a driver dependency added before it
// is used is a dependency chosen without evidence. Migrations are applied by
// golang-migrate as a container (ADR-0023), which needs no Go driver.
package database
