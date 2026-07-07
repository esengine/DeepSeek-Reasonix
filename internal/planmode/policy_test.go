package planmode

import (
	"encoding/json"
	"reflect"
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

func TestDecideAllowsDeclaredReadOnlyBashCommandPrefix(t *testing.T) {
	p := Policy{ReadOnlyCommands: []string{"gh issue view", "internal-report --read"}}
	args, err := json.Marshal(map[string]any{"command": "gh issue view 4572 --json title,body"})
	if err != nil {
		t.Fatal(err)
	}

	if decision := p.Decide(Call{Name: "bash", Args: args}); decision.Blocked {
		t.Fatalf("declared read-only command should be allowed in plan mode: %s", decision.Message)
	}

	args, err = json.Marshal(map[string]any{"command": "internal-report --read service-a"})
	if err != nil {
		t.Fatal(err)
	}
	if decision := p.Decide(Call{Name: "bash", Args: args}); decision.Blocked {
		t.Fatalf("declared read-only command with flags should be allowed in plan mode: %s", decision.Message)
	}
}

func TestDecideDeclaredReadOnlyBashCommandStillBlocksShellSyntax(t *testing.T) {
	p := Policy{ReadOnlyCommands: []string{"gh issue view"}}
	args, err := json.Marshal(map[string]any{"command": "gh issue view 4572 && rm -rf /"})
	if err != nil {
		t.Fatal(err)
	}

	if decision := p.Decide(Call{Name: "bash", Args: args}); !decision.Blocked {
		t.Fatal("declared read-only command must not bypass shell syntax checks")
	}
}

func TestDecideAllowsBashSafeNullRedirects(t *testing.T) {
	bash := func(cmd string) Call {
		args, err := json.Marshal(map[string]any{"command": cmd})
		if err != nil {
			t.Fatal(err)
		}
		return Call{Name: "bash", Args: args}
	}

	for _, cmd := range []string{
		"git log 2>/dev/null",
		"git log 2> /dev/null",
		"git log >/dev/null",
		"git log >>/dev/null",
		"git log &>/dev/null",
		"git log &>> /dev/null",
		"git log 2>$null",
		"git log 2>NUL",
		"git log 2>&1",
		"2>/dev/null git log",
	} {
		t.Run(cmd, func(t *testing.T) {
			if decision := (Policy{}).Decide(bash(cmd)); decision.Blocked {
				t.Fatalf("safe redirect command should be allowed in plan mode: %s", decision.Message)
			}
		})
	}
}

func TestDecideBashSafeRedirectsStayConservative(t *testing.T) {
	bash := func(cmd string) Call {
		args, err := json.Marshal(map[string]any{"command": cmd})
		if err != nil {
			t.Fatal(err)
		}
		return Call{Name: "bash", Args: args}
	}

	for _, cmd := range []string{
		"git log >/tmp/out",
		"git log >/dev/nullish",
		"git log 2>$nullish",
		"git log 2>nul.txt",
		"git log < /dev/null",
		"git log 2>/dev/null && rm -rf /tmp/x",
		"git diff --output changes.patch 2>/dev/null",
	} {
		t.Run(cmd, func(t *testing.T) {
			if decision := (Policy{}).Decide(bash(cmd)); !decision.Blocked {
				t.Fatal("unsafe or non-operator redirect-looking command should be blocked in plan mode")
			}
		})
	}
}

func TestDecideBashAllowsRedirectLookingText(t *testing.T) {
	bash := func(cmd string) Call {
		args, err := json.Marshal(map[string]any{"command": cmd})
		if err != nil {
			t.Fatal(err)
		}
		return Call{Name: "bash", Args: args}
	}

	for _, cmd := range []string{
		`git log "2>/dev/null"`,
		`git log 2\>/dev/null`,
	} {
		t.Run(cmd, func(t *testing.T) {
			if decision := (Policy{}).Decide(bash(cmd)); decision.Blocked {
				t.Fatalf("redirect-looking text should stay a read-only argument in plan mode: %s", decision.Message)
			}
		})
	}
}

func TestDecideDeclaredReadOnlyBashCommandIgnoresShellInterpreters(t *testing.T) {
	p := Policy{ReadOnlyCommands: []string{"bash", "gh", "gh issue", "gh issue close"}}
	args, err := json.Marshal(map[string]any{"command": "bash run-anything"})
	if err != nil {
		t.Fatal(err)
	}

	if decision := p.Decide(Call{Name: "bash", Args: args}); !decision.Blocked {
		t.Fatal("plan_mode_read_only_commands must not reopen shell interpreters")
	}
	want := []string{"bash", "gh", "gh issue", "gh issue close"}
	if got := p.IgnoredReadOnlyCommands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("IgnoredReadOnlyCommands() = %v, want %v", got, want)
	}
}

