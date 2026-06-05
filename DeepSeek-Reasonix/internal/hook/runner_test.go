package hook

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// --- Runner construction ---

func TestNewRunnerNil(t *testing.T) {
	var r *Runner
	if r.Enabled() {
		t.Error("nil Runner should not be enabled")
	}
	if r.Hooks() != nil {
		t.Error("nil Runner.Hooks() should be nil")
	}
}

func TestNewRunnerEmpty(t *testing.T) {
	r := NewRunner(nil, "/tmp", nil, nil)
	if r.Enabled() {
		t.Error("empty hooks Runner should not be enabled")
	}
}

func TestNewRunnerWithHooks(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "echo hi"}, Event: PreToolUse, Scope: ScopeGlobal},
	}
	r := NewRunner(hooks, "/tmp", nil, nil)
	if !r.Enabled() {
		t.Error("Runner with hooks should be enabled")
	}
	if len(r.Hooks()) != 1 {
		t.Errorf("Hooks() count = %d, want 1", len(r.Hooks()))
	}
}

// --- Runner.PreToolUse ---

func TestRunnerPreToolUseNoHooks(t *testing.T) {
	r := NewRunner(nil, "/tmp", nil, nil)
	block, msg := r.PreToolUse(context.Background(), "bash", nil)
	if block || msg != "" {
		t.Errorf("no hooks should pass: block=%v msg=%q", block, msg)
	}
}

func TestRunnerPreToolUsePass(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "allow"}, Event: PreToolUse},
	}
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: 0}
	}
	r := NewRunner(hooks, "/tmp", spawner, nil)
	block, msg := r.PreToolUse(context.Background(), "bash", nil)
	if block {
		t.Errorf("exit 0 should not block: msg=%q", msg)
	}
}

func TestRunnerPreToolUseBlock(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "deny"}, Event: PreToolUse},
	}
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: 2, Stderr: "blocked by policy"}
	}
	var notified string
	notify := func(msg string) { notified = msg }
	r := NewRunner(hooks, "/tmp", spawner, notify)
	block, msg := r.PreToolUse(context.Background(), "bash", nil)
	if !block {
		t.Error("exit 2 on PreToolUse should block")
	}
	if msg == "" {
		t.Error("block message should not be empty")
	}
	if notified == "" {
		t.Error("notify should have been called")
	}
}

// --- Runner.PostToolUse ---

func TestRunnerPostToolUseNoHooks(t *testing.T) {
	r := NewRunner(nil, "/tmp", nil, nil)
	// Should not panic.
	r.PostToolUse(context.Background(), "bash", nil, "ok")
}

func TestRunnerPostToolUseWarn(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "warn"}, Event: PostToolUse},
	}
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: 1, Stdout: "warning message"}
	}
	var notified string
	notify := func(msg string) { notified = msg }
	r := NewRunner(hooks, "/tmp", spawner, notify)
	r.PostToolUse(context.Background(), "bash", nil, "result")
	if notified == "" {
		t.Error("PostToolUse warn should notify")
	}
}

// --- Runner.PromptSubmit ---

func TestRunnerPromptSubmitBlock(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "gate"}, Event: UserPromptSubmit},
	}
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: 2, Stderr: "not allowed"}
	}
	r := NewRunner(hooks, "/tmp", spawner, nil)
	block, _ := r.PromptSubmit(context.Background(), "bad input", 1)
	if !block {
		t.Error("exit 2 on UserPromptSubmit should block")
	}
}

// --- Runner.Stop ---

func TestRunnerStopNoHooks(t *testing.T) {
	r := NewRunner(nil, "/tmp", nil, nil)
	// Should not panic.
	r.Stop(context.Background(), "last answer", 1)
}

func TestRunnerStopWithHooks(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "log"}, Event: Stop},
	}
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: 0}
	}
	r := NewRunner(hooks, "/tmp", spawner, nil)
	r.Stop(context.Background(), "done", 1)
}

// --- Runner.PostLLMCall ---

func TestRunnerHasPostLLMCall(t *testing.T) {
	with := NewRunner([]ResolvedHook{{HookConfig: HookConfig{Command: "x"}, Event: PostLLMCall}}, "/tmp", nil, nil)
	if !with.HasPostLLMCall() {
		t.Error("a configured PostLLMCall hook should report HasPostLLMCall")
	}
	without := NewRunner([]ResolvedHook{{HookConfig: HookConfig{Command: "x"}, Event: Stop}}, "/tmp", nil, nil)
	if without.HasPostLLMCall() {
		t.Error("only a Stop hook should not report HasPostLLMCall")
	}
	if (*Runner)(nil).HasPostLLMCall() {
		t.Error("nil runner should report no PostLLMCall hook")
	}
}

func TestRunnerPostLLMCallReplacesReasoning(t *testing.T) {
	hooks := []ResolvedHook{{HookConfig: HookConfig{Command: "translate"}, Event: PostLLMCall}}
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: 0, Stdout: "  译文  "}
	}
	r := NewRunner(hooks, "/tmp", spawner, nil)
	if got := r.PostLLMCall(context.Background(), "raw reasoning", 2); got != "译文" {
		t.Fatalf("PostLLMCall = %q, want trimmed hook stdout", got)
	}
}

