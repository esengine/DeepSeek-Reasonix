package hook

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeSettings(t *testing.T, dir, json string) {
	t.Helper()
	d := filepath.Join(dir, SettingsDirname)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, SettingsFilename), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sampleSettings = `{"hooks":{"PreToolUse":[{"match":"bash","command":"echo pre"}],"Stop":[{"command":"echo stop"}]}}`

func TestLoadTrustGating(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	writeSettings(t, proj, sampleSettings)
	writeSettings(t, home, `{"hooks":{"PostToolUse":[{"command":"echo g"}]}}`)

	// Untrusted: only the global hook loads.
	got := Load(LoadOptions{ProjectRoot: proj, HomeDir: home, Trusted: false})
	if len(got) != 1 || got[0].Scope != ScopeGlobal {
		t.Fatalf("untrusted load should be global-only, got %d %+v", len(got), got)
	}
	// Trusted: project hooks (before global) load too.
	got = Load(LoadOptions{ProjectRoot: proj, HomeDir: home, Trusted: true})
	if len(got) != 3 {
		t.Fatalf("trusted load should include project + global, got %d", len(got))
	}
	if got[0].Scope != ScopeProject {
		t.Errorf("project hooks should sort first, got %s", got[0].Scope)
	}
}

func TestLoadPermissionRequestHook(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{"hooks":{"PermissionRequest":[{"match":"bash","command":"notify"}]}}`)

	got := Load(LoadOptions{HomeDir: home})
	if len(got) != 1 {
		t.Fatalf("hooks count = %d, want 1", len(got))
	}
	if got[0].Event != PermissionRequest || got[0].Match != "bash" || got[0].Command != "notify" {
		t.Fatalf("loaded hook = %+v, want PermissionRequest/bash/notify", got[0])
	}
}

func TestProjectDefinesHooks(t *testing.T) {
	proj := t.TempDir()
	if ProjectDefinesHooks(proj) {
		t.Error("empty project should define no hooks")
	}
	writeSettings(t, proj, sampleSettings)
	if !ProjectDefinesHooks(proj) {
		t.Error("project with settings.json should define hooks")
	}
}

func TestMalformedSettingsIgnored(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{not valid json`)
	if got := Load(LoadOptions{HomeDir: home}); len(got) != 0 {
		t.Errorf("malformed settings should yield no hooks, got %d", len(got))
	}
}

func TestMatchesTool(t *testing.T) {
	pre := func(match string) ResolvedHook {
		return ResolvedHook{HookConfig: HookConfig{Match: match}, Event: PreToolUse}
	}
	if MatchesTool(pre("file"), "read_file") {
		t.Error(`anchored "file" must not match "read_file"`)
	}
	if !MatchesTool(pre(".*file"), "read_file") {
		t.Error(`".*file" should match "read_file"`)
	}
	if !MatchesTool(pre("bash"), "bash") {
		t.Error(`"bash" should match "bash"`)
	}
	if !MatchesTool(pre("*"), "anything") || !MatchesTool(pre(""), "anything") {
		t.Error(`"*"/"" should match every tool`)
	}
	if MatchesTool(pre("["), "bash") {
		t.Error("malformed regex should not fire")
	}
	perm := func(match string) ResolvedHook {
		return ResolvedHook{HookConfig: HookConfig{Match: match}, Event: PermissionRequest}
	}
	if !MatchesTool(perm("bash"), "bash") {
		t.Error(`PermissionRequest "bash" should match "bash"`)
	}
	if MatchesTool(perm("bash"), "read_file") {
		t.Error(`PermissionRequest "bash" must not match "read_file"`)
	}
	if MatchesTool(perm("["), "bash") {
		t.Error("malformed PermissionRequest regex should not fire")
	}
	// Non-tool events always match regardless of the match field.
	prompt := ResolvedHook{HookConfig: HookConfig{Match: "bash"}, Event: UserPromptSubmit}
	if !MatchesTool(prompt, "") {
		t.Error("non-tool events should always match")
	}
}

