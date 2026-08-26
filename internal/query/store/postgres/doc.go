// Package postgres implements the read path's persistence port.
//
// This is where the ceiling of plain PostgreSQL (ADR-0004) will show up first
// and most visibly. Aggregate queries across many partitions have no continuous
// aggregate to lean on, so they will either be answered by rollup tables this
// package maintains, or slowly. Which of those happens, and at what event
// volume, is a question this repository intends to answer with numbers rather
// than assert.
//
// TODO(scope): queries are out of scope pending the schema.
package postgres
