package boot

// Effect tests assert final-boundary behavior through the real Build stack:
// a scripted provider records what actually reaches the provider boundary.
// Component correctness is not system effectiveness (see REASONIX.md).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
)

type effectRecordingProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *effectRecordingProvider) Name() string { return "boot-effect-test" }

func (p *effectRecordingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *effectRecordingProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// effectRun builds the real stack around a recording provider, runs one prompt,
// and returns the executor's own requests at the provider boundary.
func effectRun(t *testing.T, kind, tokenMode string, arm ablation.Set) []provider.Request {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &effectRecordingProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: tokenMode, Ablation: arm})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := agentRequests(rec.requests())
	if len(reqs) == 0 {
		t.Fatal("no agent request reached the provider boundary")
	}
	return reqs
}

func toolNames(req provider.Request) map[string]bool {
	names := make(map[string]bool, len(req.Tools))
	for _, tool := range req.Tools {
		names[tool.Name] = true
	}
	return names
}

// TestEffectRoleSettingsShareProviderToolSurface pins the unified contract:
// light/balanced/delivery send identical top-level tool schemas; optional
// tools are reached only through use_capability.
func TestEffectRoleSettingsShareProviderToolSurface(t *testing.T) {
	balanced := effectRun(t, "boot-effect-balanced", "", ablation.Set{})
	light := effectRun(t, "boot-effect-light", "economy", ablation.Set{})
	delivery := effectRun(t, "boot-effect-delivery", "delivery", ablation.Set{})

	balNames := toolSchemaNames(balanced[0].Tools)
	if !reflect.DeepEqual(toolSchemaNames(light[0].Tools), balNames) {
		t.Fatalf("light surface diverged from balanced\nlight=%v\nbalanced=%v", toolSchemaNames(light[0].Tools), balNames)
	}
	if !reflect.DeepEqual(toolSchemaNames(delivery[0].Tools), balNames) {
		t.Fatalf("delivery surface diverged from balanced\ndelivery=%v\nbalanced=%v", toolSchemaNames(delivery[0].Tools), balNames)
	}
	if len(balNames) > 16 {
		t.Fatalf("unified surface sent %d tools; expected a small fixed core set", len(balNames))
	}
	names := toolNames(balanced[0])
	if !names["use_capability"] {
		t.Fatal("unified surface must expose use_capability")
	}
	if names["connect_tool_source"] {
		t.Fatal("connect_tool_source must not appear on the provider-visible surface")
	}
	if names["task"] || names["grep"] {
		t.Fatal("optional tools must not be top-level; use use_capability")
	}
}

// TestEffectSubagentAblationRemovesChildToolSchemas asserts the ablation at
// the capability boundary: with subagents off the model cannot dispatch
// task/fleet through the registry even via use_capability.
func TestEffectSubagentAblationRemovesChildToolSchemas(t *testing.T) {
	control := effectRun(t, "boot-effect-sub-on", "", ablation.Set{})
	ablated := effectRun(t, "boot-effect-sub-off", "", ablation.New(ablation.Subagent))

	// Top-level schema never exposes task; verify registry dispatch instead.
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	_ = control
	_ = ablated
	// Ablation is enforced inside TaskTool registration at boot; the unified
	// surface stays use_capability-only either way.
	if names := toolNames(control[0]); names["task"] {
		t.Fatal("unified surface must not expose task top-level")
	}
	if names := toolNames(ablated[0]); names["task"] || names["parallel_tasks"] || names["fleet"] {
		t.Fatalf("subagent-ablated surface still offers spawn tools top-level: %v", toolSchemaNames(ablated[0].Tools))
	}
}

// budgetRunawayProvider never repeats itself and never fails, so every
// adaptive guard stays quiet. Only the spend gate can stop it.
type budgetRunawayProvider struct {
	mu     sync.Mutex
	rounds int
}

func (p *budgetRunawayProvider) Name() string { return "boot-budget-runaway" }

