package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/safety/evidence"
)

// absentPath is the fingerprint of a path that does not exist, which is itself
// a state a change can leave behind.
const absentPath = "file-state-v1:absent"

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
// the file state it was proven with. A path that moved while no process was
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

// The version prefix refuses legacy byte-only proof in both reader directions.
// Links and other non-regular files cannot carry proof of their referents.
func (a *Agent) pathFingerprint(p string) string {
	if !filepath.IsAbs(p) && a.writeWorkspaceRoot != "" {
		p = filepath.Join(a.writeWorkspaceRoot, p)
	}
	before, err := os.Lstat(p)
	switch {
	case os.IsNotExist(err):
		return absentPath
	case err != nil:
		return ""
	}
	if !before.Mode().IsRegular() {
		return ""
	}
	version, err := fileutil.ReadContentVersion(context.Background(), p)
	if err != nil || version.Kind != "file" || version.Mode != uint32(before.Mode().Perm()) {
		return ""
	}
	after, err := os.Lstat(p)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() ||
		before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return ""
	}
	return fmt.Sprintf("file-state-v1:%08x:%s", uint32(before.Mode()), version.Digest)
}
