package teamroster

import (
	"context"
	"errors"
	"testing"
	"time"
)

var shanghai = time.FixedZone("CST", 8*3600)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, shanghai)
	if err != nil {
		panic(err)
	}
	return t
}

func TestEachRepeatNamesItsNextSlot(t *testing.T) {
	now := at("2026-10-26 09:30") // a Monday
	for _, tc := range []struct {
		name string
		s    Schedule
		want time.Time
	}{
		{"once later", Schedule{Repeat: RepeatOnce, Once: at("2026-10-27 08:00")}, at("2026-10-27 08:00")},
		{"once past", Schedule{Repeat: RepeatOnce, Once: at("2026-10-25 08:00")}, time.Time{}},
		{"daily today", Schedule{Repeat: RepeatDaily, Clock: "18:00"}, at("2026-10-26 18:00")},
		{"daily tomorrow", Schedule{Repeat: RepeatDaily, Clock: "09:00"}, at("2026-10-27 09:00")},
		{"weekly wed fri", Schedule{Repeat: RepeatWeekly, Clock: "10:00", Days: []int{3, 5}}, at("2026-10-28 10:00")},
		{"weekly today later", Schedule{Repeat: RepeatWeekly, Clock: "10:00", Days: []int{1}}, at("2026-10-26 10:00")},
		{"every 90 minutes", Schedule{Repeat: RepeatEvery, Every: 90, Unit: UnitMinutes, Once: at("2026-10-26 09:00")}, at("2026-10-26 10:30")},
		{"every 2 days", Schedule{Repeat: RepeatEvery, Every: 2, Unit: UnitDays, Once: at("2026-10-20 07:00")}, at("2026-10-28 07:00")},
	} {
		if got := tc.s.NextAfter(now, shanghai); !got.Equal(tc.want) {
			t.Errorf("%s: next = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAScheduleIsCheckedBeforeItIsKept(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	ok := Schedule{Name: "standup", Agent: newAgentID(), Prompt: "summarise yesterday", Repeat: RepeatDaily, Clock: "09:00", Enabled: true}
	for name, bad := range map[string]Schedule{
		"two targets":   {Name: "x", Agent: newAgentID(), Group: newGroupID(), Prompt: "p", Repeat: RepeatDaily, Clock: "09:00"},
		"no target":     {Name: "x", Prompt: "p", Repeat: RepeatDaily, Clock: "09:00"},
		"no prompt":     {Name: "x", Agent: newAgentID(), Repeat: RepeatDaily, Clock: "09:00"},
		"bad clock":     {Name: "x", Agent: newAgentID(), Prompt: "p", Repeat: RepeatDaily, Clock: "25:00"},
		"no days":       {Name: "x", Agent: newAgentID(), Prompt: "p", Repeat: RepeatWeekly, Clock: "09:00"},
		"too often":     {Name: "x", Agent: newAgentID(), Prompt: "p", Repeat: RepeatEvery, Every: 5, Unit: UnitMinutes},
		"unknown every": {Name: "x", Agent: newAgentID(), Prompt: "p", Repeat: RepeatEvery, Every: 1, Unit: "weeks"},
		"unknown kind":  {Name: "x", Agent: newAgentID(), Prompt: "p", Repeat: "hourly"},
	} {
		if _, err := s.SaveSchedule(context.Background(), bad); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrEmpty) {
			t.Errorf("%s: SaveSchedule = %v, want a field error", name, err)
		}
	}
	saved, err := s.SaveSchedule(context.Background(), ok)
	if err != nil || !scheduleID.MatchString(saved.ID) {
		t.Fatalf("SaveSchedule = %+v, %v", saved, err)
	}
	saved.Enabled = false
	if _, err := s.SaveSchedule(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	list, err := s.Schedules()
	if err != nil || len(list) != 1 || list[0].Enabled || list[0].Name != "standup" {
		t.Fatalf("Schedules = %+v, %v", list, err)
	}
	if err := s.DeleteSchedule(context.Background(), saved.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Schedules(); len(list) != 0 {
		t.Fatalf("after delete: %+v", list)
	}
	if err := s.DeleteSchedule(context.Background(), saved.ID); !errors.Is(err, ErrUnknownSchedule) {
		t.Fatalf("deleting a missing schedule = %v", err)
	}
}
