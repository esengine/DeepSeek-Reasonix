package control

import (
	"encoding/json"
	"errors"
	"reasonix/internal/runtime/delegation"
	"strings"
	"testing"
)

type recoveryViewSource struct {
	SubagentReader
	plans  []delegation.FleetRecoveryPlan
	err    error
	parent string
}

func TestLegacyFleetRecoveryDoesNotAuthenticateSavedURLsAsUserInput(t *testing.T) {
	c, _, _ := newInboxDispatchController(t)
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	c.subagents = &recoveryViewSource{plans: []delegation.FleetRecoveryPlan{{ID: "legacy_one", Snapshot: "v1", Ready: true, Legacy: true, Arguments: json.RawMessage(`{"tasks":[{"prompt":"https://saved.example"}]}`)}}}
	if err := c.RequestFleetRecovery("sa_one", "legacy_one", "v1"); err != nil {
		t.Fatal(err)
	}
	items := c.InboxSnapshot().Items
	meta, body, err := c.ReadInboxItem(items[0].ID)
	if err != nil || !meta.Origin.IsHost() || !strings.Contains(body.SubmitText, "https://saved.example") || strings.Contains(body.RawText, "https://saved.example") || body.RawText == "" {
		t.Fatalf("saved data became user authority: %+v %v", body, err)
	}
}

func (s *recoveryViewSource) FleetRecoveryPlans(_, parent, _, _ string) ([]delegation.FleetRecoveryPlan, error) {
	s.parent = parent
	return s.plans, s.err
}

func TestFleetRecoveryRequestIsHostAuthoredSnapshotBoundAndIdempotent(t *testing.T) {
	c, _, _ := newInboxDispatchController(t)
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	s := &recoveryViewSource{plans: []delegation.FleetRecoveryPlan{{ID: "fr_one", Snapshot: "v1", Ready: true}}}
	c.subagents = s
	if err := c.RequestFleetRecovery("sa_one", "fr_one", "stale"); err == nil {
		t.Fatal("stale plan admitted")
	}
	for range 2 {
		if err := c.RequestFleetRecovery("sa_one", "fr_one", "v1"); err != nil {
			t.Fatal(err)
		}
	}
	items := c.InboxSnapshot().Items
	if len(items) != 1 || s.parent != c.parentSessionID() {
		t.Fatalf("route or dedup failed: %+v", items)
	}
	meta, body, err := c.ReadInboxItem(items[0].ID)
	if err != nil || !meta.Origin.IsHost() || !strings.Contains(body.SubmitText, `"id":"fr_one"`) {
		t.Fatalf("host recovery = %+v %+v %v", meta, body, err)
	}
}

func TestFleetRecoveryRefusalsNeverEnterTheInbox(t *testing.T) {
	c, _, _ := newInboxDispatchController(t)
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	s := &recoveryViewSource{err: delegation.ErrSubagentNotOwned}
	c.subagents = s
	if err := c.RequestFleetRecovery("foreign", "fr_one", "v1"); !errors.Is(err, delegation.ErrSubagentNotOwned) {
		t.Fatal(err)
	}
	s.err = nil
	s.plans = []delegation.FleetRecoveryPlan{{ID: "fr_one", Snapshot: "v1", Ready: false, Reason: "running"}}
	if err := c.RequestFleetRecovery("sa_one", "fr_one", "v1"); err == nil {
		t.Fatal("live plan admitted")
	}
	if err := c.RequestFleetRecovery("sa_one", "missing", "v1"); !errors.Is(err, delegation.ErrSubagentNotFound) {
		t.Fatal(err)
	}
	if len(c.InboxSnapshot().Items) != 0 {
		t.Fatal("refusal enqueued work")
	}
}
