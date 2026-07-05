package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/event"
	"strings"
	"testing"

	"reasonix/internal/planmode"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// planSafeTool wraps fakeTool to self-report a plan-mode stance, mimicking a
// real tool that implements tool.PlanModeClassifier. Plain fakeTool deliberately
// does NOT implement the interface, so it models an unclassified/external tool
// (PlanSafetyUnknown) — relied on by the fail-closed and escape-valve cases below.
type planSafeTool struct {
	fakeTool
	planSafe bool
}

func (p planSafeTool) PlanModeSafe() bool { return p.planSafe }

// TestPlanModeBlocksWriters proves the gate refuses tools that aren't classified
// plan-safe while letting a self-reported plan-safe tool run. The returned tool
// result starts with "blocked:" so the model can adapt mid-turn.
func TestPlanModeBlocksWriters(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(planSafeTool{fakeTool: fakeTool{name: "read_only_tool", readOnly: true}, planSafe: true})
	reg.Add(fakeTool{name: "writer_tool", readOnly: false})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.SetPlanMode(true)

	ro := a.executeOne(context.Background(), provider.ToolCall{Name: "read_only_tool"})
	if !strings.Contains(ro.output, "done") {
		t.Errorf("plan-safe read-only tool in plan mode should still run: %q", ro.output)
	}

	wr := a.executeOne(context.Background(), provider.ToolCall{Name: "writer_tool"})
	if !strings.HasPrefix(wr.output, "blocked:") {
		t.Errorf("writer tool in plan mode should return a 'blocked:' result, got: %q", wr.output)
	}
}

// TestPlanModeDoesNotMutateSystemOrTools is the cache-stability test. Toggling
// plan mode between two stream calls must not change the system prompt or the
// tool list seen by the provider — those are the cache-key prefix, and any
// change there forces an expensive cache miss.
func TestPlanModeDoesNotMutateSystemOrTools(t *testing.T) {
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	reg.Add(fakeTool{name: "write_file", readOnly: false})

	a := New(prov, reg, NewSession("STABLE-SYS"), Options{}, event.Discard)

	// Auto-mode round-trip.
	if err := a.Run(context.Background(), "explore"); err != nil {
		t.Fatalf("auto Run: %v", err)
	}
	autoSys := prov.lastReq.Messages[0]
	autoTools := serializeToolNames(prov.lastReq.Tools)

	// Flip the gate and run again. The user message changes (new turn), but
	// the system message and the tool list both have to come back identical.
	prov.chunks = []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}
	a.SetPlanMode(true)
	if err := a.Run(context.Background(), "now in plan mode"); err != nil {
		t.Fatalf("plan Run: %v", err)
	}
	planSys := prov.lastReq.Messages[0]
	planTools := serializeToolNames(prov.lastReq.Tools)

	if planSys.Role != autoSys.Role || planSys.Content != autoSys.Content {
		t.Errorf("system message changed across mode toggle:\n auto: %+v\n plan: %+v", autoSys, planSys)
	}
	if planTools != autoTools {
		t.Errorf("tool list changed across mode toggle:\n auto: %s\n plan: %s", autoTools, planTools)
	}
}

func serializeToolNames(ts []provider.ToolSchema) string {
	var names []string
	for _, t := range ts {
		names = append(names, t.Name)
	}
	return strings.Join(names, ",")
}

func bashCommandArgs(t *testing.T, cmd string) json.RawMessage {
	t.Helper()
	args, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: cmd})
	if err != nil {
		t.Fatalf("marshal bash args: %v", err)
	}
	return args
}

// --- planModeBlocked tests ---

func TestPlanModeDeniedToolsBlocked(t *testing.T) {
	denied := []string{
		"write_file", "edit_file", "multi_edit", "move_file", "apply_patch",
		"edit_notebook", "notebook_edit", "range_delete", "symbol_delete", "delete_range", "delete_symbol",
		"complete_step", "task", "parallel_tasks", "run_skill",
		"explore", "research", "review", "security_review", "security-review",
		"install_source", "install_skill", "remember", "forget", "kill_shell",
	}
	for _, name := range denied {
		t.Run(name, func(t *testing.T) {
			blocked, msg := (&Agent{}).planModeBlocked(name, false, false, planmode.PlanSafetyUnknown, nil)
			if !blocked {
				t.Errorf("planModeBlocked(%q) = false, want true", name)
			}
			if name == "complete_step" && !strings.Contains(msg, "only available after plan approval") {
				t.Errorf("unexpected complete_step message: %s", msg)
			}
			if name != "complete_step" && !strings.Contains(msg, "not available in plan mode") {
				t.Errorf("unexpected message: %s", msg)
			}
		})
	}
}

