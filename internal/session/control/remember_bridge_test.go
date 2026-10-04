package control

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/memory"
)

// rememberGate builds a controller whose only variable is the switch, with one
// project fact already stored, and records what the sink was told.
func rememberGate(t *testing.T, on bool) (*Controller, memory.Store, *[]event.Event) {
	t.Helper()
	store := memory.Store{Dir: testenv.TempDir(t), GlobalDir: testenv.TempDir(t)}
	if _, err := store.SaveWithOptions(memory.Memory{
		Name: "release-target", Type: memory.TypeProject, Scope: memory.FactScopeProject,
		Description: "Project release target", Body: "Release from main-v2.",
	}, memory.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	var seen []event.Event
	c := New(Options{
		Memory:                     &memory.Set{Store: store},
		AutoConfirmProjectRemember: on,
		Sink:                       event.FuncSink(func(e event.Event) { seen = append(seen, e) }),
	})
	return c, store, &seen
}

func receipts(seen []event.Event) int {
	n := 0
	for _, e := range seen {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeMemorySavedUnasked {
			n++
		}
	}
	return n
}

// The switch's one increment over the low-risk path is an update: that path
// already writes a new, bounded, non-sensitive project fact without asking.
var updateToAProjectFact = json.RawMessage(`{"name":"release-target","type":"project","description":"Project release target","body":"Release from main-v2, tags included."}`)

// The gate itself answers for a project update and nothing else, and the receipt
// follows the write it authorized - exactly once - rather than the decision.
func TestTheSwitchAnswersOnlyTheProjectUpdate(t *testing.T) {
	off, _, _ := rememberGate(t, false)
	if got := off.allowRememberByScope(updateToAProjectFact); got.AutoAllow {
		t.Fatalf("switch off = %+v, want asking", got)
	}

	on, _, seen := rememberGate(t, true)
	allow, remember, reason, err := gateApprover{on}.approveWithPolicyReason(context.Background(), memoryRememberTool, "", updateToAProjectFact, "")
	if err != nil || !allow || remember || reason != "" {
		t.Fatalf("gate with the switch on = (%v,%v,%q,%v)", allow, remember, reason, err)
	}
	if got := on.allowRememberByScope(json.RawMessage(`{"name":"prefers-go","type":"user","description":"Preferred language","body":"Prefer Go."}`)); got.AutoAllow {
		t.Fatalf("a preference = %+v, want asking", got)
	}
	if receipts(*seen) != 0 {
		t.Fatal("a receipt was emitted before the write")
	}
	// The decision left the note; the write pays it, and pays it once.
	on.memory.markReceipt("release-target")
	on.QueueMemory(`Saved memory "release-target" (project): Project release target`)
	if got := receipts(*seen); got != 1 {
		t.Fatalf("receipts after the write = %d, want 1", got)
	}
	on.QueueMemory(`Saved memory "release-target" (project): Project release target`)
	if got := receipts(*seen); got != 1 {
		t.Fatalf("receipts after a second report = %d, want the note consumed once", got)
	}
	// A different fact's save is not this write's report, so it pays nothing.
	on.memory.markReceipt("release-target")
	on.QueueMemory(`Saved memory "other-fact" (project): Another fact`)
	if got := receipts(*seen); got != 1 {
		t.Fatalf("receipts after an unrelated save = %d, want 1", got)
	}
}

// A session without an approver answers with the low-risk path alone, so the
// switch cannot widen what it writes: an update is not what that path allows.
func TestTheSwitchDoesNotReachTheUnattendedGate(t *testing.T) {
	c, _, _ := rememberGate(t, true)
	if c.allowLowRiskRemember(updateToAProjectFact) {
		t.Fatal("the low-risk path allowed an update, so the unattended gate would too")
	}
	gate := c.newHeadlessGate(ToolApprovalAsk, nil)
	if gate == nil || gate.allowLowRiskFreshAction == nil {
		t.Fatal("the unattended gate has no low-risk answer to check")
	}
	if gate.allowLowRiskFreshAction(memoryRememberTool, updateToAProjectFact) {
		t.Fatal("the unattended gate let an update through on the switch, which it cannot see")
	}
	if !gate.allowLowRiskFreshAction(memoryRememberTool, json.RawMessage(`{"name":"fresh-fact","type":"project","description":"Fresh fact","body":"Bounded."}`)) {
		t.Fatal("the unattended gate stopped answering on its own low-risk path")
	}
}

// The switch covers remember and nothing else: forget is not its business, and
// neither is the fresh-approval requirement that keeps the guardian out of it.
// A fact the switch wrote is an ordinary fact, so /forget still takes it back.
func TestForgetAndTheGuardianAreOutsideTheSwitch(t *testing.T) {
	c, store, _ := rememberGate(t, true)
	if c.newHeadlessGate(ToolApprovalAsk, nil).allowLowRiskFreshAction(memoryForgetTool, json.RawMessage(`{"name":"release-target"}`)) {
		t.Fatal("the unattended gate answered for forget")
	}
	if !requiresFreshApprovalTool(memoryRememberTool) {
		t.Fatal("remember no longer requires a fresh human approval, so the guardian could answer it")
	}
	if _, err := memory.NewForgetTool(store).Execute(context.Background(), json.RawMessage(`{"name":"release-target"}`)); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Read("release-target"); ok {
		t.Fatal("the fact survived /forget")
	}
}
