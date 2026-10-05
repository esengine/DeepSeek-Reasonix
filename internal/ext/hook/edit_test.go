package hook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

// settings.json is shared with unrelated configuration. A hooks editor that
// rewrites the file must not be the reason someone's other settings vanish.
func TestSaveKeepsUnrelatedSettings(t *testing.T) {
	root := testenv.TempDir(t)
	path := ProjectSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark","hooks":{"Stop":[{"command":"old"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Save(ScopeProject, root, Settings{Hooks: map[Event][]HookConfig{
		PreToolUse: {{Command: "echo hi", Match: "edit_file"}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["theme"]) != `"dark"` {
		t.Errorf("an unrelated setting was dropped: theme = %s", doc["theme"])
	}
	home := testenv.TempDir(t)
	approveProjectHooks(t, LoadOptions{ProjectRoot: root, HomeDir: home})
	loaded := Load(LoadOptions{ProjectRoot: root, HomeDir: home})
	var events []Event
	for _, h := range loaded {
		if h.Scope == ScopeProject {
			events = append(events, h.Event)
		}
	}
	if len(events) != 1 || events[0] != PreToolUse {
		t.Fatalf("project hooks after save = %v, want exactly [PreToolUse]", events)
	}
}

// A project scope with nowhere to write must refuse. Falling back to the global
// file would quietly widen one project's rule to every project.
func TestSaveRefusesProjectScopeWithoutRoot(t *testing.T) {
	if err := Save(ScopeProject, "", Settings{}); err == nil {
		t.Fatal("saving project hooks with no workspace succeeded")
	}
}

// The whole point of a dry run is that exit codes mean different things on
// different events: the same failure blocks a PreToolUse and only warns after.
func TestDryRunTranslatesExitCodeByEvent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell exit")
	}
	root := testenv.TempDir(t)
	blocking, err := DryRun(context.Background(), HookConfig{Command: "exit 2"}, PreToolUse, root, NewDefaultSpawner(RuntimeOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	if !blocking.Blocks {
		t.Errorf("exit 2 on PreToolUse did not report as blocking: %+v", blocking)
	}
	if blocking.ExitCode != 2 {
		t.Errorf("exit code = %d, want 2", blocking.ExitCode)
	}

	after, err := DryRun(context.Background(), HookConfig{Command: "exit 2"}, PostToolUse, root, NewDefaultSpawner(RuntimeOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	if after.Blocks {
		t.Error("PostToolUse cannot block, but the dry run said it would")
	}

	ok, err := DryRun(context.Background(), HookConfig{Command: "echo fine"}, PreToolUse, root, NewDefaultSpawner(RuntimeOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	if ok.Blocks || ok.Decision != DecisionPass {
		t.Errorf("a passing hook reported %+v", ok)
	}
}

// A rule that cannot run is rejected before it is executed, so the message names
// the mistake instead of surfacing whatever the shell said about it.
func TestDryRunRejectsUnrunnableRules(t *testing.T) {
	root := testenv.TempDir(t)
	for _, tc := range []struct {
		name  string
		cfg   HookConfig
		event Event
	}{
		{"no command", HookConfig{Command: "  "}, PreToolUse},
		{"unknown event", HookConfig{Command: "echo hi"}, Event("PostToolsUse")},
		{"bad matcher", HookConfig{Command: "echo hi", Match: "([a-"}, PreToolUse},
	} {
		if _, err := DryRun(context.Background(), tc.cfg, tc.event, root, NewDefaultSpawner(RuntimeOptions{})); err == nil {
			t.Errorf("%s: dry run accepted an unrunnable rule", tc.name)
		}
	}
}
