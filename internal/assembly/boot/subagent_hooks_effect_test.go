package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/hook"
	"reasonix/internal/state/sessionstore"
)

const subagentHookProviderKind = "boot-subagent-hooks"

// subagentHookProvider scripts one delegation: the parent reads secret.txt,
// delegates through delegationTool, the child reads secret.txt, then both
// finish. Every request is recorded so the child's tool result can be checked.
type subagentHookProvider struct {
	mu             sync.Mutex
	delegationTool string
	reqs           []provider.Request
}

func (p *subagentHookProvider) Name() string { return subagentHookProviderKind }

func (p *subagentHookProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := len(p.reqs)
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	switch call {
	case 0:
		emitReadFile(ch, "parent-read", "secret.txt")
	case 1:
		args := `{"prompt":"read secret.txt"}`
		if strings.HasSuffix(p.delegationTool, "skill") {
			args = `{"name":"hook-probe","arguments":"read secret.txt"}`
		}
		if p.delegationTool == "parallel_tasks" {
			args = `{"tasks":[{"prompt":"read secret.txt","tools":["read_file"]},{"prompt":"read secret.txt","tools":["read_file"]}]}`
		}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "delegate-1", Name: p.delegationTool, Arguments: args}}
	case 2:
		emitReadFile(ch, "child-read", "secret.txt")
	default:
		// Parallel children can start in either order. Their tool list excludes
		// delegation, so a child without a tool result still needs its read.
		parent := false
		for _, tl := range req.Tools {
			parent = parent || tl.Name == "parallel_tasks"
		}
		if p.delegationTool == "parallel_tasks" && !parent && len(effectToolResults(req)) == 0 {
			emitReadFile(ch, "child-read", "secret.txt")
		} else {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
		}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *subagentHookProvider) childReadResult() (string, bool) {
	results := p.childReadResults()
	if len(results) == 0 {
		return "", false
	}
	return results[0], true
}

func (p *subagentHookProvider) childReadResults() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var results []string
	for _, req := range p.reqs {
		for _, m := range req.Messages {
			if m.Role == provider.RoleTool && m.ToolCallID == "child-read" {
				results = append(results, m.Content)
			}
		}
	}
	return results
}

var (
	subagentHookRegister sync.Once
	subagentHookMu       sync.Mutex
	subagentHookCurrent  *subagentHookProvider
)

func useSubagentHookProvider(t *testing.T, p *subagentHookProvider) {
	t.Helper()
	subagentHookRegister.Do(func() {
		provider.Register(subagentHookProviderKind, func(provider.Config) (provider.Provider, error) {
			subagentHookMu.Lock()
			defer subagentHookMu.Unlock()
			if subagentHookCurrent == nil {
				return nil, errors.New("subagent hook test provider is not installed")
			}
			return subagentHookCurrent, nil
		})
	})
	subagentHookMu.Lock()
	subagentHookCurrent = p
	subagentHookMu.Unlock()
	t.Cleanup(func() {
		subagentHookMu.Lock()
		subagentHookCurrent = nil
		subagentHookMu.Unlock()
	})
}

// TestBuildRunsPreToolUseInsideTaskSubagent covers every delegation entry the
// executor can call: task and read_only_task (TaskTool.subagentOptions), and
// run_skill and read_only_skill (RunProfileSpec and skillRunOptions).
func TestBuildRunsPreToolUseInsideTaskSubagent(t *testing.T) {
	for _, delegationTool := range []string{"task", "read_only_task", "run_skill", "read_only_skill"} {
		t.Run(delegationTool, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			prov := &subagentHookProvider{delegationTool: delegationTool}
			useSubagentHookProvider(t, prov)
			writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+subagentHookProviderKind+`"
model = "x"
`)
			approveWorkspace(t, dir)
			writeFile(t, dir, "secret.txt", roleHookSecret+"\n")
			if strings.HasSuffix(delegationTool, "skill") {
				writeFile(t, dir, ".reasonix/skills/hook-probe.md", "---\ndescription: inspect a file\nrunAs: subagent\nallowed-tools: read_file\n---\nRead the requested file.")
			}
			settings, logPath := denyReadSettings(t, dir)
			if err := hook.Save(hook.ScopeProject, dir, settings); err != nil {
				t.Fatal(err)
			}
			if err := hook.ApproveSavedProjectHooks(hook.LoadOptions{ProjectRoot: dir}, settings); err != nil {
				t.Fatal(err)
			}

			ctrl, err := Build(context.Background(), Options{Sink: event.Discard, SessionDir: filepath.Join(dir, "sessions")})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_ = ctrl.Run(ctx, "read secret.txt, then delegate reading it")

			result, ok := prov.childReadResult()
			if !ok {
				t.Fatalf("the %s child never got a result for its read_file", delegationTool)
			}
			if !strings.HasPrefix(result, "blocked:") {
				t.Fatalf("the %s child's read_file result = %q, want a PreToolUse block", delegationTool, result)
			}
			sessions := hookSessionIDs(t, logPath)
			if len(sessions) != 2 {
				t.Fatalf("PreToolUse calls = %d, want the parent's and the child's read_file; sessions=%q", len(sessions), sessions)
			}
			parent := sessionstore.BranchID(ctrl.SessionPath())
			if parent == "" || sessions[0] != parent {
				t.Fatalf("parent hook session = %q, want the session's own id %q", sessions[0], parent)
			}
			if !strings.HasPrefix(sessions[1], parent+":") {
				t.Fatalf("child hook session = %q, want one derived from the parent %q", sessions[1], parent)
			}
		})
	}
}

// hookSessionIDs reads the session id of every payload a logging hook saw.
func hookSessionIDs(t *testing.T, logPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("PreToolUse never ran: %v", err)
	}
	var out []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var p hook.Payload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("decode hook payload %q: %v", line, err)
		}
		if p.ToolName != "read_file" || p.SessionID == "" {
			t.Fatalf("hook payload = %+v, want read_file under a session id", p)
		}
		out = append(out, p.SessionID)
	}
	return out
}
