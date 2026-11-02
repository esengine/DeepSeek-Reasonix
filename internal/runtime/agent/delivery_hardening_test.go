package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// fakeReadFileTool is a minimal read-only tool whose successful calls produce
// Read receipts with an extractable path, like the real read_file.
type fakeReadFileTool struct{}

func (fakeReadFileTool) Name() string            { return "read_file" }
func (fakeReadFileTool) Description() string     { return "fake read" }
func (fakeReadFileTool) ReadOnly() bool          { return true }
func (fakeReadFileTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (fakeReadFileTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "contents", nil
}

// fakeWriterTool is registered (never called) so a registry counts as
// writer-capable for delivery mutation expectations.
type fakeWriterTool struct{}

func (fakeWriterTool) Name() string            { return "fake_write" }
func (fakeWriterTool) Description() string     { return "fake write" }
func (fakeWriterTool) ReadOnly() bool          { return false }
func (fakeWriterTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (fakeWriterTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "wrote", nil
}

func TestDeliveryResolvedReadOnlyBashDoesNotArmMutationReadiness(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(stubBash{})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("pwd-base", "bash", `{"command":"basename \"$(pwd)\""}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "workspace basename inspected"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "inspect and report the current workspace basename"); err != nil {
		t.Fatalf("resolved read-only delivery command: %v", err)
	}
	if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("resolved read-only bash was recorded as a mutation")
	}
	msgs := a.sess.conversation.Snapshot()
	var resolved bool
	for _, msg := range msgs {
		for _, call := range msg.ToolCalls {
			if call.ID == "pwd-base" && call.ResolvedReadOnly != nil && *call.ResolvedReadOnly {
				resolved = true
			}
		}
	}
	if !resolved {
		t.Fatal("session receipt did not preserve resolved_read_only=true")
	}
}

func TestDeliveryConversationTokenSurvivesToNextTurnWithoutActionEvidence(t *testing.T) {
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "Understood."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "ORBIT-42"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "Remember ORBIT-42 and answer on the next turn."); err != nil {
		t.Fatalf("deferred conversation turn was blocked: %v", err)
	}
	if err := a.Run(context.Background(), "What was the code?"); err != nil {
		t.Fatalf("answer turn was blocked: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want exactly two conversational turns", prov.call)
	}
	if got := lastAssistantContent(a.Session()); got != "ORBIT-42" {
		t.Fatalf("last assistant text = %q, want ORBIT-42", got)
	}
}

func TestDeliveryDurableMemoryRequiresRememberWithoutCodeCeremony(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "remember", readOnly: false})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("remember", "remember", `{"description":"ORBIT code","body":"ORBIT-42"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Saved for future sessions."}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "Remember ORBIT-42 permanently across sessions"); err != nil {
		t.Fatalf("durable-memory workflow inherited code-delivery ceremony: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want remember plus final answer", prov.call)
	}
	if a.turn.deliveryCriteriaEstablished {
		t.Fatal("durable-memory-only workflow should not manufacture code acceptance criteria")
	}
}

func TestNonGoalUpdateGoalWithVisibleTextDoesNotSpendRepairRound(t *testing.T) {
	goalTool, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(goalTool)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "Here is the answer."}, toolCallChunk("goal", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "unexpected repair"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(context.Background(), "answer normally"); err != nil {
		t.Fatalf("non-Goal update_goal with text: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want no repair round", prov.call)
	}
	if got := lastAssistantContent(a.Session()); got != "Here is the answer." {
		t.Fatalf("last assistant text = %q", got)
	}
	if got := lastToolResult(a.Session(), "update_goal"); !strings.Contains(got, "only available while an active goal turn") {
		t.Fatalf("paired update_goal result = %q", got)
	}
}

func TestNonGoalToolOnlyUpdateGoalGetsAtMostOneRepairRound(t *testing.T) {
	goalTool, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(goalTool)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("goal-1", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("goal-2", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "unexpected third round"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{}, event.Discard)
	err := a.Run(context.Background(), "answer normally")
	if err == nil || !strings.Contains(err.Error(), "repeatedly called update_goal outside Goal mode") {
		t.Fatalf("repeated tool-only misuse error = %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want one repair round", prov.call)
	}
}

func TestDeliveryPlanModeReturnsProposalBeforeExecutionReadiness(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	proposal := "1. Fix the parser\n   - update a.go\n   - run the focused tests"
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: proposal}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	a.SetPlanMode(true)

	if err := a.Run(context.Background(), "fix the parser bug in a.go"); err != nil {
		t.Fatalf("delivery plan proposal was blocked by execution readiness: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 without readiness retries in plan mode", prov.call)
	}
	if got := lastAssistantContent(a.Session()); got != proposal {
		t.Fatalf("last assistant text = %q, want proposal %q", got, proposal)
	}

}

func TestPreviewStripsDeliveryMarkerAndSyntheticTurns(t *testing.T) {
	first := "你是谁？\n\n" + sessionstore.DeliveryRuntimeMarker
	if got := sessionstore.UserPreviewText(first); got != "你是谁？" {
		t.Fatalf("UserPreviewText kept framing: %q", got)
	}
	// A literal <delivery-runtime> mention inside user prose is not the host
	// suffix: nothing may be cut. (The agent never appends the marker when the
	// input already mentions the tag, so this content carries no host suffix.)
	inline := "Explain this literal: <delivery-runtime>example</delivery-runtime> and keep this sentence"
	if got := sessionstore.UserPreviewText(inline); got != inline {
		t.Fatalf("inline delivery-runtime mention was mangled: %q", got)
	}
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: first},
		{Role: provider.RoleAssistant, Content: "hi"},
		{Role: provider.RoleUser, Content: sessionstore.MidTurnSteerPrefix + "\nslow down"},
		{Role: provider.RoleUser, Content: "帮我写一个魂斗罗游戏\n\n" + sessionstore.DeliveryRuntimeMarker},
	}
	preview, turns := sessionstore.SessionPreviewFromMessages(msgs)
	if preview != "你是谁？" {
		t.Fatalf("preview = %q", preview)
	}
	if turns != 2 {
		t.Fatalf("turns = %d, want 2 (steer excluded)", turns)
	}
}

func TestDeliveryDiagnosticConversationCompletes(t *testing.T) {
	// End-to-end: a diagnostic troubleshooting conversation with no mutation
	// keywords must complete without a FinalReadinessError — the agent can
	// give advice but can't write files on the user's machine.
	inputs := []string{
		"为什么wps导入zetero参考文献报错，请你帮我诊断一下",
		"分析一下为什么 WPS 不能运行",
		"why can't I check my email in Outlook?",
		"what does `context.Context` mean?",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(fakeReadFileTool{})
			reg.Add(fakeWriterTool{})
			// The model gives advice text (no tool calls) — a diagnostic response.
			advice := []provider.Chunk{
				{Type: provider.ChunkText, Text: "请尝试以下步骤：1. 检查端口监听 2. 重新注册插件"},
				{Type: provider.ChunkDone},
			}
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{advice}}
			a := New(prov, reg, sessionstore.NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
			if err := a.Run(context.Background(), input); err != nil {
				t.Fatalf("diagnostic conversation deadlocked: %v", err)
			}
			if prov.call != 1 {
				t.Fatalf("diagnostic conversation had %d provider calls, want 1 (no readiness retries)", prov.call)
			}
		})
	}
}