func TestRunnerPostLLMCallKeepsOriginal(t *testing.T) {
	cases := []struct {
		name  string
		hooks []ResolvedHook
		spawn SpawnResult
	}{
		{"no PostLLMCall hook", []ResolvedHook{{HookConfig: HookConfig{Command: "x"}, Event: Stop}}, SpawnResult{ExitCode: 0, Stdout: "ignored"}},
		{"empty stdout", []ResolvedHook{{HookConfig: HookConfig{Command: "x"}, Event: PostLLMCall}}, SpawnResult{ExitCode: 0, Stdout: "   "}},
		{"non-zero exit", []ResolvedHook{{HookConfig: HookConfig{Command: "x"}, Event: PostLLMCall}}, SpawnResult{ExitCode: 1, Stdout: "should be ignored"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRunner(tc.hooks, "/tmp", func(context.Context, SpawnInput) SpawnResult { return tc.spawn }, nil)
			if got := r.PostLLMCall(context.Background(), "raw", 1); got != "raw" {
				t.Fatalf("PostLLMCall = %q, want original reasoning preserved", got)
			}
		})
	}
}

// --- FormatOutcome ---

func TestFormatOutcomePass(t *testing.T) {
	o := Outcome{
		Hook:     ResolvedHook{HookConfig: HookConfig{Command: "echo hi"}, Event: PreToolUse, Scope: ScopeProject},
		Decision: DecisionPass,
	}
	msg := FormatOutcome(o)
	if msg == "" {
		t.Error("FormatOutcome should not be empty")
	}
}

func TestFormatOutcomeWithDetail(t *testing.T) {
	o := Outcome{
		Hook:      ResolvedHook{HookConfig: HookConfig{Command: "check"}, Event: PreToolUse, Scope: ScopeGlobal},
		Decision:  DecisionBlock,
		Stderr:    "forbidden",
		Truncated: true,
	}
	msg := FormatOutcome(o)
	if !contains(msg, "forbidden") {
		t.Errorf("should include stderr: %s", msg)
	}
	if !contains(msg, "truncated") {
		t.Errorf("should mention truncation: %s", msg)
	}
}

// --- clipRunes ---

func TestClipRunes(t *testing.T) {
	if got := clipRunes("short", 10); got != "short" {
		t.Errorf("clipRunes short = %q", got)
	}
	if got := clipRunes("hello world", 5); got != "hello…" {
		t.Errorf("clipRunes = %q", got)
	}
	if got := clipRunes("", 5); got != "" {
		t.Errorf("clipRunes empty = %q", got)
	}
	if got := clipRunes("abc", 0); got != "" {
		t.Errorf("clipRunes max=0 = %q", got)
	}
}

// --- payload JSON ---

func TestPayloadJSON(t *testing.T) {
	args := json.RawMessage(`{"command":"echo hi"}`)
	p := Payload{
		Event:    PreToolUse,
		Cwd:      "/tmp",
		ToolName: "bash",
		ToolArgs: args,
		Turn:     1,
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Payload
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Event != PreToolUse {
		t.Errorf("Event = %q", decoded.Event)
	}
	if decoded.ToolName != "bash" {
		t.Errorf("ToolName = %q", decoded.ToolName)
	}
	if decoded.Turn != 1 {
		t.Errorf("Turn = %d", decoded.Turn)
	}
}

// --- capping behavior ---

func TestCappedBuffer(t *testing.T) {
	var cb cappedBuffer
	// Write within cap.
	n, err := cb.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Errorf("small write: n=%d err=%v", n, err)
	}
	if cb.truncated {
		t.Error("should not be truncated yet")
	}
	if cb.String() != "hello" {
		t.Errorf("String() = %q", cb.String())
	}

	// Write beyond cap.
	big := make([]byte, outputCapBytes+1000)
	for i := range big {
		big[i] = 'x'
	}
	n, err = cb.Write(big)
	if err != nil || n != len(big) {
		t.Errorf("big write: n=%d err=%v", n, err)
	}
	if !cb.truncated {
		t.Error("should be truncated after exceeding cap")
	}
}

// --- IsBlocking ---

func TestIsBlocking(t *testing.T) {
	if !IsBlocking(PreToolUse) {
		t.Error("PreToolUse should be blocking")
	}
	if !IsBlocking(UserPromptSubmit) {
		t.Error("UserPromptSubmit should be blocking")
	}
	if IsBlocking(PostToolUse) {
		t.Error("PostToolUse should not be blocking")
	}
	if IsBlocking(Stop) {
		t.Error("Stop should not be blocking")
	}
}

// --- defaultTimeout ---

func TestDefaultTimeout(t *testing.T) {
	if defaultTimeout(PreToolUse) != 5*time.Second {
		t.Errorf("PreToolUse timeout = %v", defaultTimeout(PreToolUse))
	}
	if defaultTimeout(PostToolUse) != 30*time.Second {
		t.Errorf("PostToolUse timeout = %v", defaultTimeout(PostToolUse))
	}
}

// helper
func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
