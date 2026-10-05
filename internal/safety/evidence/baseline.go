// The turn's mutation baseline: which change the host measures the remaining
// obligations from. Kept apart from the ledger's recording so the question
// "what still counts as changed" has one place to be answered.
package evidence

import (
	"path/filepath"
	"slices"
	"strings"
)

// CreatedInTurn reports whether this turn is what brought path into existence.
// It is what separates a cleanup from a deletion: removing a file the turn made
// leaves the workspace as it was found, removing one it did not is the change.
func (l *Ledger) CreatedInTurn(path string) bool {
	if l == nil {
		return false
	}
	// Created is stored under the ledger's path identity, so the question has to
	// be asked in it as well.
	want := normalizePath(path)
	if want == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && slices.Contains(r.Created, want) {
			return true
		}
	}
	return false
}

func (l *Ledger) LatestSuccessfulWriterIndex() (int, bool) {
	return l.LatestSuccessfulWriterIndexFunc(nil)
}

// LatestSuccessfulWriterIndexFunc is LatestSuccessfulWriterIndex with the
// caller deciding which writes still count: a scratch file written, used and
// removed left nothing to verify, and only the host — which can look at the
// disk — knows that. nil keeps every write. keep runs outside the ledger's
// lock, since the caller deciding may ask the ledger something itself.
func (l *Ledger) LatestSuccessfulWriterIndexFunc(keep func(Receipt) bool) (int, bool) {
	if l == nil {
		return 0, false
	}
	latest := -1

	for i, r := range l.snapshotReceipts() {
		if r.Success && r.Write && (keep == nil || keep(r)) {
			latest = i
		}
	}
	return latest, latest >= 0
}

// LatestSuccessfulMutationIndex returns the most recent host-observed
// state-changing call. It includes known file writers, writer-capable delegated
// or external tools, and bash commands that are not demonstrably observational
// or verification-only.
func (l *Ledger) LatestSuccessfulMutationIndex() (int, bool) {
	if l == nil {
		return 0, false
	}
	latest := -1
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, r := range l.receipts {
		if r.Success && r.Mutation {
			latest = i
		}
	}
	return latest, latest >= 0
}

// LatestProvenMutationIndex is the baseline for what a change owes: the latest
// write the host could prove, or — when it proved none — the latest it could
// not classify. A check that merely resists classification must not become the
// change set, or every post-verification `gofmt -l` moves the goalposts past
// the verification that just ran.
func (l *Ledger) LatestProvenMutationIndex() (int, bool) {
	return l.LatestProvenMutationIndexFunc(nil)
}

// LatestProvenMutationIndexFunc is LatestProvenMutationIndex with the same
// caller veto LatestSuccessfulWriterIndexFunc takes, for the same reason.
// Unproven mutations are never vetoed: the host does not know what they
// touched, so it cannot know they left nothing behind. keep runs outside the
// ledger's lock, as in LatestSuccessfulWriterIndexFunc.
func (l *Ledger) LatestProvenMutationIndexFunc(keep func(Receipt) bool) (int, bool) {
	if l == nil {
		return 0, false
	}
	proven, unproven := -1, -1
	for i, r := range l.snapshotReceipts() {
		if !r.Success || !r.Mutation {
			continue
		}
		if r.MutationEvidence == MutationProven {
			if keep == nil || keep(r) {
				proven = i
			}
			continue
		}
		unproven = i
	}
	if proven >= 0 {
		return proven, true
	}
	return unproven, unproven >= 0
}

// LatestUnprovenMutationIndex returns the most recent successful mutation whose
// scope the host could not establish. Such a change invalidates every earlier
// check without saying which, so the debt it leaves is settled by a check that
// ran after it, never by one that ran before.
func (l *Ledger) LatestUnprovenMutationIndex() (int, bool) {
	if l == nil {
		return 0, false
	}
	latest := -1
	for i, r := range l.snapshotReceipts() {
		if r.Success && r.Mutation && r.MutationEvidence == MutationUnknown {
			latest = i
		}
	}
	return latest, latest >= 0
}

// snapshotReceipts copies the ledger so a caller's predicate can run without
// the lock — and so it can ask the ledger further questions while deciding.
func (l *Ledger) snapshotReceipts() []Receipt {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Receipt(nil), l.receipts...)
}

// UnreviewedMutationBaseline answers what the change set is; how fresh a review
// of it must be is still the latest mutation's to say. One index answering both
// let a later harmless write shrink the set an earlier sensitive one put in
// scope. It is the widest of the per-kind windows, since risk — read from this
// set — is itself what decides which kinds are owed.
func (l *Ledger) UnreviewedMutationBaseline() (int, bool) {
	if l == nil {
		return 0, false
	}
	receipts := l.snapshotReceipts()
	widest, found := 0, false
	for _, kind := range ReviewKinds() {
		at, ok := firstMutationAfter(receipts, latestAcceptedReview(receipts, kind))
		if !ok {
			continue
		}
		if !found || at < widest {
			widest, found = at, true
		}
	}
	return widest, found
}

// latestAcceptedReview returns the index of the last review of this kind the
// host accepted, or -1. Which obligation a report closed is the host's grant to
// say, not the report's own kind. A blocking one is not accepted either: it
// leaves the change set exactly where it was.
func latestAcceptedReview(receipts []Receipt, kind ReviewKind) int {
	accepted := -1
	for i, r := range receipts {
		if !ReceiptProves(r, kind) {
			continue
		}
		report, err := ParseReviewReport(r.Args)
		if err != nil || report.HasBlockingFinding() {
			continue
		}
		accepted = i
	}
	return accepted
}

func firstMutationAfter(receipts []Receipt, after int) (int, bool) {
	for i := max(after+1, 0); i < len(receipts); i++ {
		if receipts[i].Success && receipts[i].Mutation {
			return i, true
		}
	}
	return 0, false
}

// UninspectedWritePaths returns the paths this turn wrote and never looked at
// again. Freshness is per path — a read counts when it came after that path's
// own last write — so a turn that edits and inspects file by file is not sent
// back to re-read four of them because it went on to touch a fifth.
func (l *Ledger) UninspectedWritePaths(paths []string) []string {
	if l == nil || len(paths) == 0 {
		return nil
	}
	receipts := l.snapshotReceipts()
	var out []string
	for _, path := range normalizePaths(paths) {
		needle := strings.ToLower(filepath.ToSlash(path))
		if needle == "" {
			continue
		}
		written := -1
		for i, r := range receipts {
			if r.Success && (r.Write || r.Mutation) && pathsAnswerFor(r.Paths, needle) {
				written = i
			}
		}
		if written < 0 {
			continue
		}
		seen := false
		for i := written + 1; i < len(receipts) && !seen; i++ {
			r := receipts[i]
			if !r.Success {
				continue
			}
			if (r.Read && pathsAnswerFor(r.Paths, needle)) || pathsAnswerFor(r.Showed, needle) {
				seen = true
			}
		}
		if !seen {
			out = append(out, path)
		}
	}
	return out
}
