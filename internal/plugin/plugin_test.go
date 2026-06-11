package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

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
