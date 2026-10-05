package agent

import (
	"fmt"
	"reasonix/internal/state/sessionstore"
	"runtime"
	"slices"
	"testing"

	"reasonix/internal/contract/provider"
)

// The turn path answers from a memo when it can and from the canonical
// transcript when it cannot. Two answers to one question is a bug waiting for
// the state where they disagree, so the states are enumerated here: whenever
// the fast path claims an answer, it must be the answer the full scan gives.
func TestFastPathNeverDisagreesWithFullScan(t *testing.T) {
	cases := []struct {
		name  string
		setUp func(a *Agent)
		// Whether the fast path is expected to carry this state at all. The
		// ones it declines still have to decline rather than answer wrongly.
		fast bool
	}{
		{name: "folded and quiet", fast: true},
		{
			name: "one turn appended behind the fold",
			setUp: func(a *Agent) {
				a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: "and one more"})
			},
			fast: true,
		},
		{
			name: "transcript rewritten under the projection",
			setUp: func(a *Agent) {
				msgs, _ := a.sess.conversation.SnapshotMessagesVersion()
				msgs[1].Content = "something else entirely"
				a.sess.conversation.Rewrite(msgs, "test")
			},
		},
		{
			name: "projection built for another model",
			setUp: func(a *Agent) {
				a.sess.win.compactionState.PromptCacheKey = "other|other|other"
			},
		},
		{
			name:  "legacy sidecar with no covered hash",
			setUp: func(a *Agent) { a.sess.win.compactionState.Projection.CoveredPrefixHash = "" },
		},
		{
			name:  "covered count past the end of the transcript",
			setUp: func(a *Agent) { a.sess.win.compactionState.Projection.CoveredCount = 1 << 20 },
		},
		{
			name:  "no projection at all",
			setUp: func(a *Agent) { a.sess.win.compactionState = sessionstore.CompactionState{} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := longSession(30)
			if tc.setUp != nil {
				tc.setUp(a)
			}
			// One full pass first, exactly as a real turn would take it: the
			// memo is a warm cache, never a precondition the caller arranges.
			want := a.window().visibleByFullScan()

			got, ok := a.window().visibleBehindMemoisedFold()
			if ok != tc.fast {
				t.Fatalf("fast path carried=%v, want %v", ok, tc.fast)
			}
			if !ok {
				return
			}
			if !slices.EqualFunc(got, want, sameMessage) {
				t.Fatalf("fast path disagrees:\n fast: %s\n scan: %s", digest(got), digest(want))
			}
		})
	}
}

// A fold exists so a long session stops paying for history the model never
// sees, so what one turn allocates must not track how long the session is. It
// did: 14MB per turn on a 20k-message transcript, to read four messages off the
// end. Measured, because the defect is invisible at unit-test sizes.
func TestTurnCostDoesNotTrackTranscriptLength(t *testing.T) {
	short := perTurnBytes(t, longSession(200))
	long := perTurnBytes(t, longSession(20_000))
	if short == 0 {
		t.Fatal("measured nothing")
	}
	// The transcript grows 100×; the visible tail does not grow at all, so the
	// bound is generous on purpose and still an order of magnitude below a
	// per-turn copy of the canonical slice.
	if long > short*4 {
		t.Fatalf("a turn pays for the transcript: %d B at 200 turns, %d B at 20000", short, long)
	}
}

func perTurnBytes(t *testing.T, a *Agent) uint64 {
	t.Helper()
	a.window().modelVisibleMessages() // warm the memo, as the previous turn would have
	const runs = 50
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range runs {
		if got := a.window().modelVisibleMessages(); len(got) == 0 {
			t.Fatal("no visible messages")
		}
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / runs
}

// Compared on what reaches a provider, which is what the two paths owe each
// other; unexported bookkeeping is free to differ.
func sameMessage(a, b provider.Message) bool {
	return a.Role == b.Role && a.Content == b.Content && slices.Equal(a.Images, b.Images)
}

func digest(msgs []provider.Message) string {
	out := fmt.Sprintf("%d msgs", len(msgs))
	for i, m := range msgs {
		if i == 3 {
			return out + " …"
		}
		content := m.Content
		if len(content) > 24 {
			content = content[:24] + "…"
		}
		out += fmt.Sprintf(" | %s:%s", m.Role, content)
	}
	return out
}
