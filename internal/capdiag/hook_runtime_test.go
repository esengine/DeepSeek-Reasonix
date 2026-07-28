package capdiag

import (
	"errors"
	"testing"

	"reasonix/internal/hook"
)

func TestHookRuntimeIssueIsActionableAndScoped(t *testing.T) {
	entry := hook.Entry{
		Event:  hook.SessionStart,
		Scope:  hook.ScopePlugin,
		Source: `C:\Users\Test\AppData\Roaming\reasonix\plugins\superpowers\.codex-plugin\plugin.json`,
	}
	issue, ok := hookRuntimeIssue(entry, errors.New("hook requires a POSIX shell on Windows, but no usable Git Bash was found"), func(string) string {
		return "<reasonix-home>/plugins/superpowers/.codex-plugin/plugin.json"
	})
	if !ok {
		t.Fatal("missing runtime dependency should produce a diagnostic issue")
	}
	if issue.Code != "hook.shell_unavailable" || issue.Severity != "error" || issue.Subsystem != "hooks" {
		t.Fatalf("issue identity = %+v", issue)
	}
	if issue.Name != string(hook.SessionStart) || issue.SettingsTab != "hooks" {
		t.Fatalf("issue scope = %+v", issue)
	}
	if issue.Source != "<reasonix-home>/plugins/superpowers/.codex-plugin/plugin.json" {
		t.Fatalf("issue source = %q", issue.Source)
	}
	if issue.Remediation == "" {
		t.Fatal("runtime dependency issue must include remediation")
	}
}

func TestHookRuntimeIssueIgnoresAvailableRuntime(t *testing.T) {
	if _, ok := hookRuntimeIssue(hook.Entry{}, nil, func(path string) string { return path }); ok {
		t.Fatal("available runtime must not produce an issue")
	}
}
