package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// Capacity and economics answer different questions: how close the prompt is to
// not fitting, and how much it costs to replay. A declared 1M window makes a
// 300K prompt legal without making it economical, so the boundary that decides
// maintenance is whichever one is reached first.
func TestMaintenanceBoundaryIsTheNearerOfCapacityAndEconomics(t *testing.T) {
	for _, tc := range []struct {
		name     string
		window   int
		ratio    float64
		soft     int
		trigger  int
		boundary string
	}{
		{"million-window folds on economics", 1_000_000, 0.85, 0, defaultContextSoftLimitTokens, "economic"},
		{"small window keeps capacity first", 128_000, 0.85, 0, 108_800, "capacity"},
		{"configured soft limit wins when lower", 1_000_000, 0.85, 96_000, 96_000, "economic"},
		{"configured soft limit loses when higher", 200_000, 0.85, 400_000, 170_000, "capacity"},
		{"negative soft limit disables economics", 1_000_000, 0.85, -1, 850_000, "capacity"},
		{"no declared window leaves both unset", 0, 0.85, 0, 0, ""},
		// A ratio past the window is how a user turns automatic maintenance off.
		{"ratio past the window stays off", 1_000_000, 2, 0, 2_000_000, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New(nil, tool.NewRegistry(), &Session{}, Options{
				ContextWindow:     tc.window,
				CompactRatio:      tc.ratio,
				CompactionBudgets: CompactionBudgets{ContextSoftLimitTokens: tc.soft},
			}, event.Discard)
			if got := a.compactTrigger(); got != tc.trigger {
				t.Errorf("compactTrigger = %d, want %d", got, tc.trigger)
			}
			if got := a.compactBoundary(); got != tc.boundary {
				t.Errorf("compactBoundary = %q, want %q", got, tc.boundary)
			}
		})
	}
}

// economicFixture is a session whose visible input sits far above the economic
// boundary and far below the capacity one, which is exactly the shape that ran
// for 2150 rounds at 321K tokens inside a declared 1M window.
const economicActiveTurnAt int64 = 1_700_000_000_000

func economicFixture(t *testing.T, soft int, activeTurnRound int) (*Agent, *recordSink) {
	t.Helper()
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "long-running task"},
	}}
	body := strings.Repeat("x", 24*1024)
	for i := range 40 {
		turn := provider.Message{Role: provider.RoleUser, Content: body}
		if i == activeTurnRound {
			turn.CreatedAt = economicActiveTurnAt
		}
		sess.Messages = append(sess.Messages,
			provider.Message{Role: provider.RoleAssistant, Content: "step " + string(rune('a'+i%26))},
			turn)
	}
	sink := &recordSink{}
	a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess, Options{
		ContextWindow:     1_000_000,
		CompactRatio:      0.85,
		RecentKeep:        2,
		ArchiveDir:        testenv.TempDir(t),
		CompactionBudgets: CompactionBudgets{ContextSoftLimitTokens: soft},
	}, sink)
	return a, sink
}

// A prompt above the economic boundary must be maintained even though it is
// nowhere near the window share, and one below it must not be.
func TestEconomicTriggerFoldsBelowCapacityShare(t *testing.T) {
	a, sink := economicFixture(t, 0, -1)
	visible := a.estimatedVisibleRequestTokens(a.modelVisibleMessages())
	if visible <= a.economicCompactTrigger() || visible >= a.capacityCompactTrigger() {
		t.Fatalf("fixture input %d is not between the economic (%d) and capacity (%d) boundaries",
			visible, a.economicCompactTrigger(), a.capacityCompactTrigger())
	}
	if _, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
		t.Fatal(err)
	}
	if got := a.currentProjectionVersion(); got == 0 {
		t.Fatal("input above the economic boundary installed no projection")
	}
	if got := appliedMaintenanceEvents(sink); got != 1 {
		t.Fatalf("applied maintenance events = %d, want 1", got)
	}
}

func TestInputBelowBothBoundariesIsNotMaintained(t *testing.T) {
	a, sink := economicFixture(t, 4_000_000, -1)
	if _, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
		t.Fatal(err)
	}
	if got := a.currentProjectionVersion(); got != 0 {
		t.Fatalf("input below both boundaries installed projection version %d", got)
	}
	if got := appliedMaintenanceEvents(sink); got != 0 {
		t.Fatalf("applied maintenance events = %d, want 0", got)
	}
}

// growTurn appends closed tool transactions to the running turn — call and
// result, paired — which is what a tool loop produces between two Prepare
// calls and what a fold inside the turn is allowed to reach.
func growTurn(a *Agent, rounds int) {
	body := strings.Repeat("x", 24*1024)
	for range rounds {
		id := fmt.Sprintf("call-%d", a.hostAdvanceSeq.Add(1))
		a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: `{}`}}})
		a.sess.conversation.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: body})
	}
}

// openTransaction appends a call the turn is still waiting on.
func openTransaction(a *Agent, calls int) {
	body := strings.Repeat("x", 24*1024)
	pending := make([]provider.ToolCall, 0, calls)
	for i := range calls {
		pending = append(pending, provider.ToolCall{ID: fmt.Sprintf("open-%d", i), Name: "read_file",
			Arguments: `{"path":"` + body + `"}`})
	}
	a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: pending})
}

