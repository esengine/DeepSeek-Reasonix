package agent

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

func commitTestAgent(t *testing.T) *Agent {
	t.Helper()
	dir := testenv.TempDir(t)
	return New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), NewSession("sys"), Options{
		ContextWindow: 2000, RecentKeep: 2, ArchiveDir: dir,
		SessionPath: filepath.Join(dir, "s.jsonl"), WorkspaceID: "ws", ModelRef: "m",
	}, event.Discard)
}

func commitCanonical(n int) []provider.Message {
	msgs := make([]provider.Message, 0, n)
	msgs = append(msgs, provider.Message{Role: provider.RoleSystem, Content: "sys"})
	for i := 1; i < n; i++ {
		msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: strings.Repeat("u", 40+i)})
	}
	return msgs
}

// TestSummaryProjectionStateRecordsTheCommittedBoundary pins the ownership the
// writer now has: it stores the boundary the fold decided and hashes exactly
// that prefix. Deriving either from the transcript length is what let a planner
// and a writer hold different answers to the same question.
func TestSummaryProjectionStateRecordsTheCommittedBoundary(t *testing.T) {
	a := commitTestAgent(t)
	canonical := commitCanonical(12)
	const covered = 7

	state := a.summaryProjectionState(summaryProjectionCommit{
		canonical: canonical, covered: covered,
		projected: []provider.Message{{Role: provider.RoleSystem, Content: "digest"}},
		summary:   "digest", trigger: CompactionTriggerManual,
	})

	if got := state.Projection.CoveredCount; got != covered {
		t.Fatalf("CoveredCount = %d, want the committed %d", got, covered)
	}
	want := coveredPrefixHash(canonical, covered)
	if got := state.Projection.CoveredPrefixHash; got != want {
		t.Fatalf("CoveredPrefixHash = %q, want the hash of canonical[:%d] %q", got, covered, want)
	}
	// The half-migrated shape this guards against: the count moves to the fold
	// boundary while the hash still covers the whole transcript.
	if whole := coveredPrefixHash(canonical, len(canonical)); state.Projection.CoveredPrefixHash == whole {
		t.Fatal("CoveredPrefixHash still covers the whole canonical transcript")
	}
	if r := state.LastReceipt; r == nil {
		t.Fatal("no receipt written")
	} else if r.CoveredCount != covered || r.CoveredPrefixHash != want {
		t.Fatalf("receipt disagrees with the projection: covered=%d hash=%q", r.CoveredCount, r.CoveredPrefixHash)
	}
}

// A commit that names no boundary, or one outside the transcript, is refused
// rather than silently recorded: the writer has no answer of its own to fall
// back on any more.
func TestCommitSummaryProjectionRefusesBoundaryOutsideCanonical(t *testing.T) {
	canonical := commitCanonical(6)
	for _, covered := range []int{0, -1, len(canonical) + 1} {
		a := commitTestAgent(t)
		if _, err := a.commitSummaryProjection(summaryProjectionCommit{
			canonical: canonical, covered: covered,
			projected: []provider.Message{{Role: provider.RoleSystem, Content: "digest"}},
		}); err == nil {
			t.Fatalf("covered %d was accepted", covered)
		}
	}
}

// TestFoldedProjectionCarriesTheOldBodyRemainder pins the half of the split
// that has nowhere else to live. A boundary inside an older body leaves
// coverage where it was — those view indices name no canonical message — and
// the part of that body the new digest did not consume has to ride along, or
// the next view loses it with nothing to splice it back from.
func TestFoldedProjectionCarriesTheOldBodyRemainder(t *testing.T) {
	a := commitTestAgent(t)
	body := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "old digest"},
		{Role: provider.RoleUser, Content: "body-kept-a"},
		{Role: provider.RoleAssistant, Content: "body-kept-b"},
	}
	state := CompactionState{Projection: ContextProjection{Messages: body, CoveredCount: 19}}
	view := append(append([]provider.Message(nil), body...),
		provider.Message{Role: provider.RoleUser, Content: "live-tail"})

	const start = 2 // inside the body, past the head
	got, boundary := a.foldedProjection(state, true, view, nil, 1, start, "new digest")

	if boundary.Covered != 19 {
		t.Fatalf("Covered = %d, want the previous coverage 19", boundary.Covered)
	}
	if boundary.BodySuffixFrom != start {
		t.Fatalf("BodySuffixFrom = %d, want %d", boundary.BodySuffixFrom, start)
	}
	for _, want := range []string{"body-kept-a", "body-kept-b"} {
		if !slices.ContainsFunc(got, func(m provider.Message) bool { return m.Content == want }) {
			t.Fatalf("body remainder %q dropped: %+v", want, contentsOf(got))
		}
	}
	if slices.ContainsFunc(got, func(m provider.Message) bool { return m.Content == "live-tail" }) {
		t.Fatalf("the live tail was frozen into the body: %+v", contentsOf(got))
	}
}

func contentsOf(msgs []provider.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}
