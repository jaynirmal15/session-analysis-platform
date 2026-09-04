// Package maintenance holds the tests for partition maintenance.
//
// There is no Go code here and there should not be. The maintenance mechanism
// is a PL/pgSQL function shipped in migration 000004 and scheduled by pg_cron
// in 000005, because partition creation is a property of the database rather
// than of any application (ADR-0029). Putting a Go implementation beside it
// would create a second mechanism that could disagree with the first.
//
// What lives here is the verification. ADR-0028 requires that an artifact
// standing in for reality be checked against reality, and that the check be
// part of the artifact — so these tests assert against the actual partition
// set in a real database, never against a record of whether the job ran. A job
// that runs nightly and creates nothing is the failure that matters, and only
// looking at the partitions catches it.
package maintenance
