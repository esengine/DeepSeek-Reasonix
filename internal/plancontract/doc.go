// Package plancontract is the structured form of a proposed plan: the record a
// planner submits, the host renders, and every downstream consumer reads.
//
// The plan is data, not prose. A planner that writes markdown forces the host to
// guess at structure — which steps exist, which paths were read rather than
// inferred, whether execution should gate — and every guess is a heuristic that
// fails silently. Here those are fields.
//
// Two rules keep the type honest:
//
//   - Identity is host-assigned. ID and Revision are stamped when a plan is
//     accepted; a planner submits neither. A revision replaces its predecessor
//     under the same ID, so an approval gate can diff instead of reseeding.
//   - Evidence is not blurred. VerifiedFiles are paths the planner actually
//     read, CandidateFiles are paths it inferred. Free text cannot hold that
//     line, so a plan built on guesses cannot present them as facts.
//
// Normalize repairs what is repairable — missing IDs, dangling parents and
// dependencies, nesting past two levels — and Validate rejects what is not, so
// code downstream of an accepted Plan never re-checks its shape. Ordered is the
// single ordering both Render and ProjectTodos read, which is what makes the
// list a user approves and the task list the host seeds the same list.
package plancontract
