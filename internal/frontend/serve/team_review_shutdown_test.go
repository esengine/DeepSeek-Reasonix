package serve

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

func TestTeamReviewStopsBeforeTheWriterLeaseIsReleased(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	if err := h.team.service().UserSetMemoryReview(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	c := newTeamClient(t, h)
	g := c.view("POST", "/team/groups", map[string]any{"members": []string{ids["Chief"], ids["Ada"]}}).Groups[0].ID
	if _, err := h.team.store.AppendLine(t.Context(), g, teamroster.GroupLine{From: teamroster.ByUser, Kind: teamroster.LinePost, Text: "Remember the launch"}); err != nil {
		t.Fatal(err)
	}
	entered, cancelled := make(chan struct{}), make(chan struct{})
	h.team.reviews.ask = func(ctx context.Context, _, _ string) (string, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return `{"changes":[{"op":"add","description":"late","body":"late result"}]}`, nil
	}
	h.team.reviews.work <- team.SeatRef{Agent: ids["Ada"], Group: g}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("review did not start")
	}
	h.team.reviews.close()
	select {
	case <-cancelled:
	default:
		t.Fatal("close did not join the active review")
	}
	facts, err := team.SharedStore(h.team.opts.Dir, team.TierGroup, g).List()
	if err != nil || len(facts) != 0 {
		t.Fatalf("stopped review wrote memory: %+v, %v", facts, err)
	}
}
