package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/permission"
)

// browserScriptProvider plays a fixed browsing session: each round's call is
// built from what the previous tool result showed the model.
type browserScriptProvider struct {
	mu     sync.Mutex
	rounds []func(lastResult string) *provider.ToolCall
	reqs   []provider.Request
}

func (p *browserScriptProvider) Name() string { return "boot-browser-script" }

func (p *browserScriptProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	round := len(p.reqs) - 1
	p.mu.Unlock()
	last := ""
	if results := effectToolResults(req); len(results) > 0 {
		last = results[len(results)-1]
	}
	ch := make(chan provider.Chunk, 2)
	if round < len(p.rounds) {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: p.rounds[round](last)}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *browserScriptProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func browserCall(id, name string, args any) *provider.ToolCall {
	raw, _ := json.Marshal(args)
	return &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}
}

func buildBrowserEffect(t *testing.T, browserSection string, rounds []func(string) *provider.ToolCall) []provider.Request {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &browserScriptProvider{rounds: rounds}
	kind := "boot-browser-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

`+browserSection+`

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ctrl.Run(context.Background(), "use the browser"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()
	return agentRequests(rec.requests())
}

// A machine without a browser still shows the tools, and the model hears why a
// call did nothing rather than a bare failure.
func TestEffectMissingBrowserReachesTheModelAsItsCode(t *testing.T) {
	reqs := buildBrowserEffect(t, `[browser]
executable = "/nonexistent/chrome"`, []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("open-1", "browser_open", map[string]any{"url": "https://example.com/"})
		},
	})
	if !toolNames(reqs[0])["browser_open"] {
		t.Fatalf("browser tools missing from the schema: %v", toolSchemaNames(reqs[0].Tools))
	}
	var actSchema string
	for _, schema := range reqs[0].Tools {
		if schema.Name == "browser_act" {
			actSchema = string(schema.Parameters)
			break
		}
	}
	if !strings.Contains(actSchema, `"eval"`) || !strings.Contains(actSchema, `"script"`) {
		t.Fatalf("browser_act at the provider boundary cannot execute JavaScript: %s", actSchema)
	}
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) == 0 || !strings.Contains(results[0], "browser.engine_missing") {
		t.Fatalf("the model was not told the browser is missing: %q", results)
	}
}

func TestEffectDisabledBrowserLeavesTheSchema(t *testing.T) {
	reqs := buildBrowserEffect(t, "[tools]\nbrowser_tools = false", nil)
	for _, name := range BrowserToolNames() {
		if toolNames(reqs[0])[name] {
			t.Fatalf("%s is in the schema with the browser disabled", name)
		}
	}
}

// [browser] enabled belongs to the 1.x command line, which writes false on
// every save of the shared file; it must not take the tools away here.
func TestEffectSharedBrowserKeyLeavesTheToolsBound(t *testing.T) {
	reqs := buildBrowserEffect(t, "[browser]\nenabled = false", nil)
	for _, name := range BrowserToolNames() {
		if !toolNames(reqs[0])[name] {
			t.Fatalf("%s is missing from the schema: %v", name, toolSchemaNames(reqs[0].Tools))
		}
	}
}

var refPattern = regexp.MustCompile(`(?m)^\s*- (\w+) "([^"]*)" \[(e\d+)\]`)

func refFor(result, role, name string) string {
	for _, m := range refPattern.FindAllStringSubmatch(result, -1) {
		if m[1] == role && m[2] == name {
			return m[3]
		}
	}
	return ""
}

// Through the real assembly: the snapshot's refs reach the model, the refs it
// sends back operate the page, and the change comes back as a change.
func TestEffectBrowserSessionThroughTheRealAssembly(t *testing.T) {
	exe := os.Getenv("REASONIX_LIVE_BROWSER")
	if exe == "" || exe == "1" {
		t.Skip("set REASONIX_LIVE_BROWSER to a browser executable to run the live assembly test")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<title>Greeter</title><label>Name <input></label><button onclick="document.querySelector('p').textContent='Hello, '+document.querySelector('input').value">Greet</button><p></p>`)
	}))
	defer srv.Close()
	var refs struct{ name, greet string }
	reqs := buildBrowserEffect(t, fmt.Sprintf("[browser]\nexecutable = %q\nheadless = true", exe), []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("open-1", "browser_open", map[string]any{"url": srv.URL + "/"})
		},
		func(last string) *provider.ToolCall {
			refs.name, refs.greet = refFor(last, "textbox", "Name"), refFor(last, "button", "Greet")
			return browserCall("act-1", "browser_act", map[string]any{"steps": []map[string]any{
				{"action": "fill", "ref": refs.name, "text": "李雷"},
				{"action": "click", "ref": refs.greet},
			}})
		},
	})
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 2 {
		t.Fatalf("tool results at the boundary = %d, want open and act", len(results))
	}
	if refs.name == "" || refs.greet == "" {
		t.Fatalf("the snapshot the model received carried no refs:\n%s", results[0])
	}
	if !strings.Contains(results[1], "Completed 2 of 2 step(s)") || !strings.Contains(results[1], `+ - text: "Hello, 李雷"`) {
		t.Fatalf("the act result did not report the change it made:\n%s", results[1])
	}
}

// Three packages recognise a browser tool by name and cannot import the tools
// to ask: the grant group, the mutation classifier, and this schema rule. They
// have to agree with the tools that exist.
func TestBrowserToolIdentityAgreesAcrossTheKernel(t *testing.T) {
	names := BrowserToolNames()
	if len(names) != 3 {
		t.Fatalf("browser tools = %v", names)
	}
	for _, name := range names {
		if !permission.IsBrowserTool(name) {
			t.Errorf("%s is not in the browser grant group", name)
		}
		if evidence.ToolCallMutates(name, json.RawMessage(`{}`), name == "browser_read") {
			t.Errorf("%s is classified as a workspace mutation", name)
		}
	}
}
