package boot

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/browser"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// bootBrowserExecutor is the smallest Executor a boot can attach: it lists
// fixed tabs and refuses everything else.
type bootBrowserExecutor struct{ tabs []browser.Tab }

func (b bootBrowserExecutor) Tabs(context.Context) ([]browser.Tab, error) { return b.tabs, nil }
func (bootBrowserExecutor) Open(context.Context, browser.OpenRequest) (browser.Tab, error) {
	return browser.Tab{}, browser.ErrNoGrant
}
func (bootBrowserExecutor) Navigate(context.Context, browser.NavigateRequest) (browser.Tab, error) {
	return browser.Tab{}, browser.ErrNoGrant
}
func (bootBrowserExecutor) Snapshot(context.Context, browser.SnapshotRequest) (browser.Snapshot, error) {
	return browser.Snapshot{}, browser.ErrNoGrant
}
func (bootBrowserExecutor) Screenshot(context.Context, browser.ScreenshotRequest) (browser.Screenshot, error) {
	return browser.Screenshot{}, browser.ErrNoGrant
}
func (bootBrowserExecutor) Act(context.Context, browser.ActRequest) (browser.ActResult, error) {
	return browser.ActResult{}, browser.ErrNoGrant
}
func (bootBrowserExecutor) Downloads(context.Context, browser.DownloadsRequest) ([]browser.Download, error) {
	return nil, browser.ErrNoGrant
}
func (bootBrowserExecutor) Close(context.Context, browser.CloseRequest) error {
	return browser.ErrNoGrant
}

const browserBootConfig = `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`

type browserSurface struct {
	prompt   string
	tools    []string
	visible  []string
	registry []string
}

func captureBrowserSurface(t *testing.T, exec browser.Executor) browserSurface {
	t.Helper()
	prov := testutil.NewMock("browser-surface", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, BrowserExecutor: exec})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := mainConversationRequests(prov.Requests())
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	s := browserSurface{prompt: systemMessage(ctrl.History()), tools: toolSchemaNames(reqs[0].Tools)}
	for _, e := range ctrl.ToolContractEntries() {
		s.visible = append(s.visible, e.Name)
	}
	for _, e := range ctrl.AllToolContractEntries() {
		s.registry = append(s.registry, e.Name)
	}
	return s
}

// TestBrowserToolsStayOffTheProviderSurface is the cache guard for the
// browser capability: attaching an executor registers the tools for
// use_capability but leaves the provider request's tool array and the
// system prompt byte-identical to a build without one.
func TestBrowserToolsStayOffTheProviderSurface(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", browserBootConfig)
	registerBootTokenProfileTestProvider()

	without := captureBrowserSurface(t, nil)
	with := captureBrowserSurface(t, bootBrowserExecutor{})

	for _, name := range browser.Names() {
		if !slices.Contains(with.registry, name) {
			t.Errorf("registry with executor lacks %s: %v", name, with.registry)
		}
		if slices.Contains(without.registry, name) {
			t.Errorf("registry without executor registered %s", name)
		}
		if slices.Contains(with.visible, name) || slices.Contains(with.tools, name) {
			t.Errorf("%s leaked into the provider-visible surface", name)
		}
	}
	if !slices.Equal(with.tools, without.tools) {
		t.Fatalf("provider tool array changed with a browser attached:\nwith    %v\nwithout %v", with.tools, without.tools)
	}
	if with.prompt != without.prompt {
		t.Fatalf("system prompt changed with a browser attached: %s", firstDivergence(without.prompt, with.prompt))
	}
}

func TestUseCapabilityListsAndCallsBrowserTools(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", browserBootConfig)
	registerBootTokenProfileTestProvider()
	listArgs, _ := json.Marshal(map[string]any{"action": "list"})
	callArgs, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:browser_tabs", "arguments": map[string]any{}})
	prov := testutil.NewMock("browser-ucap",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "l1", Name: "use_capability", Arguments: string(listArgs)}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "use_capability", Arguments: string(callArgs)}}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	exec := bootBrowserExecutor{tabs: []browser.Tab{{ID: "tab-1", URL: "https://example.test", Title: "Example"}}}
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, BrowserExecutor: exec})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "list the browser tabs"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var toolOut []string
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool {
			toolOut = append(toolOut, msg.Content)
		}
	}
	if len(toolOut) != 2 {
		t.Fatalf("tool outputs = %d, want 2: %q", len(toolOut), toolOut)
	}
	if !strings.Contains(toolOut[0], "tool:browser_snapshot") {
		t.Fatalf("use_capability list omits browser_snapshot:\n%s", toolOut[0])
	}
	if !strings.Contains(toolOut[1], "tab tab-1: https://example.test") {
		t.Fatalf("use_capability call did not reach the executor:\n%s", toolOut[1])
	}
	for _, req := range prov.Requests() {
		if requestHasToolPrefix(req, "browser_") {
			t.Fatalf("browser tool leaked into provider schemas: %v", toolSchemaNames(req.Tools))
		}
	}
}