func TestDecideUnknownBashReadOnlyCommandCanAskForTrust(t *testing.T) {
	args, err := json.Marshal(map[string]any{"command": "gh issue view 4572 --json title"})
	if err != nil {
		t.Fatal(err)
	}
	decision := (Policy{}).Decide(Call{Name: "bash", Args: args})
	if !decision.Blocked || decision.ReadOnlyCommandTrust == nil {
		t.Fatalf("unknown query should be blocked with trust candidate, got %+v", decision)
	}
	if decision.ReadOnlyCommandTrust.Prefix != "gh issue view" {
		t.Fatalf("trust prefix = %q, want gh issue view", decision.ReadOnlyCommandTrust.Prefix)
	}
}

func TestDecideUnsafeUnknownBashCommandDoesNotAskForTrust(t *testing.T) {
	for _, cmd := range []string{
		"gh issue close 4572",
		"gh pr merge 5867",
		"aws s3 rm s3://bucket/key",
		"bash -lc 'gh issue view 1'",
	} {
		t.Run(cmd, func(t *testing.T) {
			args, err := json.Marshal(map[string]any{"command": cmd})
			if err != nil {
				t.Fatal(err)
			}
			decision := (Policy{}).Decide(Call{Name: "bash", Args: args})
			if decision.ReadOnlyCommandTrust != nil {
				t.Fatalf("unsafe command should not ask for trust, got %+v", decision.ReadOnlyCommandTrust)
			}
		})
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

// TestPlanModeAllowsSharedReadOnlyBashSet is the #5341 regression: plan mode now
// classifies bash commands against the same shellsafe table the permission
// auto-approve path uses, so read-only git/tooling commands beyond the old
// status/diff/log/show short list are runnable while planning. Write-capable
// commands and write-capable args remain blocked.
func TestPlanModeAllowsSharedReadOnlyBashSet(t *testing.T) {
	bash := func(cmd string) Call {
		args, err := json.Marshal(map[string]any{"command": cmd})
		if err != nil {
			t.Fatal(err)
		}
		return Call{Name: "bash", Args: args}
	}

	for _, cmd := range []string{
		"git rev-parse --abbrev-ref HEAD", "git describe --tags", "git reflog",
		"git for-each-ref", "git cat-file -p HEAD", "git ls-tree HEAD",
		"go env", "npm view react version", "docker ps", "kubectl get pods",
		"grep 'a|b' file",
	} {
		if d := (Policy{}).Decide(bash(cmd)); d.Blocked {
			t.Errorf("plan mode blocked read-only %q: %s", cmd, d.Message)
		}
	}

	for _, cmd := range []string{
		"git checkout main", "git commit -m x", "git branch -d feature",
		"go build ./...", "npm install",
		"git status && rm -rf /", "git status > out.txt",
		"git grep foo --open-files-in-pager=sh", "go list ./... -mod=mod",
		"echo $HOME",
	} {
		if d := (Policy{}).Decide(bash(cmd)); !d.Blocked {
			t.Errorf("plan mode allowed unsafe %q", cmd)
		}
	}
}
