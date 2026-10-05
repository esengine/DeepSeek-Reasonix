package boot

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// restartIsolationProvider delegates an isolated task in the DELEGATE turn and
// leaves its result pending; in the APPLY turn it applies the id the transcript
// carries and reports what the host answered.
type restartIsolationProvider struct {
	mu          sync.Mutex
	applyResult string
}

func (p *restartIsolationProvider) Name() string { return "boot-restart-isolation" }

func (p *restartIsolationProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	last := lastToolResult(req)
	lastUser := ""
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			lastUser = m.Content
		}
	}
	switch {
	case strings.Contains(firstUser(req), isolatedTask):
		if hasToolResult(req) {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "wrote out.txt"}
		} else {
			call("w1", "write_file", map[string]string{"path": "out.txt", "content": "made in isolation\n"})
		}
	case strings.Contains(lastUser, "APPLY") && !toolResultAfterLastUser(req):
		id := ""
		for _, m := range req.Messages {
			if found := isoIDPattern.FindString(m.Content); found != "" {
				id = found
			}
		}
		call("a1", "apply_isolated", map[string]string{"id": id})
	case strings.Contains(lastUser, "APPLY"):
		p.mu.Lock()
		p.applyResult = last
		p.mu.Unlock()
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	case hasToolResult(req):
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "left pending"}
	default:
		call("t1", "task", map[string]string{"prompt": isolatedTask, "isolation": "worktree"})
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func toolResultAfterLastUser(req provider.Request) bool {
	seen := false
	for _, m := range req.Messages {
		switch m.Role {
		case provider.RoleUser:
			seen = false
		case provider.RoleTool:
			seen = true
		}
	}
	return seen
}

type isolationArm struct {
	landed      bool
	applyResult string
}

// runIsolationArm delegates an isolated task in one turn and applies it in the
// next, restarting the process between the two when restart is set.
func runIsolationArm(t *testing.T, kind string, restart bool) isolationArm {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &restartIsolationProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
worktree_isolation = true

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	gitInDir(t, dir, "init", "-q")
	// Attempts run in worktrees, each a folder of its own: the model is the
	// user's, so an approval of this folder is not what makes them run.
	mirrorToUserConfig(t, dir)
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "init")

	build := func() *control.Controller {
		ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
		return ctrl
	}
	ctrl := build()
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "isolation.jsonl"))
	if err := ctrl.Run(context.Background(), "DELEGATE make out.txt in isolation"); err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if restart {
		path := ctrl.SessionPath()
		ctrl.Close()
		loaded, err := sessionstore.LoadSession(path)
		if err != nil || loaded == nil {
			t.Fatalf("load %s: %v", path, err)
		}
		ctrl = build()
		if err := ctrl.Resume(loaded, path); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "APPLY the pending isolated result"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	_, statErr := os.Stat(filepath.Join(dir, "out.txt"))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return isolationArm{landed: statErr == nil, applyResult: rec.applyResult}
}

// A pending isolated result is work the model was told it holds. Uninterrupted,
// the next turn applies it; restarted between the turns, the same id should be
// just as applicable.
func TestEffectRestartKeepsAPendingIsolatedResult(t *testing.T) {
	uninterrupted := runIsolationArm(t, uniqueKind("boot-restart-iso-a"), false)
	restarted := runIsolationArm(t, uniqueKind("boot-restart-iso-b"), true)
	t.Logf("uninterrupted: landed %v, apply said %.120q", uninterrupted.landed, uninterrupted.applyResult)
	t.Logf("restarted: landed %v, apply said %.160q", restarted.landed, restarted.applyResult)
	if !uninterrupted.landed {
		t.Fatalf("the uninterrupted arm never applied its result, so it proves nothing: %+v", uninterrupted)
	}
	if restarted.landed != uninterrupted.landed {
		t.Fatalf("a restart between delegating and applying lost the isolated result: %q", restarted.applyResult)
	}
}
