package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

type countingToolsTransport struct {
	mu    sync.Mutex
	calls int
	raw   json.RawMessage
}

func (t *countingToolsTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if method != "tools/list" {
		return json.RawMessage(`{}`), nil
	}
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	if len(t.raw) > 0 {
		return t.raw, nil
	}
	return json.RawMessage(`{"tools":[{"name":"zed","description":"Sorted after echo.","inputSchema":{"type":"object"}},{"name":"echo","description":"Echo back the message.","inputSchema":{"type":"object","properties":{"msg":{"type":"string"}},"required":["z","msg"]},"annotations":{"readOnlyHint":true}}]}`), nil
}

func (t *countingToolsTransport) notify(ctx context.Context, method string, params any) error {
	return nil
}
func (t *countingToolsTransport) close() {}

func (t *countingToolsTransport) toolsListCalls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

type deadlineRecordingTransport struct {
	mu        sync.Mutex
	deadline  []time.Duration
	methods   []string
	block     bool
	noContext bool
}

func (t *deadlineRecordingTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if d, ok := ctx.Deadline(); ok {
		t.mu.Lock()
		t.deadline = append(t.deadline, time.Until(d))
		t.methods = append(t.methods, method)
		t.mu.Unlock()
	} else {
		t.mu.Lock()
		t.noContext = true
		t.methods = append(t.methods, method)
		t.mu.Unlock()
	}
	if t.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return json.RawMessage(`{}`), nil
}

func (t *deadlineRecordingTransport) notify(ctx context.Context, method string, params any) error {
	return nil
}
func (t *deadlineRecordingTransport) close() {}

func (t *deadlineRecordingTransport) lastDeadline(tst *testing.T) time.Duration {
	tst.Helper()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.deadline) == 0 {
		tst.Fatalf("transport recorded no deadline; methods=%v noContext=%v", t.methods, t.noContext)
	}
	return t.deadline[len(t.deadline)-1]
}

func assertDeadlineNear(t *testing.T, got, want time.Duration) {
	t.Helper()
	if got < want-2*time.Second || got > want+2*time.Second {
		t.Fatalf("deadline = %v, want near %v", got, want)
	}
}

func TestClientCallAppliesBuiltInDefaultTimeout(t *testing.T) {
	for _, transportName := range []string{"stdio", "http"} {
		t.Run(transportName, func(t *testing.T) {
			tr := &deadlineRecordingTransport{}
			c := &Client{name: "maker", t: tr, spec: Spec{Name: "maker"}, transport: transportName}
			if _, err := c.call(context.Background(), "tools/list", map[string]any{}); err != nil {
				t.Fatalf("call: %v", err)
			}
			assertDeadlineNear(t, tr.lastDeadline(t), defaultCallTimeout)
		})
	}
}

func TestClientCallTimeoutPrecedence(t *testing.T) {
	tr := &deadlineRecordingTransport{}
	c := &Client{
		name: "maker",
		t:    tr,
		spec: Spec{
			Name:               "maker",
			DefaultCallTimeout: 300 * time.Second,
			CallTimeout:        600 * time.Second,
			ToolTimeouts:       map[string]time.Duration{"generate_video": 1800 * time.Second},
		},
		transport: "stdio",
	}

	if _, err := c.call(context.Background(), "tools/call", map[string]any{"name": "generate_video"}); err != nil {
		t.Fatalf("tool override call: %v", err)
	}
	assertDeadlineNear(t, tr.lastDeadline(t), 1800*time.Second)

	if _, err := c.call(context.Background(), "tools/call", map[string]any{"name": "search"}); err != nil {
		t.Fatalf("plugin override call: %v", err)
	}
	assertDeadlineNear(t, tr.lastDeadline(t), 600*time.Second)

	if _, err := c.call(context.Background(), "prompts/list", map[string]any{}); err != nil {
		t.Fatalf("method call: %v", err)
	}
	assertDeadlineNear(t, tr.lastDeadline(t), 600*time.Second)
}

