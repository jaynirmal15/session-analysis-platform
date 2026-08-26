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
// TODO(scope): pool construction lands here once the schema exists. Deferred
// deliberately — there is nothing to connect to until migrations are designed
// (ADR-0014), and a driver dependency added before it is used is a dependency
// chosen without evidence.
package database