func TestPlanModeReadOnlyToolsAllowed(t *testing.T) {
	// read_file is on the audited read-only whitelist — Unknown safety suffices.
	if blocked, _ := (&Agent{}).planModeBlocked("read_file", true, false, planmode.PlanSafetyUnknown, nil); blocked {
		t.Error("audited read-only tool read_file should not be blocked in plan mode")
	}
	// read_only_skill is not a built-in; it runs only via its plan-safe self-report.
	if blocked, _ := (&Agent{}).planModeBlocked("read_only_skill", true, false, planmode.PlanSafetySafe, nil); blocked {
		t.Error("self-reported plan-safe read_only_skill should not be blocked in plan mode")
	}
}

func TestPlanModeAllowedToolsOverride(t *testing.T) {
	a := &Agent{planModeAllowedTools: []string{"custom_tool"}}
	blocked, _ := a.planModeBlocked("custom_tool", false, false, planmode.PlanSafetyUnknown, nil)
	if blocked {
		t.Error("tool in planModeAllowedTools should not be blocked")
	}
}

func TestPlanModeReadOnlyCommandsOverride(t *testing.T) {
	a := &Agent{planModeReadOnlyCommands: []string{"gh issue view"}}
	blocked, msg := a.planModeBlocked("bash", false, false, planmode.PlanSafetyUnknown, bashCommandArgs(t, "gh issue view 4572 --json title"))
	if blocked {
		t.Fatalf("command in planModeReadOnlyCommands should not be blocked: %s", msg)
	}
}

func TestPlanModeGenericWriterBlocked(t *testing.T) {
	blocked, msg := (&Agent{}).planModeBlocked("some_writer_tool", false, false, planmode.PlanSafetyUnknown, nil)
	if !blocked {
		t.Error("generic writer tool should be blocked in plan mode")
	}
	if !strings.Contains(msg, "writer tool") {
		t.Errorf("unexpected message: %s", msg)
	}
}

// TestPlanModeExternalToolEscapeValve proves an unclassified external tool (an
// MCP/plugin tool that does not implement PlanModeClassifier, so its safety is
// Unknown) is fail-closed in plan mode, but can be re-enabled end to end via
// plan_mode_allowed_tools.
func TestPlanModeExternalToolEscapeValve(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "mcp__server__query", readOnly: false})

	failClosed := New(nil, reg, NewSession(""), Options{}, event.Discard)
	failClosed.SetPlanMode(true)
	if out := failClosed.executeOne(context.Background(), provider.ToolCall{Name: "mcp__server__query"}); !strings.HasPrefix(out.output, "blocked:") {
		t.Errorf("unclassified external tool should fail closed in plan mode, got: %q", out.output)
	}

	a := New(nil, reg, NewSession(""), Options{PlanModeAllowedTools: []string{"mcp__server__query"}}, event.Discard)
	a.SetPlanMode(true)
	if out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__server__query"}); strings.HasPrefix(out.output, "blocked:") {
		t.Errorf("plan_mode_allowed_tools should let the declared external tool run, got: %q", out.output)
	}
}

// untrustedReadOnlyTool models an MCP tool that reports ReadOnly()==true from an
// untrusted server readOnlyHint (PlanModeUntrustedReadOnly()==true).
type untrustedReadOnlyTool struct {
	fakeTool
}

func (untrustedReadOnlyTool) PlanModeUntrustedReadOnly() bool { return true }

type untrustedMCPReadOnlyTool struct {
	untrustedReadOnlyTool
	server string
	raw    string
}

func (t untrustedMCPReadOnlyTool) MCPServerName() string  { return t.server }
func (t untrustedMCPReadOnlyTool) MCPRawToolName() string { return t.raw }

type fakePlanModeReadOnlyTrustGate struct {
	allow  bool
	reason string
	req    PlanModeReadOnlyTrustRequest
	calls  int
}

func (g *fakePlanModeReadOnlyTrustGate) CheckPlanModeReadOnlyTrust(ctx context.Context, req PlanModeReadOnlyTrustRequest) (bool, string, error) {
	g.calls++
	g.req = req
	return g.allow, g.reason, nil
}

