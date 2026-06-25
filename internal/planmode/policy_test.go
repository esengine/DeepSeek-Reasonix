package planmode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecideAllowsReadOnlyResearchAndBlocksKnownWriters(t *testing.T) {
	p := Policy{}

	allowed := p.Decide(Call{Name: "read_file", ReadOnly: true})
	if allowed.Blocked {
		t.Fatalf("read-only research tool blocked: %s", allowed.Message)
	}

	blocked := p.Decide(Call{Name: "write_file", ReadOnly: false})
	if !blocked.Blocked {
		t.Fatal("write_file should be blocked in plan mode")
	}
	if !strings.Contains(blocked.Message, "not available in plan mode") {
		t.Fatalf("blocked message = %q, want plan-mode availability explanation", blocked.Message)
	}
}

func TestDecideDoesNotLetOverridesReopenKnownBlockedTools(t *testing.T) {
	p := Policy{AllowedTools: []string{"write_file"}}

	decision := p.Decide(Call{Name: "write_file", ReadOnly: false})
	if !decision.Blocked {
		t.Fatal("plan_mode_allowed_tools must not allow known blocked writer tools")
	}
	if got := p.IgnoredAllowedTools(); len(got) != 1 || got[0] != "write_file" {
		t.Fatalf("IgnoredAllowedTools() = %v, want [write_file]", got)
	}
}

func TestDecideBlocksSubagentStyleToolsWithCategoryMessage(t *testing.T) {
	for _, name := range []string{"explore", "research", "review", "security_review", "security-review"} {
		t.Run(name, func(t *testing.T) {
			decision := (Policy{}).Decide(Call{Name: name, ReadOnly: false})
			if !decision.Blocked {
				t.Fatalf("%s should be blocked in plan mode", name)
			}
			if !strings.Contains(decision.Message, "not available in plan mode") {
				t.Fatalf("blocked message = %q, want category message", decision.Message)
			}
		})
	}
}

func TestDecideAllowsReadOnlyTaskDelegation(t *testing.T) {
	// read_only_task is not a built-in, so under fail-closed it is allowed only
	// because it self-reports plan-safe via PlanModeClassifier (Safety: Safe).
	decision := (Policy{}).Decide(Call{Name: "read_only_task", ReadOnly: true, Safety: PlanSafetySafe})
	if decision.Blocked {
		t.Fatalf("read_only_task should be allowed in plan mode: %s", decision.Message)
	}
}

func TestDecideAllowsReadOnlySkillDelegation(t *testing.T) {
	decision := (Policy{}).Decide(Call{Name: "read_only_skill", ReadOnly: true, Safety: PlanSafetySafe})
	if decision.Blocked {
		t.Fatalf("read_only_skill should be allowed in plan mode: %s", decision.Message)
	}
}

func TestDecideTrustsReadOnlyButFailsClosedOnNonReadOnly(t *testing.T) {
	// Moderate fail-closed boundary: a ReadOnly()==true tool is trusted (only
	// in-process tools can assert that flag; plugin/MCP are contractually
	// ReadOnly()==false). connect_tool_source / history / recall rely on this.
	if d := (Policy{}).Decide(Call{Name: "connect_tool_source", ReadOnly: true}); d.Blocked {
		t.Fatalf("a ReadOnly internal tool should be trusted in plan mode: %s", d.Message)
	}
	// A non-read-only, unclassified tool (the plugin/MCP shape) fails closed.
	if d := (Policy{}).Decide(Call{Name: "mcp__x__mutate", ReadOnly: false}); !d.Blocked {
		t.Fatal("a non-read-only unclassified tool must fail closed in plan mode")
	}
}

func TestDecidePlanSafeReportMustBeReadOnly(t *testing.T) {
	// PlanSafe ⇒ ReadOnly: a tool that claims plan-safe but is not read-only is a
	// wiring bug and must still be refused.
	decision := (Policy{}).Decide(Call{Name: "lying_writer", ReadOnly: false, Safety: PlanSafetySafe})
	if !decision.Blocked {
		t.Fatal("a non-read-only tool claiming plan-safe must be blocked (invariant)")
	}
	if !strings.Contains(decision.Message, "invariant") {
		t.Fatalf("blocked message = %q, want invariant explanation", decision.Message)
	}
}

