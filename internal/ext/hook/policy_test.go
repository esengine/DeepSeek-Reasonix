package hook

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestPolicyGuardClassificationAndLiveReplacement(t *testing.T) {
	for _, event := range Events {
		for _, format := range []string{"", "claude"} {
			h := ResolvedHook{Event: event, HookConfig: HookConfig{PayloadFormat: format}}
			want := event == PreToolUse || event == UserPromptSubmit || event == PermissionRequest
			if h.IsPolicyGuard() != want {
				t.Errorf("%s/%s guard classification", event, format)
			}
		}
	}
	var commands []string
	spawn := func(_ context.Context, in SpawnInput) SpawnResult {
		commands = append(commands, in.Command)
		return SpawnResult{Stdout: in.Command}
	}
	runner := NewRunner([]ResolvedHook{
		{Event: SessionStart, HookConfig: HookConfig{Command: "retained"}},
		{Event: PreToolUse, HookConfig: HookConfig{Command: "old-guard"}},
	}, testenv.TempDir(t), spawn, nil)
	role := runner.ForRole("child")
	runner.ReplacePolicyGuards([]ResolvedHook{
		{Event: SessionStart, HookConfig: HookConfig{Command: "unselected"}},
		{Event: PreToolUse, HookConfig: HookConfig{Command: "new-guard"}},
	})
	for _, r := range []*Runner{runner, role} {
		r.SessionStart(t.Context())
		r.PreToolUse(t.Context(), "read_file", json.RawMessage(`{}`))
	}
	if !reflect.DeepEqual(commands, []string{"retained", "new-guard", "retained", "new-guard"}) {
		t.Fatalf("policy update changed retained automation or left a role stale: %v", commands)
	}
}