type readOnlyRecordingGate struct {
	readOnly []bool
}

func (g *readOnlyRecordingGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	g.readOnly = append(g.readOnly, readOnly)
	if !readOnly {
		return false, "expected read-only", nil
	}
	return true, "", nil
}

// TestPlanModeUntrustedReadOnlyToolFailsClosed proves the gate does NOT trust an
// MCP tool's self-reported readOnlyHint: a ReadOnly()==true external tool is
// still fail-closed in plan mode, and runs only once declared in
// plan_mode_allowed_tools.
func TestPlanModeUntrustedReadOnlyToolFailsClosed(t *testing.T) {
	mk := func() *tool.Registry {
		reg := tool.NewRegistry()
		reg.Add(untrustedReadOnlyTool{fakeTool{name: "mcp__srv__query", readOnly: true}})
		return reg
	}

	failClosed := New(nil, mk(), NewSession(""), Options{}, event.Discard)
	failClosed.SetPlanMode(true)
	if out := failClosed.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__query"}); !strings.HasPrefix(out.output, "blocked:") {
		t.Errorf("untrusted read-only MCP tool should fail closed in plan mode, got: %q", out.output)
	}

	declared := New(nil, mk(), NewSession(""), Options{PlanModeAllowedTools: []string{"mcp__srv__query"}}, event.Discard)
	declared.SetPlanMode(true)
	if out := declared.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__query"}); strings.HasPrefix(out.output, "blocked:") {
		t.Errorf("declared untrusted tool should run in plan mode, got: %q", out.output)
	}
}

