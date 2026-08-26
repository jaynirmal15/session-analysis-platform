// Package pipeline hosts the staged processing that runs after an event is
// durably stored.
//
// The pipeline is deliberately downstream of persistence rather than inline
// with the webhook response. Correlation is the expensive, stateful, failure-
// prone part; putting it on the delivery path would couple the media backend's
// retry behaviour to the cost of a join (see internal/ingest/webhook).
//
// Stages are expected to be independently restartable and to make forward
// progress from persisted state alone. A stage that can only work from
// in-memory state cannot survive a deploy, and sessions outlive deploys.
//
// TODO(scope): stage composition, checkpointing and the runner are out of
// scope for scaffolding.
package pipeline
