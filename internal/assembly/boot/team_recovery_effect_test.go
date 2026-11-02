package boot

import (
	"testing"

	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/teamroster"
)

func TestRecoveredExchangeCannotReplayItsDurableInbox(t *testing.T) {
	h := newTeamApprovalHarness(t, "team-feedback-recovery", nil)
	if err := h.ctrl.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	r, err := h.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	x, _, err := h.store.OpenExchange(t.Context(), teamroster.Exchange{From: teamroster.ByUser, To: r.Agents[0].ID, ReplyTo: teamroster.ReplyToUser}, "stale work")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.ctrl.TryEnqueueFollowup(control.InboxRequest{Origin: sessioninbox.OriginHost, Display: "stale work", Raw: "stale work", Submit: "stale work", Extra: map[string]string{team.ExtraKind: string(team.DeliverMessage), team.ExtraExchange: x.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := team.New(team.Options{Store: h.store}).Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.ctrl.SetInboxPaused(false); err != nil {
		t.Fatal(err)
	}
	h.waitTurn()
	if got := len(h.prov.requests()); got != 0 {
		t.Fatalf("interrupted exchange made %d provider calls", got)
	}
}

func TestRecoveredGroupStepCannotReplayItsDurableInbox(t *testing.T) {
	h := newTeamApprovalHarness(t, "team-feedback-group-recovery", nil)
	if err := h.ctrl.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	var seat team.SeatRef
	_, err := h.store.Update(t.Context(), func(r *teamroster.Roster) error {
		seat.Agent = r.Agents[0].ID
		ben := r.AddAgent(teamroster.Agent{Name: "Ben", CreatedBy: teamroster.ByUser})
		seat.Group = r.AddGroup(teamroster.Group{Members: []string{seat.Agent, ben.ID}, Mode: teamroster.ModeEveryone}).ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	const step = "old-step"
	if err := h.store.SaveRound(t.Context(), seat.Group, teamroster.RoundState{Speaking: &teamroster.Speaking{Slot: teamroster.Slot{Member: seat.Agent, Cause: teamroster.CauseUser, By: teamroster.ByUser}, Key: step}}); err != nil {
		t.Fatal(err)
	}
	fresh := team.New(team.Options{Store: h.store})
	h.ctrl.BindTeamSeat(team.Seat{Service: fresh, Ref: seat})
	_, err = h.ctrl.TryEnqueueFollowup(control.InboxRequest{Origin: sessioninbox.OriginHost, Display: "old group step", Raw: "old group step", Submit: "old group step", Extra: map[string]string{team.ExtraKind: string(team.DeliverGroupTurn), team.ExtraStep: step}})
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.ctrl.SetInboxPaused(false); err != nil {
		t.Fatal(err)
	}
	h.waitTurn()
	if len(h.prov.requests()) != 0 {
		t.Fatal("cut group step reached the provider")
	}
}

func TestGroupDeliveryPublishesBeforeInboxChecksTheRound(t *testing.T) {
	h := newTeamApprovalHarness(t, "team-feedback-group-live", nil)
	var seat team.SeatRef
	_, err := h.store.Update(t.Context(), func(r *teamroster.Roster) error {
		seat.Agent = r.Agents[0].ID
		ben := r.AddAgent(teamroster.Agent{Name: "Ben", CreatedBy: teamroster.ByUser})
		seat.Group = r.AddGroup(teamroster.Group{Members: []string{seat.Agent, ben.ID}, Mode: teamroster.ModeEveryone}).ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	panes := &mixedPanes{real: map[team.SeatRef]team.Pane{seat: ctrlPane{h.ctrl}}}
	svc := team.New(team.Options{Store: h.store, Panes: panes})
	h.ctrl.BindTeamSeat(team.Seat{Service: svc, Ref: seat})
	if _, err := svc.UserPost(t.Context(), seat.Group, "Discuss the plan", []string{seat.Agent}, false); err != nil {
		t.Fatal(err)
	}
	h.waitTurn()
	if len(h.prov.requests()) == 0 {
		t.Fatal("current group turn was discarded")
	}
}