func TestPlanModeUntrustedReadOnlyToolCanAskForTrust(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(untrustedMCPReadOnlyTool{
		untrustedReadOnlyTool: untrustedReadOnlyTool{fakeTool{name: "mcp__srv__normalized_query", readOnly: true}},
		server:                "srv",
		raw:                   "raw/query",
	})
	gate := &fakePlanModeReadOnlyTrustGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{PlanModeReadOnlyTrustGate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__normalized_query", Arguments: `{"q":"x"}`})
	if strings.HasPrefix(out.output, "blocked:") || !strings.Contains(out.output, "done") {
		t.Fatalf("trusted untrusted-read-only MCP tool should run, got: %q", out.output)
	}
	if gate.calls != 1 {
		t.Fatalf("trust gate calls = %d, want 1", gate.calls)
	}
	if gate.req.ServerName != "srv" || gate.req.RawToolName != "raw/query" {
		t.Fatalf("trust request target = %+v, want raw MCP identity", gate.req)
	}
	if gate.req.ToolName != "mcp__srv__normalized_query" || string(gate.req.Args) != `{"q":"x"}` {
		t.Fatalf("trust request call metadata = %+v", gate.req)
	}
}

func TestPlanModeUntrustedReadOnlyToolTrustDeclineBlocks(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(untrustedReadOnlyTool{fakeTool{name: "mcp__srv__query", readOnly: true}})
	gate := &fakePlanModeReadOnlyTrustGate{allow: false, reason: "not trusted"}
	a := New(nil, reg, NewSession(""), Options{PlanModeReadOnlyTrustGate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__query"})
	if !strings.Contains(out.output, "not trusted") {
		t.Fatalf("declined trust should block with reason, got: %q", out.output)
	}
	if gate.req.ServerName != "srv" || gate.req.RawToolName != "query" {
		t.Fatalf("fallback trust request target = %+v", gate.req)
	}
}

type unsafeUntrustedReadOnlyTool struct {
	untrustedReadOnlyTool
}

func (unsafeUntrustedReadOnlyTool) PlanModeSafe() bool { return false }

func TestPlanModeUnsafeUntrustedReadOnlyToolDoesNotAskForTrust(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(unsafeUntrustedReadOnlyTool{untrustedReadOnlyTool{fakeTool{name: "mcp__srv__unsafe", readOnly: true}}})
	gate := &fakePlanModeReadOnlyTrustGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{PlanModeReadOnlyTrustGate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__unsafe"})
	if !strings.HasPrefix(out.output, "blocked:") {
		t.Fatalf("unsafe untrusted read-only MCP tool should remain blocked, got: %q", out.output)
	}
	if gate.calls != 0 {
		t.Fatalf("trust gate calls = %d, want 0 for unsafe tool", gate.calls)
	}
}

func TestPlanModeUnknownBashReadOnlyCommandCanAskForTrust(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	gate := &fakePlanModeReadOnlyTrustGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{PlanModeReadOnlyTrustGate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: string(bashCommandArgs(t, "gh issue view 4572 --json title"))})
	if strings.HasPrefix(out.output, "blocked:") || !strings.Contains(out.output, "done") {
		t.Fatalf("trusted unknown bash query should run, got: %q", out.output)
	}
	if gate.calls != 1 {
		t.Fatalf("trust gate calls = %d, want 1", gate.calls)
	}
	if gate.req.ToolName != PlanModeReadOnlyCommandApprovalTool || gate.req.Command != "gh issue view 4572 --json title" || gate.req.Prefix != "gh issue view" {
		t.Fatalf("trust request = %+v, want bash command prefix request", gate.req)
	}
}

func TestPlanModeTrustedUnknownBashCommandReachesGateAsReadOnly(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	trustGate := &fakePlanModeReadOnlyTrustGate{allow: true}
	permissionGate := &readOnlyRecordingGate{}
	a := New(nil, reg, NewSession(""), Options{
		Gate:                      permissionGate,
		PlanModeReadOnlyTrustGate: trustGate,
	}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: string(bashCommandArgs(t, "gh issue view 4572"))})
	if strings.HasPrefix(out.output, "blocked:") || !strings.Contains(out.output, "done") {
		t.Fatalf("trusted unknown bash query should run through the normal gate as read-only, got: %q", out.output)
	}
	if len(permissionGate.readOnly) != 1 || !permissionGate.readOnly[0] {
		t.Fatalf("permission gate readOnly calls = %v, want [true]", permissionGate.readOnly)
	}
}

func TestPlanModeUnknownBashReadOnlyCommandDeclineBlocks(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	gate := &fakePlanModeReadOnlyTrustGate{allow: false, reason: "not read-only"}
	a := New(nil, reg, NewSession(""), Options{PlanModeReadOnlyTrustGate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: string(bashCommandArgs(t, "gh issue view 4572"))})
	if !strings.Contains(out.output, "not read-only") {
		t.Fatalf("declined bash trust should block with reason, got: %q", out.output)
	}
}

func TestPlanModeUnsafeUnknownBashCommandDoesNotAskForTrust(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	gate := &fakePlanModeReadOnlyTrustGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{PlanModeReadOnlyTrustGate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: string(bashCommandArgs(t, "gh issue view 4572 && rm -rf /"))})
	if !strings.HasPrefix(out.output, "blocked:") {
		t.Fatalf("unsafe shell syntax should remain blocked, got: %q", out.output)
	}
	if gate.calls != 0 {
		t.Fatalf("trust gate calls = %d, want 0 for unsafe shell syntax", gate.calls)
	}
}

// --- planModeBashBlocked tests ---

func TestPlanModeBashBlocked_SafeCommands(t *testing.T) {
	safe := []string{
		"git status",
		"git diff",
		"git diff --staged",
		"git log --oneline -10",
		"git show HEAD",
		"git ls-files",
		"git grep 'func main'",
		"git grep -o 'func'",
		"git blame file.go",
		"ls -la",
		"cat file.go",
		"grep -rn 'pattern' .",
		"find . -name '*.go'",
		"head -20 file.go",
		"tail -5 file.go",
		"pwd",
		"echo hello",
		"wc -l file.go",
		"which go",
		"type git",
		"uname -a",
		"hostname",
		"go version",
		"go list ./...",
		"go doc fmt.Println",
		"go vet ./...",
		"node -v",
		"npm list",
		"python --version",
	}
	for _, cmd := range safe {
		t.Run(cmd, func(t *testing.T) {
			blocked, msg := planModeBashBlocked(bashCommandArgs(t, cmd))
			if blocked {
				t.Errorf("planModeBashBlocked(%q) = blocked, want allowed; msg: %s", cmd, msg)
			}
		})
	}
}

func TestPlanModeBashBlocked_Metacharacters(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"semicolon", "git status; rm file.go"},
		{"and", "git status && rm file.go"},
		{"or", "git status || rm file.go"},
		{"pipe", "ls | grep foo"},
		{"redirect_out", "echo hello > file.go"},
		{"redirect_append", "echo hello >> file.go"},
		{"redirect_in", "cat < file.go"},
		{"heredoc", "cat << EOF"},
		{"cmd_sub_dollar", "echo $(rm file.go)"},
		{"background_ampersand", "git status & rm file.go"},
		{"newline", "git status\nrm file.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, msg := planModeBashBlocked(bashCommandArgs(t, tt.cmd))
			if !blocked {
				t.Errorf("planModeBashBlocked(%q) = allowed, want blocked", tt.cmd)
			}
			if !strings.Contains(msg, "shell operators") {
				t.Errorf("unexpected message: %s", msg)
			}
		})
	}
}

