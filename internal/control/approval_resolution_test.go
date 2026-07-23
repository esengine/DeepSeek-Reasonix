package control

import (
	"runtime"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/recovery"
)

func TestResolveApprovalReportsLiveAndStaleIDs(t *testing.T) {
	c := New(Options{})
	id, reply := c.approval.register("bash", "go test ./...", "")

	if !c.ResolveApproval(id, true, true, false) {
		t.Fatal("live approval was reported stale")
	}
	got := <-reply
	if !got.allow || !got.session || got.persist {
		t.Fatalf("reply = %+v, want allow+session without persist", got)
	}
	if c.ResolveApproval(id, false, false, false) {
		t.Fatal("resolved approval was accepted a second time")
	}
	if c.ResolveApproval("missing", true, false, false) {
		t.Fatal("unknown approval was reported live")
	}
}

func TestRecoveryTypedAndLegacyResolutionHaveOneAtomicOwner(t *testing.T) {
	c := New(Options{})
	gate := recovery.NewGate(recovery.Options{})
	c.mu.Lock()
	c.recoveryGate = gate
	c.mu.Unlock()

	id, reply := c.approval.registerDecisionKind(
		"bash", "git push origin feature", "confirm", true,
		recovery.ApprovalKindRecovery, nil,
	)
	gate.BindApprovalID("root", id)

	// Park the typed resolver after it atomically consumes the gate but before
	// it can retire the approval-manager mirror. This is the old double-success
	// window: the legacy resolver must not claim that mirror as an ordinary
	// approval while the typed resolver is paused here.
	c.approval.mu.Lock()
	typedDone := make(chan error, 1)
	go func() {
		typedDone <- c.ResolveRecovery(id, agent.RecoveryActionContinue, "")
	}()
	deadline := time.Now().Add(2 * time.Second)
	for gate.HasApproval(id) {
		if time.Now().After(deadline) {
			c.approval.mu.Unlock()
			t.Fatal("typed resolver did not consume the recovery gate")
		}
		runtime.Gosched()
	}

	legacyDone := make(chan bool, 1)
	go func() {
		legacyDone <- c.ResolveApproval(id, false, false, false)
	}()
	c.approval.mu.Unlock()

	if err := <-typedDone; err != nil {
		t.Fatalf("typed resolution failed: %v", err)
	}
	if legacyOK := <-legacyDone; legacyOK {
		t.Fatal("conflicting legacy resolution also reported success")
	}
	got := <-reply
	if !got.allow {
		t.Fatalf("final action = revise, want typed winner's continue: %+v", got)
	}
	metrics := gate.Metrics()
	if metrics.HumanContinues != 1 || metrics.HumanRevises != 0 {
		t.Fatalf("recovery metrics = %+v, want exactly one typed continue", metrics)
	}
}