func TestDecideOutcome(t *testing.T) {
	cases := []struct {
		name  string
		event Event
		r     SpawnResult
		want  Decision
	}{
		{"pass", PreToolUse, SpawnResult{ExitCode: 0}, DecisionPass},
		{"block-exit2", PreToolUse, SpawnResult{ExitCode: 2}, DecisionBlock},
		{"exit2-nonblocking-warns", PostToolUse, SpawnResult{ExitCode: 2}, DecisionWarn},
		{"permission-exit2-warns", PermissionRequest, SpawnResult{ExitCode: 2}, DecisionWarn},
		{"other-nonzero-warns", PreToolUse, SpawnResult{ExitCode: 1}, DecisionWarn},
		{"timeout-blocking", UserPromptSubmit, SpawnResult{TimedOut: true}, DecisionBlock},
		{"permission-timeout-warns", PermissionRequest, SpawnResult{TimedOut: true}, DecisionWarn},
		{"timeout-nonblocking", Stop, SpawnResult{TimedOut: true}, DecisionWarn},
		{"spawn-error", PreToolUse, SpawnResult{SpawnErr: os.ErrNotExist}, DecisionError},
	}
	for _, c := range cases {
		if got := decideOutcome(c.event, c.r); got != c.want {
			t.Errorf("%s: decideOutcome = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestRunStopsAtFirstBlock(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "first"}, Event: PreToolUse, Scope: ScopeProject},
		{HookConfig: HookConfig{Command: "second"}, Event: PreToolUse, Scope: ScopeProject},
	}
	var ran []string
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		ran = append(ran, in.Command)
		return SpawnResult{ExitCode: 2} // first blocks
	}
	rep := Run(context.Background(), Payload{Event: PreToolUse, ToolName: "bash"}, hooks, spawner)
	if !rep.Blocked {
		t.Error("report should be blocked")
	}
	if len(ran) != 1 || ran[0] != "first" {
		t.Errorf("should stop after the first block, ran %v", ran)
	}
}

func TestRunFiltersByEventAndTool(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "a", Match: "bash"}, Event: PreToolUse},
		{HookConfig: HookConfig{Command: "b", Match: "read_file"}, Event: PreToolUse},
		{HookConfig: HookConfig{Command: "c"}, Event: PostToolUse},
	}
	var ran []string
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		ran = append(ran, in.Command)
		return SpawnResult{ExitCode: 0}
	}
	Run(context.Background(), Payload{Event: PreToolUse, ToolName: "bash"}, hooks, spawner)
	if len(ran) != 1 || ran[0] != "a" {
		t.Errorf("only the matching PreToolUse hook should run, got %v", ran)
	}
}

func TestRunFiltersPermissionRequestByTool(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "a", Match: "bash"}, Event: PermissionRequest},
		{HookConfig: HookConfig{Command: "b", Match: "read_file"}, Event: PermissionRequest},
		{HookConfig: HookConfig{Command: "c"}, Event: Notification},
	}
	var ran []string
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		ran = append(ran, in.Command)
		return SpawnResult{ExitCode: 0}
	}
	Run(context.Background(), Payload{Event: PermissionRequest, ToolName: "bash"}, hooks, spawner)
	if len(ran) != 1 || ran[0] != "a" {
		t.Errorf("only the matching PermissionRequest hook should run, got %v", ran)
	}
}

func TestTrustStore(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	if IsTrusted(proj, home) {
		t.Error("project should start untrusted")
	}
	if err := Trust(proj, home); err != nil {
		t.Fatalf("trust: %v", err)
	}
	if !IsTrusted(proj, home) {
		t.Error("project should be trusted after Trust")
	}
}

func TestDefaultSpawner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	ctx := context.Background()
	// exit 0 with stdout
	r := DefaultSpawner(ctx, SpawnInput{Command: "printf hi", Timeout: 2 * time.Second})
	if r.ExitCode != 0 || r.Stdout != "hi" {
		t.Errorf("expected exit 0 / hi, got code=%d out=%q err=%v", r.ExitCode, r.Stdout, r.SpawnErr)
	}
	// exit 2 (block verdict on a gating event)
	r = DefaultSpawner(ctx, SpawnInput{Command: "exit 2", Timeout: 2 * time.Second})
	if r.ExitCode != 2 {
		t.Errorf("expected exit 2, got %d", r.ExitCode)
	}
	// stdin is delivered as the payload
	r = DefaultSpawner(ctx, SpawnInput{Command: "cat", Stdin: "payload-here", Timeout: 2 * time.Second})
	if r.Stdout != "payload-here" {
		t.Errorf("stdin not delivered: %q", r.Stdout)
	}
	// timeout kills the command
	r = DefaultSpawner(ctx, SpawnInput{Command: "sleep 5", Timeout: 100 * time.Millisecond})
	if !r.TimedOut {
		t.Errorf("expected timeout, got %+v", r)
	}
}

func TestDefaultSpawnerOutputCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	// Emit more than the cap; expect truncation flagged and bounded capture.
	r := DefaultSpawner(context.Background(), SpawnInput{
		Command: "yes x | head -c 400000",
		Timeout: 5 * time.Second,
	})
	if !r.Truncated {
		t.Error("oversized output should be flagged truncated")
	}
	if len(r.Stdout) > outputCapBytes {
		t.Errorf("captured output %d exceeds cap %d", len(r.Stdout), outputCapBytes)
	}
}