func TestPlanModeBashBlocked_ProcessControlArgs(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{
			name: "background execution",
			args: `{"command":"git status","run_in_background":true}`,
			want: "background execution",
		},
		{
			name: "process preservation",
			args: `{"command":"git status","preserve_background_processes":true}`,
			want: "process preservation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, msg := planModeBashBlocked(json.RawMessage(tt.args))
			if !blocked {
				t.Fatalf("planModeBashBlocked(%s) = allowed, want blocked", tt.args)
			}
			if !strings.Contains(msg, tt.want) {
				t.Fatalf("blocked message = %q, want %q", msg, tt.want)
			}
		})
	}
}

func TestPlanModeBashBlocked_WriteCapableSafeCommandArgs(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		arg  string
	}{
		{"find_delete", "find . -delete", "-delete"},
		{"find_exec", "find . -name '*.tmp' -exec rm {} +", "-exec"},
		{"find_fprint", "find . -name '*.go' -fprint files.txt", "-fprint"},
		{"git_diff_output_equals", "git diff --output=patch.diff", "--output=patch.diff"},
		{"git_diff_output_space", "git diff --output patch.diff", "--output"},
		{"git_show_output", "git show HEAD --output=show.txt", "--output=show.txt"},
		{"git_log_ext_diff", "git log --ext-diff", "--ext-diff"},
		{"git_grep_open_pager_short", "git grep -O 'func main'", "-O"},
		{"git_grep_open_pager_long", "git grep --open-files-in-pager=sh 'func main'", "--open-files-in-pager=sh"},
		{"go_list_mod_write", "go list -mod=mod ./...", "-mod=mod"},
		{"go_list_mod_space", "go list -mod mod ./...", "-mod"},
		{"go_list_modfile", "go list -modfile=other.mod ./...", "-modfile=other.mod"},
		{"go_list_toolexec", "go list -toolexec=./wrapper ./...", "-toolexec=./wrapper"},
		{"go_vet_fix", "go vet -fix ./...", "-fix"},
		{"go_vet_toolexec", "go vet -toolexec ./wrapper ./...", "-toolexec"},
		{"go_vet_vettool", "go vet -vettool=./writer ./...", "-vettool=./writer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, msg := planModeBashBlocked(bashCommandArgs(t, tt.cmd))
			if !blocked {
				t.Errorf("planModeBashBlocked(%q) = allowed, want blocked", tt.cmd)
			}
			if !strings.Contains(msg, tt.arg) {
				t.Errorf("blocked message %q does not mention unsafe arg %q", msg, tt.arg)
			}
		})
	}
}

func TestPlanModeBashBlocked_UnsafeCommands(t *testing.T) {
	unsafe := []string{
		"rm -rf /tmp/foo",
		"cp file1 file2",
		"mv old new",
		"mkdir newdir",
		"touch newfile",
		"chmod 755 script.sh",
		"git add .",
		"git commit -m 'msg'",
		"git push origin main",
		"git checkout -b new-branch",
		"go build ./...",
		"go test ./...",
		"npm install",
		"npm run build",
		"pip install requests",
		"docker build .",
		"make all",
		"sed -i 's/old/new/' file.go",
		"awk '{print $1}' file",
		"tee output.txt",
		"dd if=/dev/zero of=file",
		"ssh user@host",
		"curl https://example.com",
		"wget https://example.com",
		"python -c 'print(1)'",
		"node -e 'console.log(1)'",
		"ruby -e 'puts 1'",
		"perl -e 'print 1'",
	}
	for _, cmd := range unsafe {
		t.Run(cmd, func(t *testing.T) {
			blocked, _ := planModeBashBlocked(bashCommandArgs(t, cmd))
			if !blocked {
				t.Errorf("planModeBashBlocked(%q) = allowed, want blocked", cmd)
			}
		})
	}
}

