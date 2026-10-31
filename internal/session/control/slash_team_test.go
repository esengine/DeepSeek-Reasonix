package control

import (
	"context"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/teamroster"
)

// /team reads the team from any session, the terminal's included, and /msg
// leaves a message in the team's outbox for the window holding the team.
func TestTheTeamCommandsWorkFromAnySession(t *testing.T) {
	c, notices := settingsController(t)
	i18n.DetectLanguage("en")
	if c.managementNotice("/team"); !strings.Contains(lastNotice(t, notices()), "no agents yet") {
		t.Fatal("an empty team did not say so")
	}
	store := teamroster.Store{Dir: config.RootsForHome("").TeamDir()}
	var ada string
	if _, err := store.Update(context.Background(), func(r *teamroster.Roster) error {
		r.AddAgent(teamroster.Agent{Name: "Chief", Main: true, CreatedBy: teamroster.ByUser})
		ada = r.AddAgent(teamroster.Agent{Name: "Ada Lovelace", Role: "writer", CreatedBy: teamroster.ByUser}).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c.managementNotice("/team")
	listed := lastNotice(t, notices())
	for _, want := range []string{"Chief", "main", "Ada Lovelace", "writer", "/msg"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("/team lacks %q:\n%s", want, listed)
		}
	}

	c.managementNotice("/msg ada lovelace write the weekly report")
	if got := lastNotice(t, notices()); !strings.Contains(got, "sent to Ada Lovelace") {
		t.Fatalf("/msg = %q", got)
	}
	queued, err := store.PendingUserMessages(context.Background())
	if err != nil || len(queued) != 1 || queued[0].To != ada || queued[0].Text != "write the weekly report" {
		t.Fatalf("outbox = %+v, %v", queued, err)
	}
	c.managementNotice("/msg Nobody hello")
	if got := lastNotice(t, notices()); !strings.Contains(got, `"Nobody"`) {
		t.Fatalf("an unknown agent = %q", got)
	}
	c.managementNotice("/msg")
	if got := lastNotice(t, notices()); !strings.Contains(got, "usage") {
		t.Fatalf("no arguments = %q", got)
	}
}

func TestMsgExactIDWinsOverLongerAgentNameInEitherOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "target-first", true: "target-last"}[reverse], func(t *testing.T) {
			c, _ := settingsController(t)
			store := c.teamStore()
			var target string
			_, err := store.Update(t.Context(), func(r *teamroster.Roster) error {
				target = r.AddAgent(teamroster.Agent{Name: "Receiver", Main: true, CreatedBy: teamroster.ByUser}).ID
				r.AddAgent(teamroster.Agent{Name: target + " extra", CreatedBy: teamroster.ByUser})
				if reverse {
					slices.Reverse(r.Agents)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			c.queueUserMessage(target + " extra payload")
			messages, err := store.PendingUserMessages(t.Context())
			if err != nil || len(messages) != 1 || messages[0].To != target || messages[0].Text != "extra payload" {
				t.Fatalf("ID routing = %+v, %v", messages, err)
			}
		})
	}
}
