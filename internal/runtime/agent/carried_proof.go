package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"reasonix/internal/safety/evidence"
)

// absentPath is the fingerprint of a path that does not exist, which is itself
// a state a change can leave behind.
const absentPath = "absent"

// carryProof writes what outlives this process into the checkpoint: the review
// still owed, and the proof a pending change has earned. A settled change
// carries no proof, since nothing is left for it to answer for.
func (a *Agent) carryProof(cp *evidence.DeliveryCheckpoint) {
	cp.OwedReview = a.owedReview(cp.OwedReview)
	if _, live := a.task.ledger.LatestSuccessfulMutationIndex(); live {
		cp.Proven = a.provenMutation()
	}
	if !cp.PendingMutation {
		cp.Proven = nil
	}
}

// provenMutation is what the turn's last readiness verdict had proven, with the
// content of every path the scope changed. A change the host cannot name every
// path of carries no proof: its unnamed part could not be checked on resume.
func (a *Agent) provenMutation() *evidence.ProvenMutation {
	check := a.turn.lastReadiness
	if check == nil || check.missingMutation > 0 {
		return nil
	}
	for _, r := range a.task.ledger.Receipts() {
		if r.Success && r.Mutation && len(r.Paths) == 0 {
			return nil
		}
	}
	paths := a.task.ledger.PathsSince(0)
	if len(paths) == 0 {
		return nil
	}
	proven := &evidence.ProvenMutation{
		Paths:         make(map[string]string, len(paths)),
		Verified:      check.missingVerification == 0,
		SignedOff:     check.missingSignoff == 0,
		Inspected:     check.missingPathInspection == 0,
		ProjectChecks: check.missingProjectChecks == 0,
	}
	for _, p := range paths {
		fp := a.pathFingerprint(p)
		if fp == "" {
			return nil
		}
		proven.Paths[p] = fp
	}
	return proven
}

// carriedProof is the checkpoint's proof if every path it covers still holds
// the content it was proven with. A path that moved while no process was
// watching voids all of it: the host cannot say which proof that edit broke.
func (a *Agent) carriedProof() *evidence.ProvenMutation {
	proven := a.task.checkpoint.Proven
	if proven == nil {
		return nil
	}
	for p, want := range proven.Paths {
		if got := a.pathFingerprint(p); got == "" || got != want {
			return nil
		}
	}
	return proven
}

// pathFingerprint is a path's content hash, absentPath for a missing one, and
// "" when it cannot be read — which never matches anything.
func (a *Agent) pathFingerprint(p string) string {
	if !filepath.IsAbs(p) && a.writeWorkspaceRoot != "" {
		p = filepath.Join(a.writeWorkspaceRoot, p)
	}
	b, err := os.ReadFile(p)
	switch {
	case os.IsNotExist(err):
		return absentPath
	case err != nil:
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
