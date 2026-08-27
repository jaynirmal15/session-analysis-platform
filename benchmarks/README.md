# Benchmarks

The measurements cited in
[ADR-0004](../ARCHITECTURE.md#adr-0004--plain-partitioned-postgresql-not-timescaledb)
and [ADR-0024](../ARCHITECTURE.md#adr-0024--the-event-schema) come from here.
This directory exists so those numbers are reproducible rather than asserted.

```bash
make bench                                   # the sweep in the ADRs
make bench-verify                            # sanity-check the current dataset
ROWS=1000000 FRACTIONS="0.05 0.10" make bench
```

**Destructive.** Every run truncates and reseeds `participant_join`. Development
databases only. To clean up: `make migrate-reset && make migrate-up`.

| File | What it is |
|---|---|
| `seed.sql` | synthetic joins, parameterised by row count, open fraction and time span |
| `verify_seed.sql` | the sanity check that catches a collapsed seed |
| `reference_query.sql` | overlap + `lag()` + gap threshold — the query the trigger is stated in terms of |
| `run.sh` | warm-cache runner, median-of-N, emits a markdown table |

## Why this directory has a lesson attached

The first version of this benchmark produced a baseline that survived into an
ADR before anyone noticed it was meaningless.

The seed used a `LATERAL` subquery that never referenced the row it was supposed
to vary with, so PostgreSQL evaluated it **once**. All 200,000 rows got an
identical `started_at`. The timings measured GiST behaviour over 200,000
identical ranges — a workload this platform will never have — and the number
went into ADR-0004 as a baseline and into ADR-0024 as an index justification.

The mechanical fix is a one-line change, documented in `seed.sql`. The useful
part is why it went unchallenged for a whole commit.

### The tell

The original run reported **exactly 10,000 matched rows** for a 2-hour window
over 14 days of data.

The open-join count was **also exactly 10,000** — it had been seeded as
`i % 20 = 0` over 200,000 rows.

Two independent quantities in a system driven by `random()` came out as the same
round number, and neither of us stopped. The correct reaction to `10000` next to
`10000` is not "nice, the index is working" — it is "why is a random process
producing a round number, and why does it equal something else I already know?".

Working through it takes about a minute: if 200,000 joins are spread over 14
days, a 2-hour window should catch roughly `200000 × 2/336 ≈ 1,190` closed
joins, plus the open ones. The report showed **zero** closed joins. There was no
distribution to catch, because there was only one timestamp.

### The rule this directory tries to enforce

**A benchmark result that is suspiciously round, or that exactly equals another
quantity in the system, is a defect report until proven otherwise.**

Round numbers are what constants look like. Randomised processes produce untidy
ones. When a measurement of a random population comes back tidy, something has
collapsed a distribution somewhere — and the most likely candidate is the
harness, not the system under test.

Two habits follow, and both are cheap:

1. **Predict the result before reading it.** Not precisely — an order of
   magnitude and a rough shape is enough. The prediction here (~1,190 closed
   joins) would have failed loudly against the observed zero. A measurement you
   had no expectation for cannot surprise you, which means it cannot inform you
   either.
2. **Check the population, not just the aggregate.** `count(DISTINCT
   started_at)` costs nothing and would have shown `1` immediately.
   `verify_seed.sql` is exactly this check, and `run.sh` now refuses to report
   timings when the seed fails it — a benchmark that can silently measure
   nothing is worse than one that errors.

### What is guarded now

`run.sh` aborts before timing anything if fewer than 90% of `started_at` values
are distinct, or if the reference window matches zero closed joins. Both
conditions were true of the original run. Neither was checked.

This does not make the harness correct — it makes one known failure loud. Other
collapses are certainly possible, which is why habit 1 above matters more than
the guard.

## Caveats on the numbers

- **Synthetic and uniform.** Real traffic is bursty, rooms are not uniformly
  sized, and identities are not evenly distributed. These numbers establish
  scaling shape, not production capacity.
- **Timed inside `EXPLAIN ANALYZE`** with `TIMING OFF`, so per-node
  instrumentation overhead is excluded but statement overhead is not.
- **Warm cache**, two discarded runs. Cold-cache behaviour on a table larger
  than shared buffers is a different measurement and is not made here.
- **A single container** sharing a laptop with the rest of the stack. Absolute
  values are not portable; the ratios between fractions are the point.
