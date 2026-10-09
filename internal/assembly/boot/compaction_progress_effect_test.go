package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

const hostProgressHeading = "Host progress record"

// progressEffectProvider plays one scripted tool-using task, then pads every
// further turn so the window folds. Its digest names the changed file, which a
// fold must carry to be accepted, and nothing else: the task list and the check
// that passed reach a later request only through the host's own receipts.
type progressEffectProvider struct {
	mu     sync.Mutex
	reqs   []provider.Request
	script []*provider.ToolCall
	rounds int
	bulk   string
}

func (p *progressEffectProvider) Name() string { return "boot-progress-effect" }

func (p *progressEffectProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	digest := len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part")
	round := p.rounds
	if !digest {
		p.rounds++
	}
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	switch {
	case digest:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Files\n- notes.md was written"}
	case round < len(p.script):
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: p.script[round]}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: p.bulk}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *progressEffectProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func progressWants(check string) []string { return []string{"- notes.md", "- " + check} }

func progressNoteOf(req provider.Request) string {
	for _, m := range req.Messages {
		if strings.HasPrefix(m.Content, hostProgressHeading) {
			return m.Content
		}
	}
	return ""
}

func firstDigestRequest(reqs []provider.Request) int {
	for i, req := range reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "<compaction-summary>") {
				return i
			}
		}
	}
	return -1
}

func buildProgressEffect(t *testing.T, dir, kind string, rec *progressEffectProvider) *control.Controller {
	t.Helper()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	// Confinement is not under test, and a host without bubblewrap refuses to run the shell at all.
	writeUserConfig(t, "[sandbox]\nbash = \"off\"\n")
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 32000
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, WorkspaceRoot: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ctrl
}

var progressKinds atomic.Int64

func progressScript(finish bool, check string) []*provider.ToolCall {
	signoff := func(id string, step int, kind string) *provider.ToolCall {
		evidence := map[string]any{"kind": kind, "summary": "finished this step"}
		if kind == "verification" {
			evidence["command"] = check
		}
		return browserCall(id, "complete_step", map[string]any{"step_index": step, "result": "done", "evidence": []any{evidence}})
	}
	script := []*provider.ToolCall{
		browserCall("todo-1", "todo_write", map[string]any{"todos": []map[string]string{
			{"content": "Read the parser", "status": "in_progress"},
			{"content": "Write the notes", "status": "pending"},
		}}),
		signoff("step-1", 1, "manual"),
		browserCall("write-1", "write_file", map[string]any{"path": "notes.md", "content": "findings\n"}),
		browserCall("check-1", "bash", map[string]any{"command": check}),
	}
	if finish {
		script = append(script, signoff("step-2", 2, "verification"))
	}
	return script
}

// installFakePytest writes a passing verifier into the workspace and returns the
// command that runs it by absolute path. The host classifies a command by the
// program's base name; the tool shell does not inherit the test's PATH, and on
// Linux it runs with a private /tmp where only the workspace stays bound in.
func installFakePytest(t *testing.T, workspace string) string {
	t.Helper()
	writeFile(t, workspace, "tools/pytest", "#!/bin/sh\nexit 0\n")
	exe := filepath.Join(workspace, "tools", "pytest")
	if err := os.Chmod(exe, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(exe, "tests").CombinedOutput(); err != nil {
		t.Skipf("the verifier fixture cannot run on this machine: %v\n%s", err, out)
	}
	return exe + " tests"
}

func requireVerifierRan(t *testing.T, check string, reqs []provider.Request) {
	t.Helper()
	for _, req := range agentRequests(reqs) {
		for _, result := range effectToolResults(req) {
			if strings.HasPrefix(result, "error: command exited") {
				t.Fatalf("the tool shell could not run the verifier fixture %q, so no later assertion means anything:\n%s", check, result)
			}
		}
	}
}

func TestEffectFoldedProgressReachesTheRequestThroughRealBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the verifier fixture is a shell script")
	}
	cases := []struct {
		name   string
		finish bool
		want   []string
	}{
		{"unfinished list without ids", false, []string{"1) Read the parser (completed)", "2) Write the notes (in_progress)"}},
		{"finished list", true, []string{"1) Read the parser (completed)", "2) Write the notes (completed)"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			check := installFakePytest(t, dir)
			rec := &progressEffectProvider{bulk: strings.Repeat("work output line with detail. ", 600), script: progressScript(tc.finish, check)}
			ctrl := buildProgressEffect(t, dir, fmt.Sprintf("boot-progress-%d", progressKinds.Add(1)), rec)

			path := filepath.Join(dir, ".reasonix", "sessions", "progress.jsonl")
			ctrl.SetSessionPath(path)
			for _, prompt := range append([]string{"do the job"}, slices.Repeat([]string{"keep going"}, 11)...) {
				if err := ctrl.Run(context.Background(), prompt); err != nil && (tc.finish || prompt != "do the job") {
					requireVerifierRan(t, check, rec.requests())
					t.Fatalf("Run(%q): %v", prompt, err)
				}
			}
			ctrl.Close()

			reqs := rec.requests()
			requireVerifierRan(t, check, reqs)
			folded := firstDigestRequest(reqs)
			if folded < 0 {
				t.Fatalf("no request carried a digest; the fixture never compacted (%d requests)", len(reqs))
			}
			for i, req := range reqs[:folded] {
				if note := progressNoteOf(req); note != "" {
					t.Fatalf("request %d precedes any fold yet carries a progress record:\n%s", i, note)
				}
			}
			var checked int
			for i, req := range reqs[folded:] {
				if len(req.Tools) == 0 {
					continue
				}
				checked++
				note := progressNoteOf(req)
				for _, want := range append(append(progressWants(check), "do not redo"), tc.want...) {
					if !strings.Contains(note, want) {
						t.Fatalf("request %d after the fold: progress record lacks %q:\n%s\nmessages=%s", folded+i, want, note, messageDigest(req.Messages))
					}
				}
			}
			if checked == 0 {
				t.Fatal("no agent request followed the fold")
			}

			head := agentRequests(reqs)[0]
			for i, req := range agentRequests(reqs) {
				toolsA, _ := json.Marshal(head.Tools)
				toolsB, _ := json.Marshal(req.Tools)
				if string(toolsA) != string(toolsB) || head.Messages[0].Content != req.Messages[0].Content {
					t.Fatalf("request %d moved the cache-stable prefix", i)
				}
			}

			resumedRec := &progressEffectProvider{bulk: "ok"}
			resumed := buildProgressEffect(t, dir, fmt.Sprintf("boot-progress-%d", progressKinds.Add(1)), resumedRec)
			defer resumed.Close()
			loaded, err := sessionstore.LoadSession(path)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if err := resumed.Resume(loaded, path); err != nil {
				t.Fatalf("Resume: %v", err)
			}
			if err := resumed.Run(context.Background(), "and what is left?"); err != nil {
				t.Fatalf("Run after resume: %v", err)
			}
			after := agentRequests(resumedRec.requests())
			if len(after) == 0 {
				t.Fatal("no request after resume")
			}
			note := progressNoteOf(after[0])
			for _, want := range append(progressWants(check), tc.want...) {
				if !strings.Contains(note, want) {
					t.Fatalf("resumed request: progress record lacks %q:\n%s", want, note)
				}
			}
		})
	}
}
