package boot

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type jobListProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *jobListProvider) Name() string { return "boot-effect-joblist" }

func (p *jobListProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	round := len(p.reqs)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	switch round {
	case 1:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "c1", Name: "bash", Arguments: `{"command":"sleep 30","run_in_background":true}`}}
	case 2:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "c2", Name: "bash_output", Arguments: `{}`}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// TestEffectBashOutputListsRunningJobsThroughRealBuild pins what the model is
// told about its own background work: the host's enumeration of running jobs
// reaches the next provider request, instead of a count kept in the model's head.
func TestEffectBashOutputListsRunningJobsThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &jobListProvider{}
	resolver := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "test-model/x", Model: "x", Default: true}},
		Providers:   map[string]provider.Provider{"test-model/x": rec},
	}
	writeUserConfig(t, "[sandbox]\nbash = \"off\"\n")
	writeFile(t, dir, "reasonix.toml", `
[agent]
system_prompt = "BASE"
`)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, Model: "test-model/x", ProviderResolver: resolver})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if got := ctrl.RuntimeSelection().ModelRef; got != "test-model/x" {
		t.Fatalf("built model = %q, want scripted test-model/x", got)
	}
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "joblist.jsonl"))
	ctrl.SetBypass(true)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := ctrl.Run(ctx, "start a job and list jobs"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.reqs) < 3 {
		t.Fatalf("provider saw %d rounds, want 3", len(rec.reqs))
	}
	var launch string
	for _, m := range rec.reqs[1].Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == "c1" {
			launch = m.Content
		}
	}
	if !strings.HasPrefix(launch, "Started background job ") {
		t.Fatalf("bash background launch result = %q", launch)
	}
	if !strings.Contains(launch, "a new turn starts with its status") {
		t.Fatalf("launch result = %q, want the host's resume-on-completion note", launch)
	}
	var result string
	for _, m := range rec.reqs[2].Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == "c2" {
			result = m.Content
		}
	}
	if !strings.Contains(result, "Running background jobs (1)") || !strings.Contains(result, "sleep 30") {
		t.Fatalf("bash_output {} result = %q", result)
	}
	for _, tl := range rec.reqs[0].Tools {
		if tl.Name == "bash_output" && strings.Contains(string(tl.Parameters), `"job_id"]`) {
			t.Fatalf("bash_output still requires job_id: %s", tl.Parameters)
		}
	}
}
