package agent

import (
	"context"
	"slices"
	"strings"
	"testing"

	"encoding/json"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// unwitnessedNoopVerdicts are the verdicts this file states but does not reach,
// each with what reaching it would take. Failing to reach one is not evidence
// that nothing does, so they are named rather than recorded as dead.
var unwitnessedNoopVerdicts = map[CompactionNoopReason]string{
	// A fold changes the view it consumed, so matching its hash again needs the
	// projection to stop being used while the canonical and the receipt both
	// survive — which is what a resume does, and needs a second process.
	NoopInputUnchanged: "needs a resume",
}

// allNoopVerdicts is the family this census covers. A member added without a
// witness or a stated reason is a verdict nobody has classified.
func allNoopVerdicts() []CompactionNoopReason {
	return []CompactionNoopReason{
		NoopNoNewClosedPrefix, NoopActiveTurnBoundary, NoopNoFoldableRegion,
		NoopFoldBelowEconomics, NoopInputUnchanged, NoopFoldEmptyAfterHooks,
		NoopFixedPrefixAboveTrigger,
	}
}

// Whether a noop verdict is still something production can produce. The only way
// to tell an unreachable code from a rare one is to try to reach it: a name that
// looks legacy is not evidence that it is.
func TestEveryNoopVerdictIsReachable(t *testing.T) {
	reachable := map[CompactionNoopReason]func(*testing.T) []string{
		// Fold, then add one small message. The recent tail is measured in
		// tokens, so a budget covering the increment reaches back into the body
		// and the region a second fold could take ends inside it.
		NoopNoNewClosedPrefix: func(t *testing.T) []string {
			a := windowedFoldFixture(t, 120_000)
			ctx := context.Background()
			if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
				t.Fatal(err)
			}
			if a.currentProjectionVersion() == 0 {
				t.Fatal("the first fold installed no projection")
			}
			a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, Content: "one more step"})
			_, reason, err := a.compactToProjection(ctx, CompactionTriggerPressure, "", false, false)
			if err != nil {
				t.Fatal(err)
			}
			return []string{string(reason)}
		},
		// Nothing has closed inside the turn and nothing precedes it: the whole
		// visible context is one transaction still in flight.
		NoopActiveTurnBoundary: func(t *testing.T) []string {
			body := strings.Repeat("x", 24*1024)
			sess := &Session{Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "system"},
				{Role: provider.RoleUser, Content: body, CreatedAt: economicActiveTurnAt},
			}}
			calls := make([]provider.ToolCall, 0, 40)
			for i := range 40 {
				calls = append(calls, provider.ToolCall{
					ID:   "open-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
					Name: "read_file", Arguments: `{"path":"` + body + `"}`})
			}
			sess.Messages = append(sess.Messages, provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})
			a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess, Options{
				ContextWindow: 1_000_000, CompactRatio: 0.85, RecentKeep: 2,
				ArchiveDir: testenv.TempDir(t)}, &recordSink{})
			a.activeTurnCreatedAt.Store(economicActiveTurnAt)
			return []string{directNoop(t, a)}
		},
		// A conversation with nothing between its pinned head and its recent
		// tail has no region a fold could take.
		NoopNoFoldableRegion: func(t *testing.T) []string {
			a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), &Session{Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "system"},
				{Role: provider.RoleUser, Content: "one question"},
			}}, Options{ContextWindow: 100_000, CompactRatio: 0.85, RecentKeep: 2,
				ArchiveDir: testenv.TempDir(t)}, &recordSink{})
			return []string{directNoop(t, a)}
		},
		// The pinned head alone is over the trigger, so no fold below it can
		// bring the prompt under one.
		NoopFixedPrefixAboveTrigger: func(t *testing.T) []string {
			a := windowedFoldFixture(t, 120_000)
			a.budgets.FirstTurnPinTokens = 1 << 20
			a.sess.conversation.Replace(append([]provider.Message{
				{Role: provider.RoleSystem, Content: strings.Repeat("s", 400*1024)},
				{Role: provider.RoleUser, Content: strings.Repeat("u", 400*1024)},
			}, a.sess.conversation.Snapshot()[2:]...))
			return []string{directNoop(t, a)}
		},
		// An extension registered at the compaction-prepare point may rewrite
		// the fold, and rewriting it to nothing leaves nothing to summarise.
		NoopFoldEmptyAfterHooks: func(t *testing.T) []string {
			a, _ := economicFixture(t, 0, -1)
			// Raw JSON rather than a marshalled struct: the payload's messages
			// field is omitempty, so a Go value cannot express an empty fold at
			// all — an extension is a separate process and sends its own bytes.
			client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
				return protocol.InterceptResult{
					Decision:    protocol.DecisionReplace,
					Replacement: json.RawMessage(`{"messages":[]}`),
				}, nil
			}}
			a.SetExtensions(newExtSlotDispatcher(client, false, nil,
				[]extension.InterceptorPoint{extension.PointCompactionPrepare},
				map[extension.Slot]string{extension.SlotCompaction: extTestPlugin}))
			return []string{directNoop(t, a)}
		},
		// Growth that never closes: the tail a first fold kept verbatim is
		// reachable, and folding it costs more than it frees.
		NoopFoldBelowEconomics: func(t *testing.T) []string {
			a, sink := economicFixture(t, 0, 30)
			a.activeTurnCreatedAt.Store(economicActiveTurnAt)
			ctx := context.Background()
			if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
				t.Fatal(err)
			}
			openTransaction(a, 40)
			for range 3 {
				if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
					t.Fatal(err)
				}
			}
			return maintenanceCodes(sink, "noop")
		},
	}
	for reason, reach := range reachable {
		t.Run(string(reason), func(t *testing.T) {
			if codes := reach(t); !slices.Contains(codes, string(reason)) {
				t.Fatalf("production reported %v and never %s; the verdict may be unreachable", codes, reason)
			}
		})
	}
	for _, reason := range allNoopVerdicts() {
		if _, witnessed := reachable[reason]; witnessed {
			continue
		}
		if _, stated := unwitnessedNoopVerdicts[reason]; !stated {
			t.Errorf("%s has neither a witness nor a stated reason: nobody has classified it", reason)
		}
	}
}

// windowedFoldFixture is a conversation whose projection body is large against
// the window — the state where a recent-tail budget reaches past the tail and
// into the body. The window is the variable that decides how far it reaches.
func windowedFoldFixture(t *testing.T, window int) *Agent {
	t.Helper()
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "long-running task"},
	}}
	body := strings.Repeat("x", 24*1024)
	for i := range 40 {
		sess.Messages = append(sess.Messages,
			provider.Message{Role: provider.RoleAssistant, Content: "step " + string(rune('a'+i%26))},
			provider.Message{Role: provider.RoleUser, Content: body})
	}
	return New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess, Options{
		ContextWindow: window, CompactRatio: 0.85, RecentKeep: 2,
		ArchiveDir: testenv.TempDir(t),
	}, &recordSink{})
}

// directNoop asks for one automatic fold and returns the verdict. The trigger is
// bypassed on purpose: whether pressure would fire is a separate question from
// whether a verdict has a production path, and the two failed attempts that
// preceded this census confused them.
func directNoop(t *testing.T, a *Agent) string {
	t.Helper()
	// A rejected checkpoint reports its verdict and an error together, and the
	// verdict is what this reads: refusing to fold and failing to are the same
	// answer to "why was nothing folded".
	_, reason, err := a.compactToProjection(context.Background(), CompactionTriggerPressure, "", false, false)
	if reason == "" && err != nil {
		t.Fatalf("compactToProjection: %v", err)
	}
	return string(reason)
}
