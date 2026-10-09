package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/hook"
	"reasonix/internal/safety/permission"
	"reasonix/internal/state/workspacelease"
)

type leaseRig struct {
	c     *Controller
	owner *workspacelease.Owner
	other *workspacelease.Owner
	file  string
}

func newLeaseRig(t *testing.T, timeout time.Duration) leaseRig {
	t.Helper()
	root, locks := t.TempDir(), t.TempDir()
	owner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{
		Policy:          permission.New("ask", nil, nil, nil),
		Sink:            event.Discard,
		WorkspaceLease:  owner,
		ApprovalTimeout: timeout,
	})
	c.EnableInteractiveApproval()
	owner.BeginRun()
	other.BeginRun()
	file := filepath.Join(root, "f.go")
	if err := owner.AcquirePaths(context.Background(), []string{file}); err != nil {
		t.Fatal(err)
	}
	return leaseRig{c: c, owner: owner, other: other, file: file}
}

// pendingAsk returns the ask on screen: a registered ask still queued behind
// the prompt lock is not the one a user can answer.
func (r leaseRig) pendingAsk(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.c.approval.mu.Lock()
		for id, ask := range r.c.approval.asks {
			if !ask.queued {
				r.c.approval.mu.Unlock()
				return id
			}
		}
		r.c.approval.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no ask became pending")
	return ""
}

func (r leaseRig) otherCanWrite(t *testing.T) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	return r.other.AcquirePaths(ctx, []string{r.file}) == nil
}

func TestAskYieldsTheWriteClaimAndTakesItBackOnAnswer(t *testing.T) {
	r := newLeaseRig(t, 0)
	done := make(chan error, 1)
	go func() {
		_, err := r.c.Ask(context.Background(), []event.AskQuestion{{ID: "q", Prompt: "p"}})
		done <- err
	}()
	id := r.pendingAsk(t)
	if r.owner.State().Acquired {
		t.Fatal("claim held while the ask waits on the user")
	}
	if !r.otherCanWrite(t) {
		t.Fatal("another session could not claim the path during the ask")
	}
	r.c.AnswerQuestion(id, []event.AskAnswer{{QuestionID: "q", Selected: []string{"x"}}})
	select {
	case <-done:
		t.Fatal("ask returned while another session holds the path")
	case <-time.After(300 * time.Millisecond):
	}
	r.other.EndRun()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ask never resumed")
	}
	if !r.owner.State().Acquired {
		t.Fatal("claim not taken again after the answer")
	}
	r.owner.EndRun()
	if r.owner.State().Acquired {
		t.Fatal("claim leaked past the run")
	}
}

func TestAskCancelledWhileYieldedLeaksNoClaim(t *testing.T) {
	r := newLeaseRig(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.c.Ask(ctx, []event.AskQuestion{{ID: "q", Prompt: "p"}})
		done <- err
	}()
	r.pendingAsk(t)
	if !r.otherCanWrite(t) {
		t.Fatal("path not free during the ask")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled ask returned no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled ask did not return")
	}
	r.owner.EndRun()
	r.other.EndRun()
	if r.owner.State().Acquired || r.other.State().Acquired {
		t.Fatal("a claim leaked after cancellation")
	}
}

func TestAskTimeoutWhileYieldedStillRetakesTheClaim(t *testing.T) {
	r := newLeaseRig(t, 40*time.Millisecond)
	if _, err := r.c.Ask(context.Background(), []event.AskQuestion{{ID: "q", Prompt: "p"}}); err == nil {
		t.Fatal("unanswered ask should time out")
	}
	if !r.owner.State().Acquired {
		t.Fatal("the run continues after a timeout and must hold its claim again")
	}
	r.owner.EndRun()
}

