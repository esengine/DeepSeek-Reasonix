package boot

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// callingProvider calls one named tool, then ends once it has seen the result.
type callingProvider struct {
	mu   sync.Mutex
	call string
	reqs []provider.Request
}

func (p *callingProvider) Name() string { return "boot-calling" }

func (p *callingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	done := len(effectToolResults(req)) > 0
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if done {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "c-1", Name: p.call, Arguments: `{}`}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var (
	callingOnce sync.Once
	callingMu   sync.Mutex
	callingCur  *callingProvider
)

func registerCalling(p *callingProvider) {
	callingMu.Lock()
	callingCur = p
	callingMu.Unlock()
	callingOnce.Do(func() {
		provider.Register("boot-calling", func(provider.Config) (provider.Provider, error) {
			callingMu.Lock()
			defer callingMu.Unlock()
			return callingCur, nil
		})
	})
}

func disabledToolsBuild(t *testing.T, userConfig, call string) (*callingProvider, *askSinkRecorder) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &callingProvider{call: call}
	registerCalling(rec)
	writeUserConfig(t, userConfig)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-calling"
model = "x"
`)
	approveWorkspace(t, dir)
	sink := &askSinkRecorder{}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "search"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return rec, sink
}

// A tool named in [tools].disabled leaves every request the model is sent, and a
// call to it anyway is refused with the reason, through the real assembly.
func TestEffectDisabledToolLeavesTheRequestAndIsRefusedWhenCalled(t *testing.T) {
	rec, _ := disabledToolsBuild(t, "[tools]\ndisabled = [\"recall\"]\n", "recall")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	first := requestToolNames(rec.reqs[0])
	if slices.Contains(first, "recall") {
		t.Fatalf("the request still offers recall: %v", first)
	}
	if !slices.Contains(first, "bash") || !slices.Contains(first, "read_file") {
		t.Fatalf("the request lost tools nobody disabled: %v", first)
	}
	results := effectToolResults(rec.reqs[len(rec.reqs)-1])
	if len(results) != 1 || !strings.Contains(results[0], `tool "recall" is turned off`) {
		t.Fatalf("the model's result for the forced call = %q, want the disabled refusal", results)
	}

	enabled, _ := disabledToolsBuild(t, "", "recall")
	enabled.mu.Lock()
	defer enabled.mu.Unlock()
	if !slices.Contains(requestToolNames(enabled.reqs[0]), "recall") {
		t.Fatalf("without the setting recall is not offered: %v", requestToolNames(enabled.reqs[0]))
	}
}

// A tool the host needs stays on and the user is told which feature needs it.
func TestEffectDisabledToolTheHostNeedsStaysAndSaysSo(t *testing.T) {
	rec, sink := disabledToolsBuild(t, "[tools]\ndisabled = [\"use_capability\", \"recall\"]\n", "bash")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	names := requestToolNames(rec.reqs[0])
	if !slices.Contains(names, "use_capability") || slices.Contains(names, "recall") {
		t.Fatalf("request tools = %v, want use_capability kept and recall removed", names)
	}
	if !sink.has(func(ev event.Event) bool {
		return ev.Level == event.LevelWarn && strings.Contains(ev.Text, "use_capability") && strings.Contains(ev.Text, "deferred MCP tools")
	}) {
		t.Fatal("the user was not told the host kept use_capability")
	}
}

// The tools the loop itself reads stay on when a project file names them, and
// the user is told which feature needs each.
func TestEffectDisabledToolsTheLoopReadsStayAndSaySo(t *testing.T) {
	kept := []string{"complete_step", "todo_write", "await_user"}
	rec, sink := disabledToolsBuild(t, "[tools]\ndisabled = [\"complete_step\", \"todo_write\", \"await_user\", \"recall\"]\n", "bash")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	names := requestToolNames(rec.reqs[0])
	for _, name := range kept {
		if !slices.Contains(names, name) {
			t.Fatalf("request tools = %v, want %s kept", names, name)
		}
		if !sink.has(func(ev event.Event) bool {
			return ev.Level == event.LevelWarn && strings.Contains(ev.Text, name+" (needed for ")
		}) {
			t.Fatalf("the user was not told the host kept %s", name)
		}
	}
	if slices.Contains(names, "recall") {
		t.Fatalf("recall is not needed by the host and should be gone: %v", names)
	}
	if !sink.has(func(ev event.Event) bool {
		return strings.Contains(ev.Text, "plan step tracking") && strings.Contains(ev.Text, "ending a turn the host left open")
	}) {
		t.Fatal("the warning does not name the features")
	}
}