func maintenanceCodes(sink *recordSink, status string) []string {
	var codes []string
	for _, got := range sink.kinds(event.ContextMaintenanceEvent) {
		if got.Maintenance != nil && got.Maintenance.Status == status {
			codes = append(codes, got.Maintenance.Code)
		}
	}
	return codes
}

// A turn long enough to reach the boundary more than once must fold more than
// once. What the checkpoint may reach is the turn's closed transactions; what
// it may never reach is the request that began the turn or a call still in
// flight. Everything below is one turn: the 15.6-hour session that motivated
// this ran 2150 rounds inside a single one.
func TestRepeatedMaintenanceInsideOneTurn(t *testing.T) {
	a, sink := economicFixture(t, 0, 30)
	a.activeTurnCreatedAt.Store(economicActiveTurnAt)
	ctx := context.Background()
	canonicalBefore := len(a.sess.conversation.Snapshot())

	var versions []uint64
	var covered []int
	for round := range 3 {
		if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
			t.Fatalf("round %d: %v", round+1, err)
		}
		version := a.currentProjectionVersion()
		if version == 0 {
			t.Fatalf("round %d installed no projection; codes so far = %v", round+1, maintenanceCodes(sink, "noop"))
		}
		versions = append(versions, version)
		a.sess.compactionMu.Lock()
		covered = append(covered, a.sess.compactionState.Projection.CoveredCount)
		a.sess.compactionMu.Unlock()
		if visible := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()); visible >= a.compactTrigger() {
			t.Fatalf("round %d left %d visible tokens, still at or above the %d boundary", round+1, visible, a.compactTrigger())
		}
		growTurn(a, 30)
	}

	if a.activeTurnCreatedAt.Load() != economicActiveTurnAt {
		t.Fatal("the turn changed; this proves nothing about folding inside one")
	}
	for i := 1; i < len(versions); i++ {
		if versions[i] <= versions[i-1] {
			t.Fatalf("projection versions %v did not advance", versions)
		}
		if covered[i] <= covered[i-1] {
			t.Fatalf("checkpoint covered %v did not advance; a later fold re-folded the same prefix", covered)
		}
	}
	if grown := len(a.sess.conversation.Snapshot()); grown <= canonicalBefore {
		t.Fatal("the fixture never grew the turn")
	}
	// The canonical transcript is the recovery record; a projection may not
	// rewrite it, so the request that began the turn is still where it was.
	for _, m := range a.sess.conversation.Snapshot() {
		if m.Role == provider.RoleUser && m.CreatedAt == economicActiveTurnAt {
			goto found
		}
	}
	t.Fatal("the active turn's request is gone from the canonical transcript")
found:
	// And it is still verbatim in what the model sees.
	for _, m := range a.modelVisibleMessages() {
		if m.Role == provider.RoleUser && m.CreatedAt == economicActiveTurnAt {
			return
		}
	}
	t.Fatal("the active turn's request was folded into a digest")
}

// Growth that never closes leaves one thing a second fold can still reach: the
// tail the first fold kept verbatim, which lives in the canonical transcript
// rather than in the frozen body. Reaching it is legitimate, so the verdict is
// the economics of folding it, not an absence of new history.
func TestGrowthWithNoClosedTransactionStaysBelowFoldEconomics(t *testing.T) {
	a, sink := economicFixture(t, 0, 30)
	a.activeTurnCreatedAt.Store(economicActiveTurnAt)
	ctx := context.Background()
	if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
		t.Fatal(err)
	}
	if a.currentProjectionVersion() == 0 {
		t.Fatal("the first fold did not install a projection")
	}
	openTransaction(a, 40)
	for range 3 {
		if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
			t.Fatal(err)
		}
	}
	codes := maintenanceCodes(sink, "noop")
	if len(codes) != 1 || codes[0] != string(NoopFoldBelowEconomics) {
		t.Fatalf("noop codes = %v, want exactly [%s]", codes, NoopFoldBelowEconomics)
	}
}

// A turn whose every round is still in flight has nothing closed to fold, and
// nothing precedes it. That is a different verdict from an exhausted history,
// and the two are told apart by the code rather than by the sentence beside it.
func TestActiveTurnBoundaryLeavesNothingToFold(t *testing.T) {
	body := strings.Repeat("x", 24*1024)
	// Too large to be pinned as a brief, so the fixed prefix is the system
	// message alone and the running turn starts exactly where a fold would.
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: body, CreatedAt: economicActiveTurnAt},
	}}
	// One assistant turn holding many calls, none of them answered yet: the
	// transaction opened and never closed, so no prefix of it is foldable.
	calls := make([]provider.ToolCall, 0, 40)
	for i := range 40 {
		calls = append(calls, provider.ToolCall{ID: "open-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Name: "read_file", Arguments: `{"path":"` + body + `"}`})
	}
	sess.Messages = append(sess.Messages, provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})
	sink := &recordSink{}
	a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess, Options{
		ContextWindow: 1_000_000, CompactRatio: 0.85, RecentKeep: 2, ArchiveDir: testenv.TempDir(t),
	}, sink)
	a.activeTurnCreatedAt.Store(economicActiveTurnAt)
	if _, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
		t.Fatal(err)
	}
	codes := maintenanceCodes(sink, "noop")
	if len(codes) != 1 || codes[0] != string(NoopActiveTurnBoundary) {
		t.Fatalf("noop codes = %v, want exactly [%s]", codes, NoopActiveTurnBoundary)
	}
}
