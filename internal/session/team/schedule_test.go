package team

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/state/teamroster"
)

func TestASavedScheduleNamesItsNextSlotAndATargetTheTeamHas(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	sc, err := h.svc.UserSaveSchedule(context.Background(), teamroster.Schedule{Name: "standup", Agent: h.id("Ada"), Prompt: "summarise yesterday", Repeat: teamroster.RepeatEvery, Every: 1, Unit: teamroster.UnitHours, Enabled: true})
	if err != nil || !sc.Next.Equal(h.clock.now().Add(time.Hour)) {
		t.Fatalf("saved = %+v, %v", sc, err)
	}
	if _, err := h.svc.UserSaveSchedule(context.Background(), teamroster.Schedule{Name: "x", Agent: "a-0000000000000000", Prompt: "p", Repeat: teamroster.RepeatDaily, Clock: "09:00", Enabled: true}); !errors.Is(err, teamroster.ErrUnknownAgent) {
		t.Fatalf("a schedule for no agent = %v", err)
	}
}

// A task that comes due is sent to its agent as a turn the host runs, once per
// slot, under the agent's own approval mode; a one-off then stops.
func TestADueTaskReachesItsAgentOnce(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	if _, err := h.svc.UserUpdateAgent(ctx, h.id("Ada"), UserAgentChange{Permission: new("ask")}); err != nil {
		t.Fatal(err)
	}
	sc, err := h.svc.UserSaveSchedule(ctx, teamroster.Schedule{Name: "report", Agent: h.id("Ada"), Prompt: "write the weekly report", Repeat: teamroster.RepeatOnce, Once: h.clock.now().Add(time.Minute), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	h.clock.add(time.Minute + 5*time.Second)
	for range 2 {
		if err := h.svc.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got := h.panes.pane(SeatRef{Agent: h.id("Ada")}).deliveries()
	if len(got) != 1 || got[0].Kind != DeliverSchedule || got[0].Ceiling != "ask" || !strings.Contains(got[0].Text, "write the weekly report") || !strings.Contains(got[0].Text, "<"+TagSchedule) {
		t.Fatalf("deliveries = %+v", got)
	}
	list, _ := h.store.Schedules()
	if len(list) != 1 || list[0].ID != sc.ID || list[0].Enabled || list[0].LastFired.IsZero() {
		t.Fatalf("after a one-off fired: %+v", list)
	}
}

// A slot that passed while nothing ticked is not made up; the task waits for
// its next one.
func TestAMissedSlotIsNotMadeUp(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	if _, err := h.svc.UserSaveSchedule(ctx, teamroster.Schedule{Name: "ping", Agent: h.id("Ada"), Prompt: "check the queue", Repeat: teamroster.RepeatEvery, Every: 30, Unit: teamroster.UnitMinutes, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.clock.add(3 * time.Hour)
	if err := h.svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if p, ok := h.panes.Live(SeatRef{Agent: h.id("Ada")}); ok && len(p.(*fakePane).deliveries()) > 0 {
		t.Fatal("a slot missed hours ago was made up")
	}
	list, _ := h.store.Schedules()
	if !list[0].Next.After(h.clock.now()) {
		t.Fatalf("the missed task's next slot is %v, not after now", list[0].Next)
	}
}

// A group's task is the user's message in the group, marked as the task's, and
// it opens a round as the user's own message would.
func TestADueGroupTaskIsPostedAsTheUsersMessage(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	g := groupOf(t, h, h.id("Chief"), h.id("Chief"), h.id("Ada"))
	sc, err := h.svc.UserSaveSchedule(ctx, teamroster.Schedule{Name: "plan", Group: g, Prompt: "plan the week", Repeat: teamroster.RepeatOnce, Once: h.clock.now().Add(time.Minute), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	h.clock.add(time.Minute + time.Second)
	if err := h.svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	lines, _ := h.store.Lines(g, 0, 0)
	if len(lines) != 1 || lines[0].From != teamroster.BySchedule || lines[0].Schedule != sc.ID || !strings.Contains(lines[0].Text, "plan the week") {
		t.Fatalf("group log = %+v", lines)
	}
	if rs, _ := h.store.Round(g); rs.Speaking == nil {
		t.Fatalf("the task's post opened no round: %+v", rs)
	}
}

func TestADisabledTaskNeverFires(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	if _, err := h.svc.UserSaveSchedule(ctx, teamroster.Schedule{Name: "off", Agent: h.id("Ada"), Prompt: "p", Repeat: teamroster.RepeatOnce, Once: h.clock.now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	h.clock.add(time.Minute + time.Second)
	_ = h.svc.Tick(ctx)
	if _, ok := h.panes.Live(SeatRef{Agent: h.id("Ada")}); ok {
		t.Fatal("a disabled task opened its agent")
	}
}

func TestDeletingAnAgentOrGroupDropsItsTasks(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	ctx := context.Background()
	g := groupOf(t, h, h.id("Chief"), h.id("Chief"), h.id("Ben"))
	for _, sc := range []teamroster.Schedule{
		{Name: "a", Agent: h.id("Ada"), Prompt: "p", Repeat: teamroster.RepeatDaily, Clock: "09:00", Enabled: true},
		{Name: "g", Group: g, Prompt: "p", Repeat: teamroster.RepeatDaily, Clock: "09:00", Enabled: true},
		{Name: "c", Agent: h.id("Chief"), Prompt: "p", Repeat: teamroster.RepeatDaily, Clock: "09:00", Enabled: true},
	} {
		if _, err := h.svc.UserSaveSchedule(ctx, sc); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.svc.UserDeleteAgent(ctx, h.id("Ada")); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UserDeleteGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	list, _ := h.store.Schedules()
	if len(list) != 1 || list[0].Name != "c" {
		t.Fatalf("tasks left = %+v", list)
	}
}

// What the user sent from a window without the team reaches the agent as the
// user's own words, on the holder's next tick, once.
func TestAQueuedUserMessageReachesItsAgentAsTheUsersWords(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	if _, err := h.store.QueueUserMessage(ctx, h.id("Ada"), "write the weekly report"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := h.svc.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got := h.panes.pane(SeatRef{Agent: h.id("Ada")}).deliveries()
	if len(got) != 1 || got[0].Kind != DeliverUserMessage || got[0].Text != "write the weekly report" || got[0].From != "terminal" || got[0].Ceiling != "ask" {
		t.Fatalf("deliveries = %+v", got)
	}
}
