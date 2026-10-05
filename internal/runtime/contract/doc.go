// Package contract is the canonical Task Contract (docs/design/TRUSTED_EXECUTION.md
// §3.1): a host-owned record with an identity, immutable revisions chained by
// digest, and criteria whose verifiers are frozen when the revision is
// accepted. Nothing read from the workspace later changes what an accepted
// criterion demands (C2).
//
// Criteria come only from sources the host already owns: the check commands a
// task began requiring, and the test criteria captured as host-owned bytes.
// Host policy accepts a revision only when it keeps every criterion of the
// last one unchanged and adds to them (A3); a derivation that would drop or
// alter one is refused, because relaxing a contract is the User's decision.
package contract
