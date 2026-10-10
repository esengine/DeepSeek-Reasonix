package hook

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func okSpawner(calls *int) Spawner {
	return func(context.Context, SpawnInput) SpawnResult {
		*calls++
		return SpawnResult{ExitCode: 0}
	}
}

func TestGatingEventsBlockWhenAHookCannotBeEvaluated(t *testing.T) {
	gating := []Event{PreToolUse, UserPromptSubmit}
	cases := []struct {
		name  string
		hook  func(Event) ResolvedHook
		pay   func(Event) Payload
		spawn Spawner
	}{
		{
			name: "invalid_matcher",
			hook: func(e Event) ResolvedHook {
				return ResolvedHook{HookConfig: HookConfig{Match: "[", Command: "true"}, Event: e}
			},
			pay: func(e Event) Payload { return Payload{Event: e, ToolName: "bash"} },
		},
		{
			name: "spawn_failed",
			hook: func(e Event) ResolvedHook {
				return ResolvedHook{HookConfig: HookConfig{Command: "missing"}, Event: e}
			},
			pay: func(e Event) Payload { return Payload{Event: e, ToolName: "bash"} },
			spawn: func(context.Context, SpawnInput) SpawnResult {
				return SpawnResult{ExitCode: -1, SpawnErr: os.ErrNotExist}
			},
		},
		{
			name: "payload_unserializable",
			hook: func(e Event) ResolvedHook {
				return ResolvedHook{HookConfig: HookConfig{Command: "true"}, Event: e}
			},
			pay: func(e Event) Payload {
				return Payload{Event: e, ToolName: "bash", ToolArgs: json.RawMessage(`{not json`)}
			},
		},
	}
	for _, c := range cases {
		for _, e := range gating {
			if c.name == "invalid_matcher" && !UsesToolMatcher(e) {
				continue
			}
			t.Run(c.name+"/"+string(e), func(t *testing.T) {
				var spawned int
				spawn := c.spawn
				if spawn == nil {
					spawn = okSpawner(&spawned)
				}
				rep := Run(context.Background(), c.pay(e), []ResolvedHook{c.hook(e)}, spawn)
				if !rep.Blocked {
					t.Fatalf("a %s hook on a gating event must block; outcomes=%+v", c.name, rep.Outcomes)
				}
				if c.name != "spawn_failed" && spawned != 0 {
					t.Fatalf("a hook that cannot be evaluated must not be spawned, spawned %d", spawned)
				}
			})
		}
	}
}

func TestObservingEventsNeverBlockWhenAHookCannotBeEvaluated(t *testing.T) {
	bad := ResolvedHook{HookConfig: HookConfig{Match: "[", Command: "true"}, Event: PostToolUse}
	rep := Run(context.Background(), Payload{Event: PostToolUse, ToolName: "bash"}, []ResolvedHook{bad}, okSpawner(new(int)))
	if rep.Blocked {
		t.Fatal("an observing event must not block on an invalid matcher")
	}
	gone := ResolvedHook{HookConfig: HookConfig{Command: "missing"}, Event: Stop}
	rep = Run(context.Background(), Payload{Event: Stop}, []ResolvedHook{gone}, func(context.Context, SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: -1, SpawnErr: os.ErrNotExist}
	})
	if rep.Blocked {
		t.Fatal("an observing event must not block on a spawn failure")
	}
	bad.Event = PostToolUse
	rep = Run(context.Background(), Payload{Event: PostToolUse, ToolName: "bash", ToolArgs: json.RawMessage(`{not json`)}, []ResolvedHook{{HookConfig: HookConfig{Command: "true"}, Event: PostToolUse}}, okSpawner(new(int)))
	if rep.Blocked {
		t.Fatal("an observing event must not block on an unserializable payload")
	}
}

func TestUnevaluableOutcomesCarryATypedCause(t *testing.T) {
	cases := []struct {
		name     string
		hook     ResolvedHook
		payload  Payload
		spawner  Spawner
		sentinel error
		code     string
	}{
		{"matcher", ResolvedHook{HookConfig: HookConfig{Match: "(", Command: "true"}, Event: PreToolUse}, Payload{Event: PreToolUse, ToolName: "bash"}, nil, ErrInvalidMatcher, CodeInvalidMatcher},
		{"spawn", ResolvedHook{HookConfig: HookConfig{Command: "x"}, Event: PreToolUse}, Payload{Event: PreToolUse}, func(context.Context, SpawnInput) SpawnResult {
			return SpawnResult{ExitCode: -1, SpawnErr: os.ErrPermission}
		}, ErrSpawnFailed, CodeSpawnFailed},
		{"payload", ResolvedHook{HookConfig: HookConfig{Command: "x"}, Event: PreToolUse}, Payload{Event: PreToolUse, ToolArgs: json.RawMessage(`{`)}, nil, ErrPayloadUnserializable, CodePayloadUnserializable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := Run(context.Background(), c.payload, []ResolvedHook{c.hook}, c.spawner)
			if len(rep.Outcomes) != 1 {
				t.Fatalf("outcomes = %d, want 1", len(rep.Outcomes))
			}
			o := rep.Outcomes[0]
			if o.Decision != DecisionBlock || !errors.Is(o.Cause, c.sentinel) || UnevaluableCode(o.Cause) != c.code {
				t.Fatalf("outcome = %s cause=%v, want block with %v", o.Decision, o.Cause, c.sentinel)
			}
		})
	}
}

