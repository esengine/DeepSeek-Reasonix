package team

import (
	"strings"
	"testing"

	"reasonix/internal/state/teamroster"
)

func TestDiskScheduleCannotClaimUserAuthorityOrYolo(t *testing.T) {
	h := newHarness(t, "Chief", "Ben")
	_, err := h.store.Update(t.Context(), func(r *teamroster.Roster) error { r.Agents[1].Permission = "yolo"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	sc, err := h.store.SaveSchedule(t.Context(), teamroster.Schedule{Name: "disk task", Agent: h.id("Ben"), Prompt: "write file", Repeat: teamroster.RepeatOnce, Once: h.clock.now(), Next: h.clock.now(), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := h.store.Load()
	if err := h.svc.fire(t.Context(), r, sc); err != nil {
		t.Fatal(err)
	}
	got := h.inbox("Ben")
	if len(got) != 1 || got[0].Ceiling != "ask" {
		t.Fatalf("unverified scheduled delivery: %+v", got)
	}
	if strings.Contains(got[0].Text, "as if they had just asked") || !strings.Contains(got[0].Text, "author is not verified") {
		t.Fatalf("false provenance: %s", got[0].Text)
	}
}
