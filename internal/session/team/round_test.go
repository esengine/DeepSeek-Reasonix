package team

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/state/teamroster"
)

func (h *harness) group(name string, mode teamroster.Mode, admin string, names ...string) string {
	h.t.Helper()
	var id string
	_, err := h.store.Update(context.Background(), func(r *teamroster.Roster) error {
		g := teamroster.Group{Name: name, Mode: mode, CreatedBy: teamroster.ByUser}
		for _, n := range names {
			g.Members = append(g.Members, h.id(n))
		}
		if admin != "" {
			g.Admin = h.id(admin)
		}
		id = r.AddGroup(g).ID
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) seatInbox(name, group string) []Delivery {
	return h.panes.pane(SeatRef{Agent: h.id(name), Group: group}).deliveries()
}

// lastTurn is the group turn most recently delivered to name in group.
func (h *harness) lastTurn(name, group string) Delivery {
	h.t.Helper()
	got := h.seatInbox(name, group)
	if len(got) == 0 {
		h.t.Fatalf("%s has had no turn in the group", name)
	}
	return got[len(got)-1]
}

func (h *harness) speaking(group string) string {
	h.t.Helper()
	rs, err := h.store.Round(group)
	if err != nil {
		h.t.Fatal(err)
	}
	if rs.Speaking == nil {
		return ""
	}
	for n, id := range h.ids {
		if id == rs.Speaking.Member {
			return n
		}
	}
	return rs.Speaking.Member
}

func (h *harness) end(name, group string, st Settlement) {
	h.t.Helper()
	st.Steps = []string{h.lastTurn(name, group).Step}
	if st.Outcome == "" {
		st.Outcome = OutcomeCompleted
	}
	if err := h.svc.Settled(context.Background(), SeatRef{Agent: h.id(name), Group: group}, st); err != nil {
		h.t.Fatalf("end %s's turn: %v", name, err)
	}
}

func (h *harness) log(group string) []teamroster.GroupLine {
	h.t.Helper()
	lines, err := h.store.Lines(group, 0, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	return lines
}

func (h *harness) post(group, text string, mentions ...string) {
	h.t.Helper()
	if _, err := h.svc.UserPost(context.Background(), group, text, mentions, false); err != nil {
		h.t.Fatal(err)
	}
}

func TestTheAdminAnswersAndMembersSpeakOneAtATime(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Launch", teamroster.ModeEveryone, "Ada", "Chief", "Ada", "Ben")
	h.post(g, "who is on the launch?")
	if h.speaking(g) != "Ada" || len(h.seatInbox("Chief", g)) != 0 || len(h.seatInbox("Ben", g)) != 0 {
		t.Fatalf("speaking %q; Chief %d, Ben %d", h.speaking(g), len(h.seatInbox("Chief", g)), len(h.seatInbox("Ben", g)))
	}
	turn := h.lastTurn("Ada", g)
	if turn.Kind != DeliverGroupTurn || turn.Group != g || turn.Step == "" || turn.Key != turn.Step || turn.From != teamroster.ByUser {
		t.Fatalf("turn = %+v", turn)
	}
	contains(t, turn.Text, `<group-turn group="Launch" round="1" step="1">`, "Speaking after you: Chief → Ben", `<line seq="1" from="user">`, "who is on the launch?", "pass_turn")
	h.end("Ada", g, Settlement{FinalText: "Ada here."})
	if h.speaking(g) != "Chief" {
		t.Fatalf("after Ada, %q speaks", h.speaking(g))
	}
	contains(t, h.lastTurn("Chief", g).Text, `from="Ada"`, "Ada here.", "Speaking after you: Ben")
	h.end("Chief", g, Settlement{FinalText: "Chief here."})
	ben := h.lastTurn("Ben", g).Text
	contains(t, ben, "No one is waiting to speak after you.", "Ada here.", "Chief here.")
	h.end("Ben", g, Settlement{FinalText: "Ben here."})
	if h.speaking(g) != "" {
		t.Fatalf("the round did not end: %q speaks", h.speaking(g))
	}
	log := h.log(g)
	if len(log) != 4 || log[1].Text != "Ada here." || log[3].From != h.id("Ben") {
		t.Fatalf("log = %+v", log)
	}
}

func TestAdminModeAndMentionsDecideWhoAnswers(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Launch", teamroster.ModeAdmin, "Chief", "Chief", "Ada", "Ben")
	h.post(g, "status?")
	h.end("Chief", g, Settlement{FinalText: "fine"})
	if h.speaking(g) != "" || len(h.seatInbox("Ada", g)) != 0 {
		t.Fatal("admin mode called more than the admin")
	}
	h.post(g, "Ben then Ada, please", "Ben", h.id("Ada"))
	if h.speaking(g) != "Ben" {
		t.Fatalf("mentions: %q speaks first", h.speaking(g))
	}
	if log := h.log(g); len(log[2].Mentions) != 2 || log[2].Mentions[0] != h.id("Ben") {
		t.Fatalf("mentions were not recorded on the line: %+v", log[2])
	}
	h.end("Ben", g, Settlement{FinalText: "ok"})
	if h.speaking(g) != "Ada" {
		t.Fatalf("second mention: %q", h.speaking(g))
	}
	if _, err := h.svc.UserPost(context.Background(), g, "hi", []string{"Nobody"}, false); !errors.Is(err, teamroster.ErrNotMember) {
		t.Fatalf("mention of a non-member = %v", err)
	}
}

func TestPassAndHandTurnAreStructured(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben", "Cy")
	g := h.group("Launch", teamroster.ModeEveryone, "Ada", "Ada", "Ben", "Cy")
	h.post(g, "go")
	ada := Caller{Agent: h.id("Ada"), Group: g, Step: h.lastTurn("Ada", g).Step}
	if err := h.svc.HandTurn(ada, []string{"Cy"}); err != nil {
		t.Fatal(err)
	}
	h.end("Ada", g, Settlement{FinalText: "Cy, your call."})
	if h.speaking(g) != "Cy" {
		t.Fatalf("hand_turn: %q speaks", h.speaking(g))
	}
	if log := h.log(g); log[1].Mentions[0] != h.id("Cy") {
		t.Fatalf("the handed post does not name Cy: %+v", log[1])
	}
	rs, _ := h.store.Round(g)
	if rs.Relays != 1 || rs.Speaking.Cause != teamroster.CauseHand || rs.Speaking.By != h.id("Ada") {
		t.Fatalf("round = %+v", rs)
	}
	cy := Caller{Agent: h.id("Cy"), Group: g, Step: h.lastTurn("Cy", g).Step}
	if err := h.svc.PassTurn(cy); err != nil {
		t.Fatal(err)
	}
	h.end("Cy", g, Settlement{FinalText: "a pass with words is still a pass"})
	if log := h.log(g); log[len(log)-1].Event != teamroster.EventPassed {
		t.Fatalf("pass = %+v", log[len(log)-1])
	}
	if h.speaking(g) != "Ben" {
		t.Fatalf("after the pass: %q", h.speaking(g))
	}
	for name, err := range map[string]error{
		"not speaking": h.svc.PassTurn(Caller{Agent: h.id("Ada"), Group: g, Step: ada.Step}),
		"no group":     h.svc.PassTurn(Caller{Agent: h.id("Ben")}),
		"hand to none": h.svc.HandTurn(Caller{Agent: h.id("Ben"), Group: g, Step: h.lastTurn("Ben", g).Step}, nil),
		"hand to self": h.svc.HandTurn(Caller{Agent: h.id("Ben"), Group: g, Step: h.lastTurn("Ben", g).Step}, []string{"Ben"}),
		"hand outside": h.svc.HandTurn(Caller{Agent: h.id("Ben"), Group: g, Step: h.lastTurn("Ben", g).Step}, []string{"Chief"}),
	} {
		want := map[string]error{"not speaking": ErrNotYourTurn, "no group": ErrNotInGroup, "hand to none": ErrHandSize, "hand to self": ErrHandSize, "hand outside": teamroster.ErrNotMember}[name]
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", name, err, want)
		}
	}
}

func TestAgentRelaysPauseAtTheLimitAndContinueResumesThem(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Ping", teamroster.ModeAdmin, "Ada", "Ada", "Ben")
	if _, err := h.store.Update(context.Background(), func(r *teamroster.Roster) error {
		gr, _ := r.Group(g)
		gr.RelayLimit = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.post(g, "ping pong")
	hand := func(from, to string) {
		c := Caller{Agent: h.id(from), Group: g, Step: h.lastTurn(from, g).Step}
		if err := h.svc.HandTurn(c, []string{to}); err != nil {
			t.Fatal(err)
		}
		h.end(from, g, Settlement{FinalText: from + " → " + to})
	}
	hand("Ada", "Ben")
	hand("Ben", "Ada")
	hand("Ada", "Ben")
	rs, _ := h.store.Round(g)
	if rs.Speaking != nil || len(rs.Parked) != 1 || rs.Relays != 2 {
		t.Fatalf("at the limit: %+v", rs)
	}
	last := h.log(g)[len(h.log(g))-1]
	if last.Event != teamroster.EventPaused || last.Count != 2 {
		t.Fatalf("no paused line: %+v", last)
	}
	if err := h.svc.Continue(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if h.speaking(g) != "Ben" {
		t.Fatalf("Continue: %q speaks", h.speaking(g))
	}
	if rs, _ := h.store.Round(g); rs.Relays != 1 || len(rs.Parked) != 0 {
		t.Fatalf("after Continue: %+v", rs)
	}
	hand("Ben", "Ada")
	hand("Ada", "Ben")
	if rs, _ := h.store.Round(g); len(rs.Parked) != 1 {
		t.Fatal("did not pause again")
	}
	h.post(g, "Ada, wrap up", "Ada")
	if h.speaking(g) != "Ada" {
		t.Fatalf("a user message runs before parked calls: %q", h.speaking(g))
	}
	if rs, _ := h.store.Round(g); rs.Relays != 0 || len(rs.Parked) != 0 || len(rs.Queue) != 1 || rs.Queue[0].Member != h.id("Ben") {
		t.Fatalf("the user's message did not reset relays and release the parked call: %+v", rs)
	}
}

func TestASilentSpeakerIsMissedAndALongToolKeepsWorkingOffTheOrder(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Launch", teamroster.ModeEveryone, "", "Ada", "Ben")
	ctx := context.Background()
	h.post(g, "go")
	ada := SeatRef{Agent: h.id("Ada"), Group: g}
	h.clock.add(SpeakerTimeout - time.Second)
	h.svc.Observed(ada, Observation{Kind: ObserveEvent, At: h.clock.now()})
	h.clock.add(2 * time.Second)
	if err := h.svc.Tick(ctx); err != nil || h.speaking(g) != "Ada" {
		t.Fatalf("activity did not keep Ada's turn: %q, %v", h.speaking(g), err)
	}
	h.clock.add(SpeakerTimeout)
	if err := h.svc.Tick(ctx); err != nil || h.speaking(g) != "Ben" {
		t.Fatalf("a silent speaker was not passed over: %q, %v", h.speaking(g), err)
	}
	if l := h.log(g)[1]; l.Event != teamroster.EventMissed || l.From != h.id("Ada") {
		t.Fatalf("missed line = %+v", l)
	}
	ben := SeatRef{Agent: h.id("Ben"), Group: g}
	h.svc.Observed(ben, Observation{Kind: ObserveToolStarted, At: h.clock.now()})
	h.clock.add(WorkGrace)
	if err := h.svc.Tick(ctx); err != nil || h.speaking(g) != "" {
		t.Fatalf("a long tool kept the turn: %q, %v", h.speaking(g), err)
	}
	if rs, _ := h.store.Round(g); len(rs.Working) != 2 {
		t.Fatalf("working = %v", rs.Working)
	}
	contains(t, h.lastTurn("Ben", g).Text, "go")
	h.end("Ben", g, Settlement{FinalText: "report attached"})
	h.end("Ada", g, Settlement{FinalText: ""})
	log := h.log(g)
	if l := log[len(log)-2]; l.Kind != teamroster.LinePost || !l.Late || l.Text != "report attached" {
		t.Fatalf("late reply = %+v", l)
	}
	if l := log[len(log)-1]; l.Event != teamroster.EventFinished || !l.Late {
		t.Fatalf("a late turn without text = %+v", l)
	}
	if rs, _ := h.store.Round(g); len(rs.Working) != 0 {
		t.Fatalf("still working: %v", rs.Working)
	}
}

func TestAFailedOrStoppedTurnIsLoggedWithItsCodeAndTheRoundMovesOn(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Launch", teamroster.ModeEveryone, "", "Ada", "Ben")
	h.post(g, "go")
	h.end("Ada", g, Settlement{Outcome: OutcomeFailed, Code: "provider.timeout", FinalText: "half"})
	if l := h.log(g)[1]; l.Event != teamroster.EventFailed || l.Code != "provider.timeout" || l.Text != "" {
		t.Fatalf("failed line = %+v", l)
	}
	if h.speaking(g) != "Ben" {
		t.Fatalf("after a failure %q speaks", h.speaking(g))
	}
	if err := h.svc.StopMember(context.Background(), g, h.id("Ben")); err != nil {
		t.Fatal(err)
	}
	if h.panes.pane(SeatRef{Agent: h.id("Ben"), Group: g}).cancelled != 1 {
		t.Fatal("stop did not cancel Ben's turn")
	}
	h.end("Ben", g, Settlement{Outcome: OutcomeCancelled})
	if l := h.log(g)[2]; l.Event != teamroster.EventStopped || l.From != h.id("Ben") {
		t.Fatalf("stopped line = %+v", l)
	}
}

func TestStoppingAQueuedMemberDropsItsTurn(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Launch", teamroster.ModeEveryone, "", "Ada", "Ben")
	h.post(g, "go")
	if err := h.svc.StopMember(context.Background(), g, h.id("Ben")); err != nil {
		t.Fatal(err)
	}
	h.end("Ada", g, Settlement{FinalText: "done"})
	if h.speaking(g) != "" || len(h.seatInbox("Ben", g)) != 0 {
		t.Fatal("a stopped member still got its turn")
	}
	if l := h.log(g)[1]; l.Event != teamroster.EventStopped || l.From != h.id("Ben") {
		t.Fatalf("stopped line = %+v", l)
	}
}

func TestARestartCutsTheTurnInFlightAndParksTheRest(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Launch", teamroster.ModeEveryone, "", "Ada", "Ben")
	h.post(g, "go")
	fresh := New(Options{Store: h.store, Panes: &fakePanes{}, Now: h.clock.now})
	if err := fresh.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	rs, _ := h.store.Round(g)
	if rs.Speaking != nil || len(rs.Queue) != 0 || len(rs.Parked) != 1 || rs.Parked[0].Member != h.id("Ben") {
		t.Fatalf("after recovery: %+v", rs)
	}
	if l := h.log(g)[1]; l.Event != teamroster.EventCut || l.Code != "team.interrupted" || l.From != h.id("Ada") {
		t.Fatalf("cut line = %+v", l)
	}
	if err := fresh.Continue(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if rs, _ := h.store.Round(g); rs.Speaking == nil || rs.Speaking.Member != h.id("Ben") {
		t.Fatalf("Continue after recovery: %+v", rs)
	}
}

func TestATurnShowsOnlyWhatIsNewAndBoundsIt(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Launch", teamroster.ModeAdmin, "Ada", "Ada", "Ben")
	ctx := context.Background()
	for i := range maxUnseenLines + 5 {
		if _, err := h.store.AppendLine(ctx, g, teamroster.GroupLine{From: h.id("Ben"), Kind: teamroster.LinePost, Text: "note " + strings.Repeat("x", i)}); err != nil {
			t.Fatal(err)
		}
	}
	h.post(g, "summarise")
	first := h.lastTurn("Ada", g).Text
	contains(t, first, "6 earlier lines are not shown; read them with read_group.", "summarise")
	if strings.Count(first, "<line ") != maxUnseenLines {
		t.Fatalf("shown %d lines", strings.Count(first, "<line "))
	}
	h.end("Ada", g, Settlement{FinalText: "Ada's summary"})
	h.post(g, "thanks")
	second := h.lastTurn("Ada", g).Text
	if strings.Contains(second, "note ") || strings.Contains(second, "Ada's summary") {
		t.Fatalf("the turn re-shows old lines or Ada's own post:\n%s", second)
	}
	contains(t, second, "thanks")
	read, err := h.svc.ReadGroup(Caller{Agent: h.id("Ada")}, g, 3, 0)
	if err != nil || strings.Count(read, "<line ") != 2 {
		t.Fatalf("read_group = %q, %v", read, err)
	}
	if _, err := h.svc.ReadGroup(Caller{Agent: h.id("Chief")}, g, 0, 0); err != nil {
		t.Fatalf("a main agent reading a group: %v", err)
	}
}

func TestAgentPostsFollowTheirAuthority(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben", "Cy")
	g := h.group("Launch", teamroster.ModeAdmin, "Ada", "Ada", "Ben")
	ctx := context.Background()
	if _, err := h.svc.AgentPost(ctx, Caller{Agent: h.id("Cy")}, g, "let me in", nil); !errors.Is(err, teamroster.ErrNotMember) {
		t.Fatalf("an outsider posting = %v", err)
	}
	if _, err := h.svc.AgentPost(ctx, Caller{Agent: h.id("Ben")}, g, "fyi", nil); err != nil || h.speaking(g) != "" {
		t.Fatalf("a plain member's post with no mentions called %q, %v", h.speaking(g), err)
	}
	if _, err := h.svc.AgentPost(ctx, Caller{Agent: h.id("Chief"), Ceiling: "ask"}, g, "status, team", nil); err != nil {
		t.Fatal(err)
	}
	rs, _ := h.store.Round(g)
	if h.speaking(g) != "Ada" || rs.Relays != 1 || rs.Speaking.Ceiling != "ask" || h.lastTurn("Ada", g).Ceiling != "ask" {
		t.Fatalf("a main agent's post: %q speaks, %+v", h.speaking(g), rs)
	}
}

func TestAGroupSeatProjectsTheGroup(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := h.group("Launch", teamroster.ModeAdmin, "Ada", "Ada", "Ben")
	p, err := h.svc.Projection(SeatRef{Agent: h.id("Ben"), Group: g})
	if err != nil {
		t.Fatal(err)
	}
	contains(t, p.Group, `You are in the group "Launch"`, "- Ada ("+h.id("Ada")+") · admin", "The admin answers")
	own, _ := h.svc.Projection(SeatRef{Agent: h.id("Ben")})
	if own.Identity != p.Identity || own.Roster != p.Roster || own.Group != "" {
		t.Fatal("a group seat's identity or roster differs from the agent's own chat")
	}
	if left, err := h.svc.Projection(SeatRef{Agent: h.id("Chief"), Group: g}); err != nil || left.Group != "" || left.Identity == "" {
		t.Fatalf("a seat outside the group = %+v, %v", left, err)
	}
}