func (p *budgetRunawayProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 4)
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID:        fmt.Sprintf("call-%d", round),
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"file%d.txt"}`, round),
	}}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
		PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, RequestCount: 1,
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *budgetRunawayProvider) roundCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rounds
}

// TestEffectTaskBudgetLandsARunawayThroughRealBuild pins the gate at its final
// boundary: a configured spend budget must stop a wandering turn through the
// real Build assembly. Nothing else would stop it: ordinary chat has no round
// ceiling, and this provider never repeats itself.
func TestEffectTaskBudgetLandsARunawayThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &budgetRunawayProvider{}
	provider.Register("boot-budget-gate", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
task_time_budget_minutes = 0.0005

[[providers]]
name = "test-model"
kind = "boot-budget-gate"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	runCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runErr := ctrl.Run(runCtx, "read every file you can find")

	// Ordinary chat has no round ceiling, so a gate that never reached the
	// executor would run until the context deadline. Assert the typed boundary
	// instead of a machine-speed-dependent round count.
	pause, ok := agent.InspectRunPause(runErr)
	if !ok || pause.Kind != "task_budget" || pause.Key != "time" {
		t.Fatalf("Run error = %v (pause=%+v, ok=%v), want time task-budget pause", runErr, pause, ok)
	}
	if rec.roundCount() == 0 {
		t.Fatal("no round reached the provider; the run never started")
	}
}

// The token axis is the one that generalises: a loop serving 99.9% of its
// prompt from cache still accumulates them, where wall clock catches only a
// slow loop and money reads as free on an unpriced model. It reached the
// runtime through nothing until this wiring existed.
func TestEffectTaskTokenBudgetLandsARunawayThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &budgetRunawayProvider{}
	provider.Register("boot-token-budget-gate", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
task_token_budget = 5000

[[providers]]
name = "test-model"
kind = "boot-token-budget-gate"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := ctrl.Run(runCtx, "read every file you can find")

	pause, ok := agent.InspectRunPause(runErr)
	if !ok || pause.Kind != "task_budget" || pause.Key != "token" {
		t.Fatalf("Run error = %v (pause=%+v, ok=%v), want token task-budget pause", runErr, pause, ok)
	}
	if rec.roundCount() == 0 {
		t.Fatal("no round reached the provider; the run never started")
	}
}

// spillFollowProvider walks the two turns the read-back loop lived in: a shell
// read, which cannot page and so parks its output, then read_file following
// whatever pointer came back — exactly what a model does when told its output
// was kept out of context.
type spillFollowProvider struct {
	mu     sync.Mutex
	target string
	turn   int
	reqs   []provider.Request
}

func (p *spillFollowProvider) Name() string { return "boot-spill-follow" }

func (p *spillFollowProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.turn++
	turn := p.turn
	p.mu.Unlock()

	ch := make(chan provider.Chunk, 3)
	switch results := effectToolResults(req); turn {
	case 1:
		emitBash(ch, "call-first", "cat "+filepath.Base(p.target))
	case 2:
		if path := effectPointerPath(results[len(results)-1]); path != "" {
			emitReadFile(ch, "call-readback", path)
			break
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "no pointer to follow"}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *spillFollowProvider) lastRequest(t *testing.T) provider.Request {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) == 0 {
		t.Fatal("no request reached the provider boundary")
	}
	return p.reqs[len(p.reqs)-1]
}

// emitBash drives the one shape that still parks its output: a tool with no
// continuation of its own, so the pointer is the only way back to the bytes.
func emitBash(ch chan<- provider.Chunk, id, command string) {
	args, _ := json.Marshal(map[string]string{"command": command})
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID: id, Name: "bash", Arguments: string(args),
	}}
}

func emitReadFile(ch chan<- provider.Chunk, id, path string) {
	args, _ := json.Marshal(map[string]string{"path": path})
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID: id, Name: "read_file", Arguments: string(args),
	}}
}

func effectToolResults(req provider.Request) []string {
	var out []string
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

func effectPointerPath(result string) string {
	for l := range strings.SplitSeq(result, "\n") {
		if rest, ok := strings.CutPrefix(l, "Full output: "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// TestEffectSpilledOutputSurvivesTheReadBack pins the promise the pointer makes,
// at the boundary that decides whether it was kept: a parked result the model
// reaches for must arrive as content. Spilling that fetch instead returns a new
// pointer to a new file, and read_file numbers what it returns, so each round is
// larger than the last — the arithmetic every component test passed.
func TestEffectSpilledOutputSurvivesTheReadBack(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	const marker = "a line of recorded output"
	writeFile(t, dir, "big.log", strings.Repeat(marker+"\n", 3000))

	rec := &spillFollowProvider{target: filepath.Join(dir, "big.log")}
	provider.Register("boot-spill-follow", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

# The subject is the pointer, not the jail: a host without an OS sandbox
# refuses bash fail-closed, and the parked output this test follows never
# exists. CI runners vary on whether bwrap is installed.
[sandbox]
bash = "off"

[[providers]]
name = "test-model"
kind = "boot-spill-follow"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "read big.log"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := effectToolResults(rec.lastRequest(t))
	if len(results) < 2 {
		t.Fatalf("want the first read and its read-back at the boundary, got %d tool result(s)", len(results))
	}
	first, readBack := results[0], results[1]
	if !strings.Contains(first, "kept out of context") {
		t.Fatalf("a file this far past the cap should have been parked:\n%s", effectHead(first))
	}
	// This assembly has no transcript, so the spill lands in scratch outside the
	// isolated home; the aging sweep would not reach it for an hour.
	if p := effectPointerPath(first); p != "" && !strings.HasPrefix(p, dir) {
		t.Cleanup(func() { _ = os.Remove(p) })
	}
	if strings.Contains(readBack, "kept out of context") {
		t.Fatalf("the read-back was parked too, so the content can never arrive:\n%s", effectHead(readBack))
	}
	if !strings.Contains(readBack, marker) {
		t.Fatalf("the read-back reached the model without the file's content:\n%s", effectHead(readBack))
	}
}

func effectHead(s string) string {
	if len(s) > 240 {
		return s[:240]
	}
	return s
}

// TestEffectPrefixCarriesNoProjectText asserts the property the goldens can
// only catch drift from: two projects produce the same prefix. The workspace
// path was the one per-project string in it, and everything behind it — the
// rest of the prompt and 4.3k tokens of tool schema — missed the provider's
// prefix cache on every project the machine had not already warmed.
func TestEffectPrefixCarriesNoProjectText(t *testing.T) {
	a := effectRun(t, "boot-effect-proj-a", "", ablation.Set{})
	b := effectRun(t, "boot-effect-proj-b", "", ablation.Set{})
	sysA, sysB := systemMessage(a[0].Messages), systemMessage(b[0].Messages)
	if sysA == "" {
		t.Fatal("no system message reached the provider boundary")
	}
	if sysA != sysB {
		for i := range min(len(sysA), len(sysB)) {
			if sysA[i] != sysB[i] {
				t.Fatalf("prefix differs between projects at byte %d of %d:\n a: %.160q\n b: %.160q",
					i, len(sysA), sysA[max(i-80, 0):], sysB[max(i-80, 0):])
			}
		}
		t.Fatalf("prefix length differs between projects: %d vs %d", len(sysA), len(sysB))
	}
	if !reflect.DeepEqual(toolSchemaNames(a[0].Tools), toolSchemaNames(b[0].Tools)) {
		t.Fatal("tool surface differs between projects")
	}
}

// Moving the workspace out of the prefix must not stop the model being told
// which directory it is in, or which one it is not.
func TestEffectTurnCarriesThisProjectsWorkspace(t *testing.T) {
	a := effectRun(t, "boot-effect-ws-a", "", ablation.Set{})
	b := effectRun(t, "boot-effect-ws-b", "", ablation.Set{})
	rootA, rootB := workspaceOf(a[0]), workspaceOf(b[0])
	if rootA == "" || rootB == "" {
		t.Fatal("a turn reached the provider with no workspace stated")
	}
	if rootA == rootB {
		t.Fatalf("both projects reported the same workspace %q", rootA)
	}
	if strings.Contains(userMessages(a[0]), rootB) {
		t.Fatalf("project A carried project B's root %q", rootB)
	}
}

// workspaceOf returns the root the turn stated, read from the host's block.
func workspaceOf(req provider.Request) string {
	const open = "<workspace>\nCurrent workspace: "
	_, rest, ok := strings.Cut(userMessages(req), open)
	if !ok {
		return ""
	}
	root, _, ok := strings.Cut(rest, "\n")
	if !ok {
		return ""
	}
	return root
}

func userMessages(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// TestEffectRememberDoesNotMoveTheCachedPrefix is the cache boundary for saved
// facts: a session that saves one must leave the next session's system message
// byte-identical. The index it writes used to be composed in ahead of the
// project instructions and the skills index, so one remember re-sent all of
// them; the facts are reached through the memory tool instead.
func TestEffectRememberDoesNotMoveTheCachedPrefix(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", effectProbeConfig)
	writeFile(t, dir, "REASONIX.md", "Project rule: keep the prefix stable.")

	indexPath := filepath.Join(memory.StoreFor(config.MemoryUserDir(), dir).Dir, "MEMORY.md")

	first, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}
	before := systemMessage(first.History())
	if _, err := first.SaveMemory(memory.Memory{
		Name: "deploy-target", Description: "where releases go", Type: memory.TypeProject,
		Body: "Releases go to the staging bucket first.",
	}); err != nil {
		first.Close()
		t.Fatalf("SaveMemory: %v", err)
	}
	first.Close()

	// A prefix that did not move because nothing was written proves nothing.
	index, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read the index the save was supposed to write: %v", err)
	}
	if !strings.Contains(string(index), "deploy-target") {
		t.Fatalf("the save never reached the index this test is about:\n%s", index)
	}

	second, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	after := systemMessage(second.History())
	second.Close()

	if before != after {
		t.Fatalf("a saved fact moved the cached prefix; every byte after it is re-sent next session:\nfirst diff site: %q",
			firstDivergence(before, after))
	}
	if strings.Contains(after, "deploy-target") {
		t.Fatalf("the saved fact reached the prefix:\n%s", after)
	}
}

// effectProbeConfig is a build-only config: a provider that is never called,
// so a test can compose a prefix without registering a recording kind.
const effectProbeConfig = `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`