func TestClientCallRespectsParentDeadline(t *testing.T) {
	tr := &deadlineRecordingTransport{}
	c := &Client{
		name: "maker",
		t:    tr,
		spec: Spec{
			Name:        "maker",
			CallTimeout: 10 * time.Minute,
		},
		transport: "http",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.call(ctx, "tools/call", map[string]any{"name": "generate_video"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	got := tr.lastDeadline(t)
	if got > 150*time.Millisecond {
		t.Fatalf("deadline = %v, want caller deadline around 100ms", got)
	}
}

func TestClientCallTimeoutErrorNamesToolAndConfig(t *testing.T) {
	tr := &deadlineRecordingTransport{block: true}
	c := &Client{
		name: "maker",
		t:    tr,
		spec: Spec{
			Name:        "maker",
			CallTimeout: 25 * time.Millisecond,
		},
		transport: "stdio",
	}
	_, err := c.call(context.Background(), "tools/call", map[string]any{"name": "generate_video"})
	if err == nil {
		t.Fatal("timed-out call returned nil error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error should wrap context deadline exceeded, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `MCP tool "maker.generate_video" timed out after 25ms`) ||
		!strings.Contains(msg, "tool_timeout_seconds or call_timeout_seconds") {
		t.Fatalf("timeout error lacks useful guidance: %v", err)
	}
}

// TestStdioEndToEnd drives a real subprocess (this test binary re-invoked in
// helper mode) through the full MCP handshake and a tool call, exercising
// StartAll, tools/list, and tools/call over stdio JSON-RPC.
func TestStdioEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	spec := Spec{
		Name:    "mock",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}

	host, tools, err := StartAll(ctx, []Spec{spec})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()

	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools))
	}
	if got := tools[0].Name(); got != "mcp__mock__echo" {
		t.Fatalf("tool name: want mcp__mock__echo, got %q", got)
	}
	if got, want := string(tools[0].Schema()), `{"properties":{"msg":{"type":"string"}},"required":["msg","z"],"type":"object"}`; got != want {
		t.Fatalf("tool schema = %s, want %s", got, want)
	}

	out, err := tools[0].Execute(ctx, json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "echo: hi" {
		t.Fatalf("result: want %q, got %q", "echo: hi", out)
	}
}

func TestHostToolsForReusesCachedTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tr := &countingToolsTransport{}
	host := NewHost()
	defer host.Close()
	host.clients = []*Client{{
		name:      "mock",
		t:         tr,
		spec:      Spec{Name: "mock"},
		transport: "stdio",
	}}

	first, err := host.ToolsFor(ctx, "mock")
	if err != nil {
		t.Fatalf("first ToolsFor: %v", err)
	}
	second, err := host.ToolsFor(ctx, "mock")
	if err != nil {
		t.Fatalf("second ToolsFor: %v", err)
	}
	if got := tr.toolsListCalls(); got != 1 {
		t.Fatalf("tools/list calls = %d, want 1", got)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("ToolsFor lengths = %d and %d, want 2 each", len(first), len(second))
	}
	if got := first[0].Name(); got != "mcp__mock__echo" {
		t.Fatalf("first tool name = %q, want sorted echo first", got)
	}
	if got, want := string(second[0].Schema()), string(first[0].Schema()); got != want {
		t.Fatalf("cached schema changed:\n first=%s\nsecond=%s", want, got)
	}
	if !second[0].ReadOnly() {
		t.Fatal("cached tool lost readOnlyHint")
	}

	statuses := host.Servers()
	if len(statuses) != 1 || len(statuses[0].ToolList) != 2 {
		t.Fatalf("server tool status = %+v, want cached tool metadata", statuses)
	}
	if statuses[0].ToolList[0].Name != "echo" || !statuses[0].ToolList[0].ReadOnlyHint {
		t.Fatalf("tool metadata = %+v, want sorted echo with readOnlyHint", statuses[0].ToolList)
	}
}

func TestHostToolsForCachesEmptyToolList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tr := &countingToolsTransport{raw: json.RawMessage(`{"tools":[]}`)}
	host := NewHost()
	defer host.Close()
	host.clients = []*Client{{
		name:      "empty",
		t:         tr,
		spec:      Spec{Name: "empty"},
		transport: "stdio",
	}}

	first, err := host.ToolsFor(ctx, "empty")
	if err != nil {
		t.Fatalf("first ToolsFor: %v", err)
	}
	second, err := host.ToolsFor(ctx, "empty")
	if err != nil {
		t.Fatalf("second ToolsFor: %v", err)
	}
	if len(first) != 0 || len(second) != 0 {
		t.Fatalf("ToolsFor lengths = %d and %d, want 0 each", len(first), len(second))
	}
	if got := tr.toolsListCalls(); got != 1 {
		t.Fatalf("empty tools/list calls = %d, want 1", got)
	}
}