func TestUnevaluableReasonGivenToTheModelIsStructuredAndScrubbed(t *testing.T) {
	const secret = "sk-abcdefghijklmnopqrstuvwxyz0123456789"
	h := ResolvedHook{
		HookConfig: HookConfig{Match: "(", Command: "run --token " + secret, Description: "guard"},
		Event:      PreToolUse, Scope: ScopeProject, Source: "/repo/.reasonix/settings.json",
	}
	r := NewRunner([]ResolvedHook{h}, "/repo", nil, func(Notice) {})
	block, msg := r.PreToolUse(context.Background(), "bash", json.RawMessage(`{}`))
	if !block {
		t.Fatal("an unevaluable PreToolUse hook must block")
	}
	for _, want := range []string{"hook_unevaluable", "code=invalid_matcher", "step=match", "event=PreToolUse", "scope=project", `hook="guard"`, "settings.json"} {
		if !strings.Contains(msg, want) {
			t.Errorf("model reason %q is missing %q", msg, want)
		}
	}
	for _, leaked := range []string{secret, "--token", "missing closing", "error parsing regexp"} {
		if strings.Contains(msg, leaked) {
			t.Errorf("model reason leaks %q: %s", leaked, msg)
		}
	}

	spawnFail := func(context.Context, SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: -1, SpawnErr: errors.New("exec: " + secret + ": not found"), Stderr: "script says " + secret}
	}
	h.Match = ""
	r = NewRunner([]ResolvedHook{h}, "/repo", spawnFail, func(Notice) {})
	block, msg = r.PreToolUse(context.Background(), "bash", json.RawMessage(`{}`))
	if !block || !strings.Contains(msg, "code=spawn_failed") || strings.Contains(msg, secret) || strings.Contains(msg, "script says") {
		t.Fatalf("spawn failure reason = %q (block=%v)", msg, block)
	}
}

func TestPromptSubmitBlocksOnAnUnevaluableHook(t *testing.T) {
	h := ResolvedHook{HookConfig: HookConfig{Command: "x"}, Event: UserPromptSubmit}
	r := NewRunner([]ResolvedHook{h}, "/repo", func(context.Context, SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: -1, SpawnErr: os.ErrNotExist}
	}, nil)
	if block, _ := r.PromptSubmit(context.Background(), "hi", 1); !block {
		t.Fatal("an unevaluable UserPromptSubmit hook must stop the turn")
	}
}

func TestUnevaluableHookDoesNotChangeOtherHooks(t *testing.T) {
	var spawned []string
	spawn := func(_ context.Context, in SpawnInput) SpawnResult {
		spawned = append(spawned, in.Command)
		return SpawnResult{ExitCode: 0}
	}
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "first"}, Event: PreToolUse},
		{HookConfig: HookConfig{Match: "read_file", Command: "other-tool"}, Event: PreToolUse},
		{HookConfig: HookConfig{Match: "[", Command: "broken"}, Event: PreToolUse},
		{HookConfig: HookConfig{Command: "after"}, Event: PreToolUse},
		{HookConfig: HookConfig{Match: "[", Command: "observer"}, Event: PostToolUse},
	}
	rep := Run(context.Background(), Payload{Event: PreToolUse, ToolName: "bash"}, hooks, spawn)
	if !rep.Blocked || !slices.Equal(spawned, []string{"first"}) {
		t.Fatalf("blocked=%v spawned=%v, want the earlier hook run and the later one held back", rep.Blocked, spawned)
	}
	if rep.Outcomes[0].Decision != DecisionPass || rep.Outcomes[1].Decision != DecisionBlock {
		t.Fatalf("outcomes = %+v", rep.Outcomes)
	}
	rep = Run(context.Background(), Payload{Event: PostToolUse, ToolName: "bash"}, hooks, spawn)
	if rep.Blocked || len(rep.Outcomes) != 1 || rep.Outcomes[0].Decision != DecisionError {
		t.Fatalf("an observer's bad matcher must be reported, not block: %+v", rep)
	}
}

