package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/event"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestPlanModeBlocksWriters proves the read-only gate refuses non-ReadOnly
// tools while leaving the read-only ones to run normally. The returned tool
// result starts with "blocked:" so the model can adapt mid-turn.
func TestPlanModeBlocksWriters(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_only_tool", readOnly: true})
	reg.Add(fakeTool{name: "writer_tool", readOnly: false})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.SetPlanMode(true)

	ro := a.executeOne(context.Background(), provider.ToolCall{Name: "read_only_tool"})
	if !strings.Contains(ro.output, "done") {
		t.Errorf("read-only tool in plan mode should still run: %q", ro.output)
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
	denied := []string{"write_file", "edit_file", "multi_edit", "apply_patch"}
	for _, name := range denied {
		t.Run(name, func(t *testing.T) {
			blocked, msg := (&Agent{}).planModeBlocked(name, false, nil)
			if !blocked {
				t.Errorf("planModeBlocked(%q) = false, want true", name)
			}
			if !strings.Contains(msg, "not available in plan mode") {
				t.Errorf("unexpected message: %s", msg)
			}
		})
	}
}

func TestPlanModeReadOnlyToolsAllowed(t *testing.T) {
	blocked, _ := (&Agent{}).planModeBlocked("read_file", true, nil)
	if blocked {
		t.Error("ReadOnly tools should not be blocked in plan mode")
	}
}

func TestPlanModeAllowedToolsOverride(t *testing.T) {
	a := &Agent{planModeAllowedTools: map[string]bool{"custom_tool": true}}
	blocked, _ := a.planModeBlocked("custom_tool", false, nil)
	if blocked {
		t.Error("tool in planModeAllowedTools should not be blocked")
	}
}

func TestPlanModeGenericWriterBlocked(t *testing.T) {
	blocked, msg := (&Agent{}).planModeBlocked("some_writer_tool", false, nil)
	if !blocked {
		t.Error("generic writer tool should be blocked in plan mode")
	}
	if !strings.Contains(msg, "writer tool") {
		t.Errorf("unexpected message: %s", msg)
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
	if blocked {
		t.Error("empty command should not be blocked")
	}
	blocked, _ = planModeBashBlocked(json.RawMessage(`{"command":""}`))
	if blocked {
		t.Error("empty string command should not be blocked")
	}
}

func TestPlanModeBashBlocked_InvalidJSON(t *testing.T) {
	blocked, _ := planModeBashBlocked(json.RawMessage(`not json`))
	if blocked {
		t.Error("invalid JSON should not be blocked (fail-open)")
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
}

func TestPlanMode_BashAllowedToolsOverride(t *testing.T) {
	// If bash is in planModeAllowedTools, it should bypass the bash validation
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})

	a := New(nil, reg, NewSession(""), Options{
		PlanModeAllowedTools: []string{"bash"},
	}, event.Discard)
	a.SetPlanMode(true)

	// Even an unsafe command should pass because bash is in allowedTools
	result := a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"rm -rf /"}`,
	})
	if strings.HasPrefix(result.output, "blocked:") {
		t.Errorf("bash in planModeAllowedTools should bypass validation: %s", result.output)
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
