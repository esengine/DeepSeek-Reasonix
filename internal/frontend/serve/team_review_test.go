package serve

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/teamroster"
)

type reviewCalls struct {
	mu      sync.Mutex
	prompts []string
}

func (r *reviewCalls) ask(_ context.Context, _, prompt string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, prompt)
	return `{"changes":[{"op":"add","description":"wants tabs everywhere","type":"user","body":"Tabs, always."}]}`, nil
}

func (r *reviewCalls) first() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prompts[0]
}

func (r *reviewCalls) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.prompts)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A turn the person spoke in is folded into the agent's own memory a moment
// after it ends; a turn with nothing new from the person asks no model, and the
// team's switch stops it altogether.
func TestATurnThePersonSpokeInIsFoldedIntoTheAgentsMemory(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	if err := h.team.service().UserSetMemoryReview(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	calls := &reviewCalls{}
	h.team.reviews.delay, h.team.reviews.ask = time.Millisecond, calls.ask
	seat := team.SeatRef{Agent: ids["Ada"]}
	rt := ensureSeat(t, h, seat)
	ctrl := rt.Server.Controller().(*control.Controller)
	ctrl.Submit("I always want tabs, never spaces")

	own := memory.Store{Dir: team.AgentMemoryDir(h.team.opts.Dir, ids["Ada"])}
	waitFor(t, "the review to save", func() bool {
		facts, _ := own.List()
		return len(facts) == 1 && facts[0].Description == "wants tabs everywhere"
	})
	if first := calls.first(); !strings.Contains(first, "I always want tabs, never spaces") {
		t.Fatalf("the review did not read what the person said:\n%s", first)
	}

	h.team.reviews.schedule(seat)
	time.Sleep(50 * time.Millisecond)
	if calls.count() != 1 {
		t.Fatalf("a review with nothing new asked a model again: %d calls", calls.count())
	}

	c := newTeamClient(t, h)
	if v := c.view("PATCH", "/team/prefs", map[string]bool{"memoryReview": false}); v.Prefs.MemoryReview {
		t.Fatalf("prefs after switching review off = %+v", v.Prefs)
	}
	ctrl.Submit("and use four-wide tabs")
	waitFor(t, "the second turn to end", func() bool { return !ctrl.Running() })
	time.Sleep(100 * time.Millisecond)
	if calls.count() != 1 {
		t.Fatalf("a review ran with the team's switch off: %d calls", calls.count())
	}
}

func TestATeamDoesNotReviewMemoryWithoutOptIn(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	if v := newTeamClient(t, h).view("GET", "/team", nil); v.Prefs.MemoryReview {
		t.Fatalf("a new team's prefs = %+v", v.Prefs)
	}
	c := newTeamClient(t, h)
	g := c.view("POST", "/team/groups", map[string]any{"members": []string{ids["Chief"], ids["Ada"]}}).Groups[0].ID
	if _, err := h.team.store.AppendLine(t.Context(), g, teamroster.GroupLine{From: teamroster.ByUser, Kind: teamroster.LinePost, Text: "remember my preferences"}); err != nil {
		t.Fatal(err)
	}
	calls := &reviewCalls{}
	h.team.reviews.ask = calls.ask
	h.team.reviews.review(team.SeatRef{Agent: ids["Chief"], Group: g})
	if calls.count() != 0 {
		t.Fatal("review transmitted conversation without opt-in")
	}
}

// In a group seat the review reads the group's log and folds it into the
// group's memory, each line once.
func TestAGroupsLogIsFoldedIntoTheGroupsMemory(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	if err := h.team.service().UserSetMemoryReview(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	calls := &reviewCalls{}
	h.team.reviews.ask = calls.ask
	c := newTeamClient(t, h)
	g := c.view("POST", "/team/groups", map[string]any{"members": []string{ids["Chief"], ids["Ada"]}}).Groups[0].ID
	if status, out := c.do("POST", "/team/groups/"+g+"/messages", map[string]any{"text": "the launch is on Friday"}); status != http.StatusOK {
		t.Fatalf("post = %d %s", status, out)
	}
	seat := team.SeatRef{Agent: ids["Ada"], Group: g}
	h.team.reviews.review(seat)
	facts, _ := team.SharedStore(h.team.opts.Dir, team.TierGroup, g).List()
	if len(facts) != 1 || !strings.Contains(calls.first(), "the launch is on Friday") {
		t.Fatalf("group memory = %+v after %d calls", facts, calls.count())
	}
	log, err := h.team.store.Lines(g, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, line := range log {
		if line.Event == teamroster.EventMemory && strings.Contains(line.Text, "1 added") {
			visible = true
		}
	}
	if !visible {
		t.Fatal("automatic shared-memory change was not shown in the group log")
	}
	h.team.reviews.review(seat)
	if calls.count() != 1 {
		t.Fatalf("the same log was reviewed twice: %d calls", calls.count())
	}
}