func TestSpecReadOnlyToolNamesMarksUnhintedToolsReadOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	spec := Spec{
		Name:    "mock",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
		ReadOnlyToolNames: map[string]bool{
			"echo": true,
		},
	}

	host, tools, err := StartAll(ctx, []Spec{spec})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()

	byName := map[string]tool.Tool{}
	for _, tl := range tools {
		byName[tl.Name()] = tl
	}
	echo := byName["mcp__mock__echo"]
	if echo == nil {
		t.Fatalf("mcp__mock__echo missing from %v", byName)
	}
	if !echo.ReadOnly() {
		t.Fatal("read-only override did not mark unhinted echo tool read-only")
	}
	zed := byName["mcp__mock__zed"]
	if zed == nil {
		t.Fatalf("mcp__mock__zed missing from %v", byName)
	}
	if zed.ReadOnly() {
		t.Fatal("read-only override should not mark non-listed tools read-only")
	}
}

func TestSpecReadOnlyModelToolNamesMarksVisibleToolsTrusted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	spec := Spec{
		Name:    "mock",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
		ReadOnlyModelToolNames: map[string]bool{
			"mcp__mock__echo": true,
		},
	}

	host, tools, err := StartAll(ctx, []Spec{spec})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()

	byName := map[string]tool.Tool{}
	for _, tl := range tools {
		byName[tl.Name()] = tl
	}
	echo := byName["mcp__mock__echo"]
	if echo == nil {
		t.Fatalf("mcp__mock__echo missing from %v", byName)
	}
	if !echo.ReadOnly() {
		t.Fatal("model-visible read-only override did not mark echo tool read-only")
	}
	if u, ok := echo.(tool.PlanModeUntrustedReadOnly); ok && u.PlanModeUntrustedReadOnly() {
		t.Fatal("model-visible read-only override should be trusted in plan mode")
	}
	zed := byName["mcp__mock__zed"]
	if zed == nil {
		t.Fatalf("mcp__mock__zed missing from %v", byName)
	}
	if zed.ReadOnly() {
		t.Fatal("model-visible read-only override should not mark non-listed tools read-only")
	}
}

func TestApplyKnownReadOnlyOverridesMarksCodeGraphReadTools(t *testing.T) {
	got := ApplyKnownReadOnlyOverrides(Spec{Name: "codegraph", ReadOnlyToolNames: map[string]bool{"custom": true}})
	for _, name := range []string{"custom", "codegraph_context", "codegraph_search", "context", "search"} {
		if !got.ReadOnlyToolNames[name] {
			t.Fatalf("codegraph read-only override missing %q: %+v", name, got.ReadOnlyToolNames)
		}
	}

	other := ApplyKnownReadOnlyOverrides(Spec{Name: "not-codegraph"})
	if other.ReadOnlyToolNames["codegraph_context"] {
		t.Fatalf("non-codegraph spec should not receive codegraph overrides: %+v", other.ReadOnlyToolNames)
	}
}