func TestUnclassifiedExternalToolFailsClosedThenOverride(t *testing.T) {
	// Plan mode is fail-closed for tools it cannot classify (MCP/plugin tools,
	// whose ReadOnly() defaults to false); plan_mode_allowed_tools is the escape
	// valve, and it still never reopens a known blocked writer.
	const ext = "mcp__server__query"

	if d := (Policy{}).Decide(Call{Name: ext}); !d.Blocked {
		t.Fatal("unclassified external tool must fail closed in plan mode")
	}
	if d := (Policy{AllowedTools: []string{ext}}).Decide(Call{Name: ext}); d.Blocked {
		t.Fatalf("plan_mode_allowed_tools must re-enable a declared external tool: %s", d.Message)
	}
	if d := (Policy{AllowedTools: []string{"write_file"}}).Decide(Call{Name: "write_file"}); !d.Blocked {
		t.Fatal("override must not reopen a known blocked writer")
	}
}

func TestDecideStillValidatesBashArgumentsWhenOverridden(t *testing.T) {
	p := Policy{AllowedTools: []string{"bash"}}
	args, err := json.Marshal(map[string]any{"command": "rm -rf /"})
	if err != nil {
		t.Fatal(err)
	}

	decision := p.Decide(Call{Name: "bash", ReadOnly: false, Args: args})
	if !decision.Blocked {
		t.Fatal("bash override must not bypass plan-mode bash safety checks")
	}
}

func TestDecideBlocksQuotedBashWriteArguments(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "single quoted find delete",
			command: "find . -name scratch '-delete'",
			want:    "-delete",
		},
		{
			name:    "backslash escaped find delete",
			command: `find . -name scratch \-delete`,
			want:    "-delete",
		},
		{
			name:    "single quoted git pager",
			command: `git grep foo '--open-files-in-pager=sh -c echo'`,
			want:    "--open-files-in-pager=sh -c echo",
		},
		{
			name:    "single quoted go mod write mode",
			command: "go list ./... '-mod=mod'",
			want:    "-mod=mod",
		},
		{
			name:    "unterminated quote fails closed",
			command: "find . '-delete",
			want:    "malformed shell quoting",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]any{"command": tt.command})
			if err != nil {
				t.Fatal(err)
			}

			decision := (Policy{}).Decide(Call{Name: "bash", ReadOnly: false, Args: args})
			if !decision.Blocked {
				t.Fatalf("bash command %q should be blocked in plan mode", tt.command)
			}
			if !strings.Contains(decision.Message, tt.want) {
				t.Fatalf("blocked message = %q, want to mention %q", decision.Message, tt.want)
			}
		})
	}
}

func TestDecideBlocksBashProcessControlArguments(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "background execution",
			args: map[string]any{"command": "git status", "run_in_background": true},
			want: "background execution",
		},
		{
			name: "process preservation",
			args: map[string]any{"command": "git status", "preserve_background_processes": true},
			want: "process preservation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatal(err)
			}

			decision := (Policy{}).Decide(Call{Name: "bash", ReadOnly: false, Args: args})
			if !decision.Blocked {
				t.Fatalf("bash args %v should be blocked in plan mode", tt.args)
			}
			if !strings.Contains(decision.Message, tt.want) {
				t.Fatalf("blocked message = %q, want to mention %q", decision.Message, tt.want)
			}
		})
	}
}

func TestDecideUntrustedReadOnlyFailsClosedThenOverride(t *testing.T) {
	// An MCP tool can report ReadOnly()==true via the server's readOnlyHint, but
	// that signal is untrusted. The read-only fast path must NOT let it run; it
	// fails closed until declared in plan_mode_allowed_tools.
	call := Call{Name: "mcp__srv__query", ReadOnly: true, Untrusted: true}
	d := (Policy{}).Decide(call)
	if !d.Blocked {
		t.Fatal("untrusted read-only MCP tool must fail closed in plan mode")
	}
	if !strings.Contains(d.Message, "readOnlyHint") {
		t.Fatalf("blocked message should explain the untrusted hint: %s", d.Message)
	}
	if d := (Policy{AllowedTools: []string{"mcp__srv__query"}}).Decide(call); d.Blocked {
		t.Fatalf("plan_mode_allowed_tools must re-enable the declared MCP tool: %s", d.Message)
	}
	// A trusted read-only tool (first-party override, Untrusted=false) is allowed.
	if d := (Policy{}).Decide(Call{Name: "mcp__srv__read", ReadOnly: true, Untrusted: false}); d.Blocked {
		t.Fatalf("a trusted read-only tool should be allowed: %s", d.Message)
	}
}
