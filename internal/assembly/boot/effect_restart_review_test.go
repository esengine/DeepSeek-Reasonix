package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

const reviewNudge = "high-risk changes require"

type plannedCall struct {
	name string
	args map[string]any
}

var (
	shipAuth = []plannedCall{
		{"todo_write", map[string]any{"todos": []map[string]string{{"content": "Ship auth", "status": "in_progress"}}}},
		{"write_file", map[string]any{"path": "auth/login.go", "content": "package auth\n"}},
		{"read_file", map[string]any{"path": "auth/login.go"}},
		{"bash", map[string]any{"command": "git diff --check"}},
		{"complete_step", signoff},
	}
	// reverifyAuth redoes everything the change owes except a review.
	reverifyAuth = []plannedCall{
		{"read_file", map[string]any{"path": "auth/login.go"}},
		{"bash", map[string]any{"command": "git diff --check"}},
		{"complete_step", signoff},
	}
	signoff = map[string]any{"step": "Ship auth", "result": "implemented",
		"evidence": []map[string]string{{"kind": "verification", "summary": "check passes", "command": "git diff --check"}}}
)

// restartReviewProvider plays every model by milestone, not by turn count: the
// agent's next call is the first one its phase planned that it has not made
// since the phase's user turn, and the goal evaluator always claims complete,
// leaving the host's readiness to accept or refuse the claim.
type restartReviewProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *restartReviewProvider) Name() string { return "boot-restart-review" }

func (p *restartReviewProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	// Every reply spends budget, so a goal that can never finish stops the way a
	// real one does, on its own token budget.
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 900, CompletionTokens: 100, TotalTokens: 1000}}
	switch next, ok := nextPlanned(req); {
	case strings.Contains(systemText(req), "decide whether the goal is complete"):
		ch <- provider.Chunk{Type: provider.ChunkText, Text: `{"outcome":"complete","reason":"the work is done"}`}
	case ok:
		raw, _ := json.Marshal(next.args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: next.name + "-" + phaseOf(req), Name: next.name, Arguments: string(raw)}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func systemText(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem {
			b.WriteString(m.Content)
		}
	}
	return b.String()
}

// phaseOf names the latest phase a user turn opened.
func phaseOf(req provider.Request) string {
	phase, _ := phaseStart(req)
	return phase
}

func phaseStart(req provider.Request) (string, int) {
	for i, m := range slices.Backward(req.Messages) {
		if m.Role != provider.RoleUser {
			continue
		}
		for _, phase := range []string{"PHASE-2", "PHASE-1"} {
			if strings.Contains(m.Content, phase) {
				return phase, i
			}
		}
	}
	return "", -1
}

func nextPlanned(req provider.Request) (plannedCall, bool) {
	if len(req.Tools) == 0 {
		return plannedCall{}, false
	}
	phase, start := phaseStart(req)
	plan := map[string][]plannedCall{"PHASE-1": shipAuth, "PHASE-2": reverifyAuth}[phase]
	made := map[string]bool{}
	for _, m := range req.Messages[start+1:] {
		for _, call := range m.ToolCalls {
			made[call.Name] = true
		}
	}
	for _, call := range plan {
		if !made[call.name] {
			return call, true
		}
	}
	return plannedCall{}, false
}

// nudgedInPhase2 reports whether phase two itself, not the history it carries,
// brought the host's demand for a review to the model.
func (p *restartReviewProvider) nudgedInPhase2() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.reqs {
		phase, start := phaseStart(req)
		if phase != "PHASE-2" || len(req.Tools) == 0 {
			continue
		}
		for _, m := range req.Messages[start+1:] {
			if strings.Contains(m.Content, reviewNudge) {
				return true
			}
		}
	}
	return false
}