func TestApplyKnownOverridesPinsCodeGraphStdioToWorkspace(t *testing.T) {
	got := ApplyKnownOverrides(Spec{Name: "codegraph"}, "/workspace")
	if got.Dir != "/workspace" {
		t.Fatalf("codegraph stdio Dir = %q, want workspace root", got.Dir)
	}
	if !got.ReadOnlyToolNames["codegraph_search"] {
		t.Fatalf("codegraph read-only override missing: %+v", got.ReadOnlyToolNames)
	}
	if got.Env[codeGraphDaemonIdleTimeoutEnv] != codeGraphDaemonIdleTimeoutDefaultMS {
		t.Fatalf("codegraph daemon idle timeout env = %q, want %s; env=%v", got.Env[codeGraphDaemonIdleTimeoutEnv], codeGraphDaemonIdleTimeoutDefaultMS, got.Env)
	}

	preset := ApplyKnownOverrides(Spec{Name: "codegraph", Dir: "/custom"}, "/workspace")
	if preset.Dir != "/custom" {
		t.Fatalf("existing Dir should be preserved, got %q", preset.Dir)
	}

	httpSpec := ApplyKnownOverrides(Spec{Name: "codegraph", Type: "http"}, "/workspace")
	if httpSpec.Dir != "" {
		t.Fatalf("http codegraph should not receive stdio Dir, got %q", httpSpec.Dir)
	}
	if _, ok := httpSpec.Env[codeGraphDaemonIdleTimeoutEnv]; ok {
		t.Fatalf("http codegraph should not receive daemon idle env, got %+v", httpSpec.Env)
	}

	other := ApplyKnownOverrides(Spec{Name: "other"}, "/workspace")
	if other.Dir != "" {
		t.Fatalf("non-codegraph should not receive Dir, got %q", other.Dir)
	}
	if _, ok := other.Env[codeGraphDaemonIdleTimeoutEnv]; ok {
		t.Fatalf("non-codegraph should not receive daemon idle env, got %+v", other.Env)
	}
}

func TestApplyKnownOverridesPinsCodebaseMemoryToWorkspace(t *testing.T) {
	got := ApplyKnownOverrides(Spec{Name: "codebase-memory-mcp"}, "/workspace")
	if got.Dir != "/workspace" {
		t.Fatalf("codebase-memory-mcp stdio Dir = %q, want workspace root", got.Dir)
	}
	if !got.LowPriority {
		t.Fatalf("codebase-memory-mcp should run at low priority")
	}

	preset := ApplyKnownOverrides(Spec{Name: "codebase-memory-mcp", Dir: "/custom"}, "/workspace")
	if preset.Dir != "/custom" {
		t.Fatalf("existing Dir should be preserved, got %q", preset.Dir)
	}

	httpSpec := ApplyKnownOverrides(Spec{Name: "codebase-memory-mcp", Type: "http"}, "/workspace")
	if httpSpec.Dir != "" {
		t.Fatalf("http codebase-memory-mcp should not receive stdio Dir, got %q", httpSpec.Dir)
	}

	npxSpec := ApplyKnownOverrides(Spec{
		Name:    "custom",
		Command: "npx",
		Args:    []string{"-y", "codebase-memory-mcp@latest"},
	}, "/workspace")
	if npxSpec.Dir != "/workspace" || !npxSpec.LowPriority {
		t.Fatalf("npx codebase-memory-mcp override missing: %+v", npxSpec)
	}
}

func TestApplyKnownOverridesPreservesConfiguredCodeGraphDaemonIdleTimeout(t *testing.T) {
	got := ApplyKnownOverrides(Spec{
		Name: "codegraph",
		Env:  map[string]string{codeGraphDaemonIdleTimeoutEnv: "30000"},
	}, "/workspace")

	if got.Env[codeGraphDaemonIdleTimeoutEnv] != "30000" {
		t.Fatalf("configured codegraph daemon idle timeout was overwritten: %+v", got.Env)
	}
}

func TestStartAvailableKeepsGoodServers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	good := Spec{
		Name:    "good",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
	bad := Spec{Name: "bad", Command: "reasonix-missing-mcp-binary"}

	host, tools := StartAvailable(ctx, []Spec{bad, good})
	defer host.Close()

	if len(tools) != 2 {
		t.Fatalf("want tools from the good server, got %d", len(tools))
	}
	if got := host.ServerNames(); len(got) != 1 || got[0] != "good" {
		t.Fatalf("connected servers = %v, want [good]", got)
	}
	failures := host.Failures()
	if len(failures) != 1 || failures[0].Name != "bad" {
		t.Fatalf("failures = %+v, want bad", failures)
	}
}

