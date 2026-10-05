package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

// TestApprovedPlanLeavesOneUserLineInHistory is the reload half of the live
// stream: a plan the user approves runs an execution turn the host composed, and
// that composition must not come back looking like a second thing the user said.
// The live stream drew one user line; so does the record.
func TestApprovedPlanLeavesOneUserLineInHistory(t *testing.T) {
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	exec := agent.New(testutil.NewMock("exec",
		testutil.Turn{Text: "here is the plan"},
		testutil.Turn{Text: "executed the approved plan"},
	), tool.NewRegistry(), sess, agent.Options{}, event.Discard)

	var c *control.Controller
	done := make(chan struct{}, 4)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			go c.Approve(e.Approval.ID, true, false, false)
		}
		if e.Kind == event.TurnDone {
			done <- struct{}{}
		}
	})
	c = control.New(control.Options{
		Runner: exec, Executor: exec, Sink: sink,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.Close()
	c.EnableInteractiveApproval()
	c.SetPlanMode(true)
	c.Submit("规划一下这件事")
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the turn never finished")
	}

	rec := httptest.NewRecorder()
	s := &Server{ctrl: c}
	s.history(rec, httptest.NewRequest(http.MethodGet, "/history", nil))
	var record []struct {
		Role         string `json:"role"`
		Content      string `json:"content"`
		HostAuthored bool   `json:"hostAuthored"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &record); err != nil {
		t.Fatalf("decode history: %v (%s)", err, rec.Body.String())
	}
	var typed, injected int
	for _, m := range record {
		if m.Role != "user" {
			continue
		}
		if m.HostAuthored {
			injected++
			continue
		}
		typed++
		if m.Content != "规划一下这件事" {
			t.Fatalf("a line the user never typed is recorded as theirs: %q", m.Content)
		}
	}
	if typed != 1 {
		t.Fatalf("user lines in the record = %d, want the one they typed", typed)
	}
	if injected == 0 {
		t.Fatal("no host-composed continuation ran; this test needs the approved execution to have happened")
	}
}