func TestSaveRejectsAnInvalidMatcher(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	err := Save(ScopeGlobal, "", Settings{Hooks: map[Event][]HookConfig{PreToolUse: {{Match: "[", Command: "x"}}}})
	if !errors.Is(err, ErrInvalidMatcher) {
		t.Fatalf("Save error = %v, want ErrInvalidMatcher", err)
	}
	if err := Save(ScopeGlobal, "", Settings{Hooks: map[Event][]HookConfig{Stop: {{Match: "[", Command: "x"}}}}); err != nil {
		t.Fatalf("a matcher on an event that ignores it must not be refused: %v", err)
	}
}

func matchesTool(h ResolvedHook, toolName string) bool {
	ok, err := matchTool(h, toolName)
	return ok && err == nil
}

func approvedScriptHook(t *testing.T, event Event) (LoadOptions, string, []ResolvedHook) {
	t.Helper()
	home := testenv.TempDir(t)
	proj := testenv.TempDir(t)
	opts := LoadOptions{ProjectRoot: proj, HomeDir: home}
	script := filepath.Join(proj, "scripts", "guard.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("exit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, proj, `{"hooks":{"`+string(event)+`":[{"command":"sh scripts/guard.sh"}]}}`)
	approveProjectHooks(t, opts)
	hooks := Load(opts)
	if len(hooks) != 1 {
		t.Fatalf("approved hooks = %+v", hooks)
	}
	return opts, script, hooks
}

func TestGatingHookBlocksWhenItsApprovedScriptChanges(t *testing.T) {
	opts, script, hooks := approvedScriptHook(t, PreToolUse)
	spawned := 0
	spawn := func(context.Context, SpawnInput) SpawnResult { spawned++; return SpawnResult{} }
	pay := Payload{Event: PreToolUse, ToolName: "bash", Cwd: opts.ProjectRoot}

	if rep := Run(context.Background(), pay, hooks, spawn); rep.Blocked || spawned != 1 {
		t.Fatalf("an unchanged approved hook must run and pass: blocked=%v spawned=%d", rep.Blocked, spawned)
	}
	if err := os.WriteFile(script, []byte("exit 0 # edited\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	rep := Run(context.Background(), pay, hooks, spawn)
	if !rep.Blocked || spawned != 1 {
		t.Fatalf("a guard whose script changed must block without running: blocked=%v spawned=%d", rep.Blocked, spawned)
	}
	o := rep.Outcomes[0]
	if !errors.Is(o.Cause, ErrApprovalChanged) || !errors.Is(o.Refusal, config.ErrProjectProgramChanged) || UnevaluableCode(o.Cause) != CodeApprovalChanged {
		t.Fatalf("outcome = %+v", o)
	}
	r := NewRunner(hooks, opts.ProjectRoot, spawn, nil)
	block, msg := r.PreToolUse(context.Background(), "bash", json.RawMessage(`{}`))
	if !block || !strings.Contains(msg, "code=approval_changed") || !strings.Contains(msg, "step=approval") || strings.Contains(msg, "guard.sh") {
		t.Fatalf("model reason = %q (block=%v)", msg, block)
	}

	approveProjectHooks(t, opts)
	hooks = Load(opts)
	if rep := Run(context.Background(), pay, hooks, spawn); rep.Blocked || spawned != 2 {
		t.Fatalf("after re-approval the guard must run again: blocked=%v spawned=%d", rep.Blocked, spawned)
	}
}

func TestObservingHookIsOnlyRefusedWhenItsApprovedScriptChanges(t *testing.T) {
	opts, script, hooks := approvedScriptHook(t, PostToolUse)
	if err := os.WriteFile(script, []byte("exit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	rep := Run(context.Background(), Payload{Event: PostToolUse, ToolName: "bash", Cwd: opts.ProjectRoot}, hooks,
		func(context.Context, SpawnInput) SpawnResult {
			t.Fatal("a changed hook must not run")
			return SpawnResult{}
		})
	if rep.Blocked || len(rep.Outcomes) != 1 || rep.Outcomes[0].Decision != DecisionError || !errors.Is(rep.Outcomes[0].Refusal, config.ErrProjectProgramChanged) {
		t.Fatalf("report = %+v", rep)
	}
}

func TestSaveNamesTheHookWithTheInvalidMatcher(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	err := Save(ScopeGlobal, "", Settings{Hooks: map[Event][]HookConfig{PreToolUse: {{Command: "ok"}, {Match: "a(", Command: "x"}}}})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"settings.json", "hooks.PreToolUse[1]", `"a("`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}