// TestStartAllAllOrNothingOnFailure pins the strict StartAll contract the
// parallel rewrite must preserve: any single plugin failing aborts the whole
// set, returns no Host or tools, and tears down every server that did start —
// including, under parallel start, a good server whose index sits after the
// failing one ([bad, good]). On error the Host is nil, so callers never see a
// half-built set; the started servers are closed before StartAll returns.
func TestStartAllAllOrNothingOnFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	good := Spec{
		Name:    "good",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
	bad := Spec{Name: "bad", Command: "reasonix-missing-mcp-binary"}

	for _, tc := range []struct {
		name  string
		specs []Spec
	}{
		{"failure first", []Spec{bad, good}},
		{"failure last", []Spec{good, bad}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, tools, err := StartAll(ctx, tc.specs)
			if err == nil {
				if host != nil {
					host.Close()
				}
				t.Fatal("StartAll should fail when a plugin can't start")
			}
			if host != nil || tools != nil {
				t.Fatalf("failed StartAll must return nil host/tools, got host=%v tools=%d", host, len(tools))
			}
		})
	}
}

func TestStdioFailureCapturesStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, _ := StartAvailable(ctx, []Spec{{
		Name:    "stderr",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_STDERR_EXIT": "1"},
	}})
	defer host.Close()

	failures := host.Failures()
	if len(failures) != 1 {
		t.Fatalf("failures = %+v, want one", failures)
	}
	if !strings.Contains(failures[0].Error, "helper stderr boom") {
		t.Fatalf("failure should include stderr, got %q", failures[0].Error)
	}
}

func TestStdioUsesConfiguredPATHForCommandLookup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dir, command := helperLauncher(t, "mock-mcp")
	t.Setenv("PATH", "")

	host, tools, err := StartAll(ctx, []Spec{{
		Name:    "path",
		Command: command,
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
			"PATH":                   dir,
		},
	}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()
	if len(tools) != 2 {
		t.Fatalf("want helper tools, got %d", len(tools))
	}
}

func TestStdioFallsBackToShellPATHForCommandLookup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dir, command := helperLauncher(t, "shell-mcp")
	t.Setenv("PATH", "")
	old := stdioShellPATH
	stdioShellPATH = func(context.Context) string { return dir }
	t.Cleanup(func() { stdioShellPATH = old })

	host, tools, err := StartAll(ctx, []Spec{{
		Name:    "shell-path",
		Command: command,
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()
	if len(tools) != 2 {
		t.Fatalf("want helper tools, got %d", len(tools))
	}
}

func TestStdioCommandNotFoundSuggestsPATHFix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Setenv("PATH", "")
	old := stdioShellPATH
	stdioShellPATH = func(context.Context) string { return "" }
	t.Cleanup(func() { stdioShellPATH = old })

	host, _ := StartAvailable(ctx, []Spec{{Name: "missing", Command: "reasonix-missing-mcp-binary"}})
	defer host.Close()

	failures := host.Failures()
	if len(failures) != 1 {
		t.Fatalf("failures = %+v, want one", failures)
	}
	msg := failures[0].Error
	for _, want := range []string{
		`command "reasonix-missing-mcp-binary" not found on PATH`,
		"absolute command path",
		"MCP server env",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("failure %q missing %q", msg, want)
		}
	}
}