func TestPlanModeBashBlocked_BoundaryCheck(t *testing.T) {
	// "echop" should NOT match "echo" prefix
	args := json.RawMessage(`{"command":"echop hello"}`)
	blocked, _ := planModeBashBlocked(args)
	if !blocked {
		t.Error("echop should not match echo prefix — boundary check failed")
	}

	// "lsblk" should NOT match "ls" prefix
	args = json.RawMessage(`{"command":"lsblk"}`)
	blocked, _ = planModeBashBlocked(args)
	if !blocked {
		t.Error("lsblk should not match ls prefix — boundary check failed")
	}

	// "git status" with a flag after a whitespace boundary should match.
	args = json.RawMessage(`{"command":"git status --short"}`)
	blocked, _ = planModeBashBlocked(args)
	if blocked {
		t.Error("git status --short should match git status prefix")
	}

	// A dash inside the command name should NOT match a shorter safe prefix.
	args = json.RawMessage(`{"command":"git diff-files"}`)
	blocked, _ = planModeBashBlocked(args)
	if !blocked {
		t.Error("git diff-files should not match git diff prefix")
	}

	args = json.RawMessage(`{"command":"cat-file --batch"}`)
	blocked, _ = planModeBashBlocked(args)
	if !blocked {
		t.Error("cat-file should not match cat prefix")
	}
}

func TestPlanModeBashBlocked_EmptyCommand(t *testing.T) {
	// Empty/missing command should not crash
	blocked, _ := planModeBashBlocked(json.RawMessage(`{}`))
	if !blocked {
		t.Error("missing command should fail closed in plan mode")
	}
	blocked, _ = planModeBashBlocked(json.RawMessage(`{"command":""}`))
	if !blocked {
		t.Error("empty string command should fail closed in plan mode")
	}
}

func TestPlanModeBashBlocked_InvalidJSON(t *testing.T) {
	blocked, _ := planModeBashBlocked(json.RawMessage(`not json`))
	if !blocked {
		t.Error("invalid JSON should fail closed in plan mode")
	}
}

func TestPlanModeBash_BashToolIntegration(t *testing.T) {
	// Integration test: planModeBlocked with toolName="bash" delegates to planModeBashBlocked
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.SetPlanMode(true)

	// Safe command should execute
	safeResult := a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"git status"}`,
	})
	if strings.HasPrefix(safeResult.output, "blocked:") {
		t.Errorf("git status should not be blocked in plan mode: %s", safeResult.output)
	}

	// Unsafe command should be blocked
	unsafeResult := a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"rm -rf /"}`,
	})
	if !strings.HasPrefix(unsafeResult.output, "blocked:") {
		t.Errorf("rm -rf should be blocked in plan mode: %s", unsafeResult.output)
	}

	// Chained command should be blocked
	chainResult := a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"git status && rm file.go"}`,
	})
	if !strings.HasPrefix(chainResult.output, "blocked:") {
		t.Errorf("chained command should be blocked in plan mode: %s", chainResult.output)
	}

	backgroundResult := a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"git status","run_in_background":true}`,
	})
	if !strings.HasPrefix(backgroundResult.output, "blocked:") {
		t.Errorf("background bash should be blocked in plan mode: %s", backgroundResult.output)
	}

	preserveResult := a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"git status","preserve_background_processes":true}`,
	})
	if !strings.HasPrefix(preserveResult.output, "blocked:") {
		t.Errorf("process-preserving bash should be blocked in plan mode: %s", preserveResult.output)
	}
}

func TestPlanMode_BashAllowedToolsOverride(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})

	a := New(nil, reg, NewSession(""), Options{
		PlanModeAllowedTools: []string{"bash"},
	}, event.Discard)
	a.SetPlanMode(true)

	result := a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"rm -rf /"}`,
	})
	if !strings.HasPrefix(result.output, "blocked:") {
		t.Errorf("bash in planModeAllowedTools must still validate unsafe commands: %s", result.output)
	}
}

func TestPlanModeOff_AllToolsAllowed(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	reg.Add(fakeTool{name: "bash", readOnly: false})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	// planMode is OFF by default

	// write_file should execute normally
	result := a.executeOne(context.Background(), provider.ToolCall{Name: "write_file"})
	if strings.HasPrefix(result.output, "blocked:") {
		t.Error("write_file should not be blocked when plan mode is off")
	}

	// bash with any command should execute normally
	result = a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"rm -rf /"}`,
	})
	if strings.HasPrefix(result.output, "blocked:") {
		t.Error("bash should not be blocked when plan mode is off")
	}
}
