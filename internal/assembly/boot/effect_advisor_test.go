package boot

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// strongAdvisorProvider records what the advising model is sent and answers
// every request with one fixed piece of advice.
type strongAdvisorProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *strongAdvisorProvider) Name() string { return "boot-advisor-strong" }

func (p *strongAdvisorProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ADVICE: take the store lock before the index lock."}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func advisorEffectConfig(t *testing.T, dir, mainKind, advisorLine string) {
	t.Helper()
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"
`+advisorLine+`

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+mainKind+`"
model = "x"

[[providers]]
name = "strong"
kind = "boot-advisor-strong"
model = "pro"
`)
	approveWorkspace(t, dir)
}

// advisor_model puts advise on the provider-visible surface; the call reaches
// the advising model with the task and the question, and its answer comes back
// as the tool result. Without advisor_model the tool is not offered at all.
func TestEffectAdviseConsultsTheAdvisorModelWithTheConversation(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	strong := &strongAdvisorProvider{}
	provider.Register("boot-advisor-strong", func(provider.Config) (provider.Provider, error) { return strong, nil })
	rec := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("a1", "advise", map[string]any{"question": "Which lock goes first?"})
		},
	}}
	kind := "boot-advisor-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	advisorEffectConfig(t, dir, kind, `advisor_model = "strong"`)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ctrl.Run(context.Background(), "fix the deadlock in the store"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()
	reqs := agentRequests(rec.requests())
	if !toolNames(reqs[0])["advise"] {
		t.Fatalf("advise missing from the provider-visible schema: %v", toolSchemaNames(reqs[0].Tools))
	}
	strong.mu.Lock()
	sent := append([]provider.Request(nil), strong.reqs...)
	strong.mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("advisor requests = %d, want 1", len(sent))
	}
	evidence := sent[0].Messages[len(sent[0].Messages)-1].Content
	if !strings.Contains(evidence, "fix the deadlock in the store") || !strings.Contains(evidence, "Which lock goes first?") {
		t.Fatalf("the advisor did not see the task and the question:\n%s", evidence)
	}
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 1 || !strings.Contains(results[0], "ADVICE: take the store lock") {
		t.Fatalf("tool results = %q, want the advisor's answer", results)
	}
}

func TestEffectAdviseIsAbsentWithoutAnAdvisorModel(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &browserScriptProvider{}
	kind := "boot-advisor-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	advisorEffectConfig(t, dir, kind, "")
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ctrl.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()
	if reqs := agentRequests(rec.requests()); toolNames(reqs[0])["advise"] {
		t.Fatal("advise offered with no advisor_model")
	}
}