func TestStdioIgnoresRelativePATHEntries(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	name := "mock-mcp"
	target := filepath.Join(bin, name)
	env := []string{"PATH=bin"}
	if runtime.GOOS == "windows" {
		target += ".cmd"
		env = append(env, "PATHEXT=.CMD")
	}
	if err := os.WriteFile(target, []byte(""), 0o755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	t.Chdir(dir)

	if exe, ok := lookPathInEnv(name, env); ok {
		t.Fatalf("relative PATH entry resolved to %q; want no match", exe)
	}
}

func helperLauncher(t *testing.T, name string) (dir, command string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell launcher fixture is POSIX-only")
	}
	dir = t.TempDir()
	command = name
	target := filepath.Join(dir, name)
	script := "#!/bin/sh\nexec " + shellQuote(os.Args[0]) + " \"$@\"\n"
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatalf("write helper launcher: %v", err)
	}
	return dir, command
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// TestStartPolicyConcurrencyCap verifies the semaphore-style cap: with
// Concurrency=1 the handshakes must serialise even though every spec runs
// in its own goroutine. We sleep briefly inside each helper's initialize so
// the goroutines have a chance to overlap if the cap is broken, then assert
// that observed max-in-flight never exceeded 1.
func TestStartPolicyConcurrencyCap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mk := func(name string) Spec {
		return Spec{
			Name:    name,
			Command: os.Args[0],
			Args:    []string{"-test.run=TestHelperProcess", "--"},
			Env: map[string]string{
				"GO_WANT_HELPER_PROCESS": "1",
				"GO_WANT_HELPER_INIT_MS": "50",
			},
		}
	}
	specs := []Spec{mk("a"), mk("b"), mk("c"), mk("d")}
	t0 := time.Now()
	host, tools, err := Start(ctx, specs, StartPolicy{Concurrency: 1, AbortOnError: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer host.Close()
	elapsed := time.Since(t0)
	// 4 specs × 50ms init each, serialised. Allow generous slack for CI.
	if elapsed < 4*50*time.Millisecond {
		t.Fatalf("with Concurrency=1, total time should be ≥ Σ(per-spec) but was %v", elapsed)
	}
	if len(tools) != 4*2 { // helper exposes 2 tools per server
		t.Fatalf("want %d tools, got %d", 4*2, len(tools))
	}
}

// TestStartPolicyPerPluginTimeout verifies that one slow plugin can't take
// down the whole batch in StartAvailable mode: the slow spec times out and
// gets recorded as a failure while the fast one connects.
func TestStartPolicyPerPluginTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fast := Spec{
		Name:    "fast",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
	slow := Spec{
		Name:    "slow",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
			"GO_WANT_HELPER_INIT_MS": "5000", // 5s, well past the 2s budget
		},
	}
	host, tools, err := Start(ctx, []Spec{fast, slow}, StartPolicy{
		PerPluginTimeout: 2 * time.Second,
		Concurrency:      2,
		AbortOnError:     false,
	})
	if err != nil {
		t.Fatalf("Start should not return err in record-failure mode: %v", err)
	}
	defer host.Close()
	// Regression: the per-plugin timeout context must NOT bound the long-lived
	// stdio child. If transport was bound to cctx instead of the parent ctx, the
	// goroutine's deferred cancel would kill `fast`'s subprocess at handshake
	// success and this Execute would fail. We invoke it explicitly here so any
	// future re-introduction of the bug breaks loudly.
	if len(tools) > 0 {
		if _, callErr := tools[0].Execute(ctx, json.RawMessage(`{"msg":"hi"}`)); callErr != nil {
			t.Fatalf("fast plugin's subprocess was killed by deferred timeout cancel: %v", callErr)
		}
	}
	if len(tools) != 2 { // fast contributes 2 tools
		t.Fatalf("want only fast's 2 tools, got %d", len(tools))
	}
	failures := host.Failures()
	if len(failures) != 1 || failures[0].Name != "slow" {
		t.Fatalf("failures = %+v, want [slow]", failures)
	}
}

