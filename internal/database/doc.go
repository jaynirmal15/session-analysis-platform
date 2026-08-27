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
// The driver is pgx/v5. It arrived with the webhook receiver, which is the
// first code with a query to run — ADR-0014 held it back until then on the
// grounds that a driver chosen before it is used is a driver chosen without
// evidence. Migrations remain golang-migrate as a container (ADR-0023) and do
// not depend on this package.
//
// TODO(scope): read-path options. Only IngestOptions exists, because only the
// write path has a caller.
package database
