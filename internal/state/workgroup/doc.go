// Package workgroup folds a durable trajectory into tool calls and then into
// runs of one producer's work. It exists for the natural-value assay: whether
// a turn's execution is worth offering as one foldable unit is a question
// about real use, and answering it needs one partition that both the assay and
// the semantics tests read. A second copy of these rules would drift.
//
// # Call lifecycles
//
// Three shapes reach the fold, and only the first two are calls:
//
//   - settled — a full dispatch and a result.
//   - result-only — the provider ran it on its own side and reported the
//     result. Complete as it stands; there is no dispatch to wait for, and
//     inventing one to make the shapes uniform would be a fact nobody sent.
//   - partial-only terminal — a streaming dispatch whose run ended before its
//     arguments did. An exit tool is the ordinary case: calling it is what
//     ends the round, so the full dispatch it was promised never arrives.
//
// The third is not a call. Partial=true is a transport statement that
// arguments are still arriving; it is not a promise that a durable completion
// frame follows. Twelve real turns broke the partition on this: an exit tool's
// partial stayed open for the rest of the record, and every later boundary
// sealed a group over an invocation still in flight.
//
// # Invariants
//
// A partition is legal only where both hold. DuplicateMembership: one call id
// belongs to at most one group — atomizing before grouping is what makes this
// unreachable. SealedOverAnOpenCall: no group closes while a member is still
// running, or that member's result belongs to no group at all.
package workgroup
