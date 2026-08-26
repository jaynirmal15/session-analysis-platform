// Package postgres implements the write path's persistence port against a
// time-partitioned PostgreSQL database.
//
// "Time-partitioned" is doing the load-bearing work here. Event volume for a
// media platform is dominated by recent data, and retention is a business
// decision measured in weeks; native declarative partitioning by event time
// makes retention a DETACH PARTITION instead of a DELETE that fights autovacuum
// for a day.
//
// The costs are accepted knowingly (ADR-0004): partitions must be created ahead
// of time or inserts fail, every query that omits the partition key scans every
// partition, and there is no continuous aggregate to fall back on. Finding out
// exactly where that hurts is the point of choosing plain PostgreSQL over
// TimescaleDB.
//
// TODO(scope): DDL, partition management and queries are out of scope. See
// migrations/README.md.
package postgres
