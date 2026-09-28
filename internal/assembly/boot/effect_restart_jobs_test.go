package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

var jobIDPattern = regexp.MustCompile(`bash-[0-9]+`)

// restartJobsProvider starts a background job in the START turn and reads it
// in the CHECK turn, keeping what the host answered. Any other turn, such as
// the host delivering a job's interruption, is only acknowledged.
type restartJobsProvider struct {
	mu     sync.Mutex
	answer string
}

func (p *restartJobsProvider) Name() string { return "boot-restart-jobs" }

func (p *restartJobsProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool && m.Name == "bash_output" {
			p.mu.Lock()
			p.answer = m.Content
			p.mu.Unlock()
		}
	}
	ch := make(chan provider.Chunk, 2)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	lastUser := ""
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			lastUser = m.Content
		}
	}
	switch {
	case toolResultAfterLastUser(req):
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "noted"}
	case strings.Contains(lastUser, "CHECK"):
		id := ""
		for _, m := range req.Messages {
			if found := jobIDPattern.FindString(m.Content); found != "" {
				id = found
			}
		}
		call("o1", "bash_output", map[string]string{"job_id": id})
	case strings.Contains(lastUser, "START"):
		call("b1", "bash", map[string]any{"command": "sleep 30", "run_in_background": true})
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "noted"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func runJobsArm(t *testing.T, kind string, restart bool) string {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &restartJobsProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	// What is measured is how a job's end is attributed, not where it runs.
	writeUserConfig(t, "[sandbox]\nbash = \"off\"\n")
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)
	build := func() *control.Controller {
		ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
		return ctrl
	}
	ctrl := build()
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "jobs.jsonl"))
	if err := ctrl.Run(context.Background(), "START a background job"); err != nil {
		t.Fatalf("start: %v", err)
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
	if err := ctrl.Run(context.Background(), "CHECK the background job"); err != nil {
		t.Fatalf("check: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.answer
}

// A background job cannot outlive the process running it, so a restart cannot
// be equivalent here. What it owes is attribution: the model reading the job
// afterwards is told the session ended under it, not that someone killed it.
func TestEffectRestartNamesWhyABackgroundJobEnded(t *testing.T) {
	uninterrupted := runJobsArm(t, uniqueKind("boot-restart-jobs-a"), false)
	restarted := runJobsArm(t, uniqueKind("boot-restart-jobs-b"), true)
	t.Logf("uninterrupted: %.120q; restarted: %.220q", uninterrupted, restarted)
	if !strings.HasPrefix(uninterrupted, "[bash-1] "+string(jobs.Running)) {
		t.Fatalf("the uninterrupted job is not running, so it proves nothing: %q", uninterrupted)
	}
	want := "[bash-1] " + string(jobs.Interrupted) + jobs.Interrupted.Cause()
	if !strings.HasPrefix(restarted, want) {
		t.Fatalf("after a restart the job reads %q, want it attributed as %q", restarted, want)
	}
}