func TestApprovalYieldsTheWriteClaimWhileWaiting(t *testing.T) {
	r := newLeaseRig(t, 0)
	type result struct {
		reply approvalReply
		err   error
	}
	done := make(chan result, 1)
	go func() {
		reply, err := r.c.requestApprovalDecision(context.Background(), approvalRequest{tool: "bash", subject: "make"})
		done <- result{reply, err}
	}()
	var id string
	deadline := time.Now().Add(5 * time.Second)
	for id == "" && time.Now().Before(deadline) {
		r.c.approval.mu.Lock()
		for k := range r.c.approval.approvals {
			id = k
		}
		r.c.approval.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("no approval became pending")
	}
	if r.owner.State().Acquired || !r.otherCanWrite(t) {
		t.Fatal("claim held while the approval waits on the user")
	}
	r.other.EndRun()
	r.c.Approve(id, true, false, false)
	select {
	case got := <-done:
		if got.err != nil || !got.reply.allow {
			t.Fatalf("approval = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approval never resumed")
	}
	if !r.owner.State().Acquired {
		t.Fatal("claim not taken again after the approval")
	}
	r.owner.EndRun()
}

func awaitAsk(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(testenv.Budget(t)):
		t.Fatal("an answered ask never returned")
	}
}

func TestTwoAsksRetakeTheClaimOnlyAfterTheLastIsAnswered(t *testing.T) {
	r := newLeaseRig(t, 0)
	done := make(chan error, 2)
	ask := func(id string) {
		_, err := r.c.Ask(context.Background(), []event.AskQuestion{{ID: id, Prompt: "p"}})
		done <- err
	}
	go ask("q1")
	first := r.pendingAsk(t)
	go ask("q2")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.c.approval.mu.Lock()
		n := len(r.c.approval.asks)
		r.c.approval.mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.c.AnswerQuestion(first, []event.AskAnswer{{QuestionID: "q1", Selected: []string{"x"}}})
	awaitAsk(t, done)
	if r.owner.State().Acquired {
		t.Fatal("claim retaken while the second ask still waits on the user")
	}
	if !r.otherCanWrite(t) {
		t.Fatal("another session is blocked by a session that is only waiting on a person")
	}
	r.other.EndRun()
	id := r.pendingAsk(t)
	r.c.AnswerQuestion(id, []event.AskAnswer{{QuestionID: "q2", Selected: []string{"x"}}})
	awaitAsk(t, done)
	if !r.owner.State().Acquired {
		t.Fatal("claim not retaken after the last ask")
	}
	r.owner.EndRun()
}

func TestApprovalThatNeedsNoPromptDoesNotYield(t *testing.T) {
	r := newLeaseRig(t, 0)
	r.c.approval.grantSession("bash", "make")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reply, err := r.c.requestApprovalDecision(ctx, approvalRequest{tool: "bash", subject: "make"})
	if err != nil || !reply.allow {
		t.Fatalf("pre-approved request = (%+v, %v), want an immediate allow without a lease round trip", reply, err)
	}
	if !r.owner.State().Acquired {
		t.Fatal("a pre-approved request gave the claim up")
	}
	r.owner.EndRun()
}

func TestHookAutoAllowDoesNotYield(t *testing.T) {
	allowJSON := `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`
	root, locks := t.TempDir(), t.TempDir()
	owner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{
		Sink:           event.Discard,
		WorkspaceLease: owner,
		Hooks: hook.NewRunner([]hook.ResolvedHook{{
			HookConfig: hook.HookConfig{Command: "guard", PayloadFormat: "claude"},
			Event:      hook.PermissionRequest,
			Scope:      hook.ScopeGlobal,
		}}, "/tmp", func(context.Context, hook.SpawnInput) hook.SpawnResult {
			return hook.SpawnResult{Stdout: allowJSON}
		}, nil),
	})
	c.EnableInteractiveApproval()
	owner.BeginRun()
	if err := owner.AcquirePaths(context.Background(), []string{filepath.Join(root, "f.go")}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reply, err := c.requestApprovalDecision(ctx, approvalRequest{tool: "bash", subject: "make"})
	if err != nil || !reply.allow {
		t.Fatalf("hook-allowed request = (%+v, %v), want an allow without a lease round trip", reply, err)
	}
	if !owner.State().Acquired {
		t.Fatal("a hook-answered request gave the claim up")
	}
	owner.EndRun()
}