// submitAndSettle submits input the way a window does, so goal turns carry the
// goal's delivery scope, and waits until the goal has left running and the
// controller is idle. An idle gap between continuations is not a settled goal.
func submitAndSettle(t *testing.T, ctrl *control.Controller, input string, rec *restartReviewProvider) {
	t.Helper()
	ctrl.Submit(input)
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		st := ctrl.RuntimeStatus()
		if !st.Running && !st.PendingPrompt && ctrl.GoalStatus() != control.GoalStatusRunning {
			return
		}
	}
	rec.mu.Lock()
	n := len(rec.reqs)
	var tail []string
	for _, req := range rec.reqs[max(0, n-4):] {
		last := req.Messages[len(req.Messages)-1]
		tail = append(tail, fmt.Sprintf("tools=%d %s: %.160s", len(req.Tools), last.Role, last.Content))
	}
	rec.mu.Unlock()
	t.Fatalf("%q never settled: status %+v goal %s, %d requests, tail %q", input, ctrl.RuntimeStatus(), ctrl.GoalStatus(), n, tail)
}

type restartArm struct {
	status string
	nudged bool
}

// runReviewArm plays both phases of a high-risk goal change, restarting the
// process between them when restart is set: the first controller closes, a new
// one is built, and the session is resumed from its file as --continue does.
func runReviewArm(t *testing.T, kind string, restart bool) restartArm {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &restartReviewProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	// A reply costs 1000 tokens: 8000 buys each phase its planned calls and a few
	// refused claims. Each extra refused turn only adds a full host turn of wall clock.
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
goal_token_budget = 8000

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	writeFile(t, dir, "REASONIX.md", "## Reasonix host checks\n\n- verify: git diff --check\n- sensitive: auth/**\n")
	gitInDir(t, dir, "init", "-q")

	build := func() *control.Controller {
		ctrl, err := Build(context.Background(), Options{Sink: event.Discard, AgentPreset: AgentPresetDelivery})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
		return ctrl
	}
	ctrl := build()
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "restart.jsonl"))
	ctrl.SetGoal("change the auth login")
	submitAndSettle(t, ctrl, "PHASE-1 implement the login change", rec)
	if _, err := os.Stat(filepath.Join(dir, "auth", "login.go")); err != nil {
		t.Fatalf("the change did not land in the workspace: %v", err)
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
		if ctrl.Goal() == "" {
			t.Fatal("the goal did not survive the restart, so there is nothing to compare")
		}
	}
	defer ctrl.Close()
	ctrl.ResumeGoal()
	submitAndSettle(t, ctrl, "PHASE-2 verify again and finish", rec)
	t.Logf("goal after phase two: %s", ctrl.GoalStatus())
	return restartArm{status: ctrl.GoalStatus(), nudged: rec.nudgedInPhase2()}
}

// The production form of the agent-level restart harness: two real boots and
// the controller's own resume. A high-risk goal change that never had a review
// is held to one whether or not the process restarted between its phases.
func TestEffectRestartKeepsTheReviewAHighRiskGoalOwes(t *testing.T) {
	uninterrupted := runReviewArm(t, uniqueKind("boot-restart-review-a"), false)
	restarted := runReviewArm(t, uniqueKind("boot-restart-review-b"), true)
	t.Logf("uninterrupted: goal %s, review demanded %v; restarted: goal %s, review demanded %v",
		uninterrupted.status, uninterrupted.nudged, restarted.status, restarted.nudged)
	if uninterrupted.status == control.GoalStatusComplete || !uninterrupted.nudged {
		t.Fatalf("the uninterrupted arm never held the change to a review, so it proves nothing: %+v", uninterrupted)
	}
	if restarted != uninterrupted {
		t.Fatalf("a restart between the phases changed the outcome: uninterrupted %+v, restarted %+v", uninterrupted, restarted)
	}
}

var kindSeq atomic.Int64

// uniqueKind keeps provider registrations apart when the test runs repeatedly.
func uniqueKind(base string) string { return fmt.Sprintf("%s-%d", base, kindSeq.Add(1)) }
