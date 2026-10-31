package serve

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/teamroster"
	"reasonix/internal/state/usagereport"
)

func TestTeamOpenDoesNotTrustAnArbitraryOrPreexistingFolder(t *testing.T) {
	for _, custom := range []bool{true, false} {
		t.Run(map[bool]string{true: "custom", false: "preexisting-default"}[custom], func(t *testing.T) {
			h, ids := teamHub(t, "Chief")
			root := filepath.Join(h.team.opts.Workspace, ids["Chief"])
			if custom {
				root = t.TempDir()
			} else if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if custom {
				_, err := h.team.store.Update(t.Context(), func(r *teamroster.Roster) error { r.Agents[0].Folder = root; return nil })
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := h.team.Ensure(t.Context(), team.SeatRef{Agent: ids["Chief"]})
			if !errors.Is(err, errTeamFolderUntrusted) {
				t.Fatalf("open = %v", err)
			}
			trust, err := config.NewProjectGrantStore(config.RootsForHome("").Home()).Trust(root)
			if err != nil || trust != config.WorkspaceTrustUndecided {
				t.Fatalf("trust written: %q, %v", trust, err)
			}
		})
	}
}

func TestTeamViewCacheSeesNewHistoryAndUsageAfterRewriteAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	h := &hubTeam{}
	s := sessionstore.NewSession("system")
	write := func(text string, count int) {
		s.Replace([]provider.Message{{Role: provider.RoleSystem, Content: "system"}, {Role: provider.RoleUser, Content: text}})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatal(err)
		}
		if err := usagereport.Save(path, usagereport.Report{Usage: usagereport.Usage{RequestCount: count}}); err != nil {
			t.Fatal(err)
		}
	}
	write("first", 1)
	if h.preview(path) != "first" {
		t.Fatal("initial preview missing")
	}
	write("other", 2)
	for _, reader := range []*hubTeam{h, {}} {
		if reader.preview(path) != "other" {
			t.Fatal("stale preview")
		}
		if got, ok := reader.usage(path); !ok || got.Usage.RequestCount != 2 {
			t.Fatalf("stale usage: %+v", got)
		}
	}
}

func TestTeamGroupCacheTracksAppendAndRoundChanges(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	g, err := h.team.service().UserCreateGroup(t.Context(), team.UserGroupChange{Members: []string{ids["Chief"], ids["Ada"]}})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second"} {
		if _, err := h.team.store.AppendLine(t.Context(), g.ID, teamroster.GroupLine{From: teamroster.ByUser, Kind: teamroster.LinePost, Text: text}); err != nil {
			t.Fatal(err)
		}
		if got := h.team.groupView(g).Preview; got != text {
			t.Fatalf("preview=%q want=%q", got, text)
		}
	}
	for _, relays := range []int{1, 2} {
		if err := h.team.store.SaveRound(t.Context(), g.ID, teamroster.RoundState{Relays: relays}); err != nil {
			t.Fatal(err)
		}
		if got := h.team.groupView(g).Round.Relays; got != relays {
			t.Fatalf("relays=%d want=%d", got, relays)
		}
	}
}

func TestNewHubSettlesPersistedDeliveredAndWaitingExchanges(t *testing.T) {
	if err := team.CheckPlatform(); err != nil {
		t.Skip(err)
	}
	writeServeModelConfig(t)
	closeSharedCatalogsOnCleanup(t)
	opts := TeamHostOptions{Dir: t.TempDir(), Workspace: t.TempDir()}
	store := teamroster.Store{Dir: opts.Dir}
	var ben string
	if _, err := store.Update(t.Context(), func(r *teamroster.Roster) error {
		ben = r.AddAgent(teamroster.Agent{Name: "Ben", Main: true, CreatedBy: teamroster.ByUser}).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(HubOptions{ProviderResolver: homeResolver(), Team: &opts})
	shutdown := sync.OnceFunc(h.Shutdown)
	t.Cleanup(shutdown)
	for _, state := range []teamroster.ExchangeState{teamroster.ExchangeDelivered, teamroster.ExchangeWaiting} {
		x, _, err := h.team.store.OpenExchange(t.Context(), teamroster.Exchange{From: teamroster.ByUser, To: ben, ReplyTo: teamroster.ReplyToUser}, "pending work")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.team.store.SetExchangeState(t.Context(), x.ID, state, x.At); err != nil {
			t.Fatal(err)
		}
	}
	shutdown()
	fresh := NewHub(HubOptions{ProviderResolver: homeResolver(), Team: &opts})
	t.Cleanup(fresh.Shutdown)
	book, err := fresh.team.store.Exchanges()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range book {
		if x.State != teamroster.ExchangeInterrupted {
			t.Fatalf("host restart left exchange open: %+v", x)
		}
	}
}
