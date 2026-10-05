// Package hostaudit holds the structured audit records the host emits beside a
// run — readiness verdicts, per-round outcome samples, delegation audits — so
// events, trajectories and metrics sinks can carry them without depending on
// the evidence ledger that computes them.
package hostaudit