func TestStartRecordsTimeoutStats(t *testing.T) {
	withTempCache(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	slow := Spec{
		Name:    "slow-stats",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
			"GO_WANT_HELPER_INIT_MS": "300",
		},
	}
	for i := 0; i < 3; i++ {
		host, _, err := Start(ctx, []Spec{slow}, StartPolicy{
			PerPluginTimeout: 50 * time.Millisecond,
			Concurrency:      1,
			AbortOnError:     false,
		})
		if err != nil {
			t.Fatalf("Start #%d: %v", i, err)
		}
		host.Close()
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		rec := Recommend("slow-stats", 50*time.Millisecond, 3)
		if rec.Demote {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout samples did not trigger demote; stats=%+v rec=%+v", readStats(t, "slow-stats"), rec)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestStartPhaseAReturnsBeforePhaseB pins the two-phase handshake contract.
// The helper advertises prompts and stalls prompts/list by 200ms; StartAvailable
// must return with tools ready while the prompts surface is still empty, and the
// prompts must only materialise on Host after StartPhaseB has been called and
// drained — proving prompts ride the background phase, not the boot critical path.
func TestStartPhaseAReturnsBeforePhaseB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	spec := Spec{
		Name:    "mock",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS":         "1",
			"GO_WANT_HELPER_PROMPTS":         "1",
			"GO_WANT_HELPER_PROMPT_DELAY_MS": "200",
		},
	}

	host, tools := StartAvailable(ctx, []Spec{spec})
	defer host.Close()

	if len(tools) == 0 {
		t.Fatalf("want tools from helper, got 0")
	}
	// Phase A returns with tools but the prompts surface must still be empty:
	// StartAvailable never issues prompts/list (the helper stalls it 200ms), so
	// prompts can only appear after StartPhaseB drains them below. We assert this
	// deferral directly instead of timing StartAvailable — subprocess spawn plus
	// the MCP handshake make a wall-clock threshold flaky on slow CI runners.
	if got := host.Prompts(); len(got) != 0 {
		t.Fatalf("phase A must not surface prompts yet, got %d", len(got))
	}

	// Drive phase B and wait for the surface-ready event. Use a buffered channel
	// sink so the test never blocks the emitter — the event payload itself is
	// our completion signal.
	ready := make(chan event.Event, 4)
	host.StartPhaseB(ctx, event.FuncSink(func(e event.Event) {
		if e.Kind == event.MCPSurfaceReady {
			select {
			case ready <- e:
			default:
			}
		}
	}))

	select {
	case e := <-ready:
		if !strings.Contains(e.Text, "prompts ready") {
			t.Fatalf("phase B event text = %q, want it to mention prompts", e.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("phase B never fired MCPSurfaceReady for prompts")
	}

	if got := host.Prompts(); len(got) != 1 || got[0].Raw != "hello" {
		t.Fatalf("after phase B, prompts = %+v, want one named hello", got)
	}
}

func TestStartPhaseBDoesNotBlockToolCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	spec := Spec{
		Name:    "mock",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS":         "1",
			"GO_WANT_HELPER_PROMPTS":         "1",
			"GO_WANT_HELPER_PROMPT_DELAY_MS": "1000",
		},
	}

	host, tools := StartAvailable(ctx, []Spec{spec})
	defer host.Close()

	var echo tool.Tool
	for _, t := range tools {
		if t.Name() == "mcp__mock__echo" {
			echo = t
			break
		}
	}
	if echo == nil {
		t.Fatal("missing echo tool")
	}

	host.StartPhaseB(ctx, event.Discard)
	time.Sleep(50 * time.Millisecond)

	callCtx, callCancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer callCancel()
	out, err := echo.Execute(callCtx, json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("tool call should not be blocked by background prompts/list: %v", err)
	}
	if out != "echo: hi" {
		t.Fatalf("Execute result = %q, want %q", out, "echo: hi")
	}
}

// TestHelperProcess is not a real test; it acts as a minimal MCP stdio server
// when invoked by TestStdioEndToEnd. It exits before the test framework can
// print to stdout, keeping the JSON-RPC channel clean.
//
// GO_WANT_HELPER_INIT_MS optionally injects a sleep before responding to the
// initialize call, used by the timeout / concurrency tests to simulate slow
// handshakes without depending on external processes.
// GO_WANT_HELPER_PROMPTS advertises the prompts capability and registers a
// "hello" prompt; GO_WANT_HELPER_PROMPT_DELAY_MS stalls prompts/list so the
// phase-A vs phase-B split can be exercised.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_STDERR_EXIT") == "1" {
		os.Stderr.WriteString("helper stderr boom\n")
		os.Exit(2)
	}
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	var initDelay time.Duration
	if ms := os.Getenv("GO_WANT_HELPER_INIT_MS"); ms != "" {
		if v, err := time.ParseDuration(ms + "ms"); err == nil {
			initDelay = v
		}
	}

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // notification: no response
		}

		var result any
		switch req.Method {
		case "initialize":
			if initDelay > 0 {
				time.Sleep(initDelay)
			}
			caps := map[string]any{}
			if os.Getenv("GO_WANT_HELPER_PROMPTS") == "1" {
				caps["prompts"] = map[string]any{}
			}
			result = map[string]any{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]any{"name": "mock", "version": "0"},
				"capabilities":    caps,
			}
		case "prompts/list":
			if ms := os.Getenv("GO_WANT_HELPER_PROMPT_DELAY_MS"); ms != "" {
				if v, err := time.ParseDuration(ms + "ms"); err == nil && v > 0 {
					time.Sleep(v)
				}
			}
			result = map[string]any{"prompts": []map[string]any{{
				"name":        "hello",
				"description": "say hi",
				"arguments":   []map[string]any{},
			}}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "zed",
				"description": "Sorted after echo.",
				"inputSchema": map[string]any{"type": "object"},
			}, {
				"name":        "echo",
				"description": "Echo back the message.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"msg": map[string]any{"type": "string"}},
					"required":   []string{"z", "msg"},
				},
			}}}
		case "tools/call":
			var p struct {
				Arguments struct {
					Msg string `json:"msg"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			result = map[string]any{"content": []map[string]any{
				{"type": "text", "text": "echo: " + p.Arguments.Msg},
			}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		os.Stdout.Write(append(b, '\n'))
	}
}

// TestReadOnlyTrustDoesNotChangeModelVisibleSchema locks the cache invariant
// behind the MCP trust controls: marking a tool trusted read-only changes its
// execution/approval flags (ReadOnly / PlanModeUntrustedReadOnly) but must not
// alter the model-visible tool name or input schema. If trust leaked into the
// provider-visible tool list/schema, every trust toggle would break the stable
// prompt prefix and drop the prompt-cache hit rate.
func TestReadOnlyTrustDoesNotChangeModelVisibleSchema(t *testing.T) {
	startMockEcho := func(spec Spec) (*Host, map[string]tool.Tool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)
		spec.Name = "mock"
		spec.Command = os.Args[0]
		spec.Args = []string{"-test.run=TestHelperProcess", "--"}
		spec.Env = map[string]string{"GO_WANT_HELPER_PROCESS": "1"}
		host, tools, err := StartAll(ctx, []Spec{spec})
		if err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		t.Cleanup(func() { host.Close() })
		byName := map[string]tool.Tool{}
		for _, tl := range tools {
			byName[tl.Name()] = tl
		}
		return host, byName
	}

	_, untrusted := startMockEcho(Spec{})
	_, trusted := startMockEcho(Spec{ReadOnlyModelToolNames: map[string]bool{"mcp__mock__echo": true}})

	base, ok := untrusted["mcp__mock__echo"]
	if !ok {
		t.Fatalf("mcp__mock__echo missing from untrusted tools %v", untrusted)
	}
	trustedEcho, ok := trusted["mcp__mock__echo"]
	if !ok {
		t.Fatalf("mcp__mock__echo missing from trusted tools %v", trusted)
	}

	// The model-visible surface (name + schema bytes) must be byte-identical.
	if base.Name() != trustedEcho.Name() {
		t.Fatalf("trust changed model-visible tool name: %q vs %q", base.Name(), trustedEcho.Name())
	}
	if got, want := string(trustedEcho.Schema()), string(base.Schema()); got != want {
		t.Fatalf("trust changed model-visible schema bytes:\n trusted=%s\n   base=%s", got, want)
	}

	// Trust only flips the execution/approval flags.
	if base.ReadOnly() {
		t.Fatal("untrusted echo should not be read-only without a hint")
	}
	if !trustedEcho.ReadOnly() {
		t.Fatal("trusted echo should be marked read-only")
	}
	if u, ok := trustedEcho.(tool.PlanModeUntrustedReadOnly); ok && u.PlanModeUntrustedReadOnly() {
		t.Fatal("trusted echo should be trusted in plan mode")
	}
}
