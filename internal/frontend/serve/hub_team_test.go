package serve

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/teamroster"
	"reasonix/internal/state/workspacelist"
)

// teamHub is a hub with the team on, over a roster of the named agents; the
// first is a main agent.
func teamHub(t *testing.T, names ...string) (*Hub, map[string]string) {
	t.Helper()
	if err := team.CheckPlatform(); err != nil {
		t.Skip(err)
	}
	writeServeModelConfig(t)
	closeSharedCatalogsOnCleanup(t)
	opts := TeamHostOptions{Dir: testenv.TempDir(t), Workspace: testenv.TempDir(t)}
	ids := map[string]string{}
	if _, err := (teamroster.Store{Dir: opts.Dir}).Update(context.Background(), func(r *teamroster.Roster) error {
		for i, n := range names {
			ids[n] = r.AddAgent(teamroster.Agent{Name: n, Main: i == 0, CreatedBy: teamroster.ByUser}).ID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(HubOptions{ProviderResolver: homeResolver(), Team: &opts})
	t.Cleanup(h.Shutdown)
	return h, ids
}

func listed(h *Hub, id string) bool {
	return slices.ContainsFunc(h.List(), func(v RuntimeView) bool { return v.ID == id })
}

func ensureSeat(t *testing.T, h *Hub, seat team.SeatRef) *Runtime {
	t.Helper()
	pane, err := h.team.Ensure(context.Background(), seat)
	if err != nil {
		t.Fatalf("Ensure(%+v): %v", seat, err)
	}
	return pane.(runtimePane).rt
}

func TestASeatOpensBehindTheWindowUntilTheWindowOpensIt(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	seat := team.SeatRef{Agent: ids["Ada"]}
	rt := ensureSeat(t, h, seat)
	if listed(h, rt.ID) {
		t.Fatal("a background seat is listed as a pane")
	}
	if again := ensureSeat(t, h, seat); again.ID != rt.ID {
		t.Fatalf("a second Ensure opened %s beside %s", again.ID, rt.ID)
	}
	h.team.holdForWindow(rt.ID)
	if !listed(h, rt.ID) {
		t.Fatal("a seat the window opened is not listed")
	}
	if err := h.Close(rt.ID); err != nil {
		t.Fatal(err)
	}
	if listed(h, rt.ID) || h.Get(rt.ID) == nil {
		t.Fatal("closing the pane did not put the seat back behind the window")
	}
	if err := h.closeRuntimeNow(rt.ID); err != nil || h.Get(rt.ID) != nil {
		t.Fatalf("closing the seat itself: %v", err)
	}
	if _, ok := h.team.Live(seat); ok {
		t.Fatal("a closed seat is still live")
	}
}

func TestASeatsConversationIsRecordedAndResumed(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	seat := team.SeatRef{Agent: ids["Ada"]}
	rt := ensureSeat(t, h, seat)
	path := rt.Server.Controller().SessionPath()
	r, _ := h.team.store.Load()
	ada, _ := r.Agent(ids["Ada"])
	if path == "" || ada.SessionPath != path {
		t.Fatalf("session path %q, roster %q", path, ada.SessionPath)
	}
	if !strings.HasPrefix(rt.Root, h.team.opts.Workspace) {
		t.Fatalf("an agent with no folder works in %s, not its own folder", rt.Root)
	}
	if err := h.closeRuntimeNow(rt.ID); err != nil {
		t.Fatal(err)
	}
	saved := sessionstore.NewSession("system")
	saved.Add(provider.Message{Role: provider.RoleUser, Content: "plan the launch"})
	saved.Add(provider.Message{Role: provider.RoleAssistant, Content: "a plan"})
	if err := saved.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	again := ensureSeat(t, h, seat)
	if sessionstore.CanonicalSessionPath(again.Server.Controller().SessionPath()) != sessionstore.CanonicalSessionPath(path) {
		t.Fatalf("the seat reopened %s, not its conversation %s", again.Server.Controller().SessionPath(), path)
	}
	if !slices.ContainsFunc(again.Server.Controller().History(), func(m provider.Message) bool { return m.Content == "plan the launch" }) {
		t.Fatal("the resumed seat lost its conversation")
	}
}

// A seat closed before its first turn has a conversation path and no
// transcript yet; opening it again starts that conversation rather than
// failing to resume it.
func TestASeatClosedBeforeItsFirstTurnOpensAgain(t *testing.T) {
	h, ids := teamHub(t, "Chief")
	seat := team.SeatRef{Agent: ids["Chief"]}
	rt := ensureSeat(t, h, seat)
	path := rt.Server.Controller().SessionPath()
	if err := h.closeRuntimeNow(rt.ID); err != nil {
		t.Fatal(err)
	}
	again := ensureSeat(t, h, seat)
	if sessionstore.CanonicalSessionPath(again.Server.Controller().SessionPath()) != sessionstore.CanonicalSessionPath(path) {
		t.Fatalf("the seat reopened at %s, not its recorded conversation %s", again.Server.Controller().SessionPath(), path)
	}
}

func TestATeamFolderIsNotAddedToTheUsersProjects(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	rt := ensureSeat(t, h, team.SeatRef{Agent: ids["Ada"]})
	list, _ := workspacelist.Read(filepath.Join(config.RootsForHome("").Home(), workspacelist.FileName))
	if slices.Contains(list.Paths, rt.Root) {
		t.Fatalf("the agent's folder %s became a project", rt.Root)
	}
}

func TestAnAgentKeepsItsOwnApprovalMode(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	if _, err := h.team.store.Update(context.Background(), func(r *teamroster.Roster) error {
		a, _ := r.Agent(ids["Ada"])
		a.Permission = control.ToolApprovalAuto
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.stance.set(control.ToolApprovalAsk)
	rt := ensureSeat(t, h, team.SeatRef{Agent: ids["Ada"]})
	if got := rt.Server.Controller().ToolApprovalMode(); got != control.ToolApprovalAuto {
		t.Fatalf("the agent opened in %s, not its own auto", got)
	}
	rt.Server.applyApprovalMode(control.ToolApprovalReadOnly)
	r, _ := h.team.store.Load()
	ada, _ := r.Agent(ids["Ada"])
	if ada.Permission != control.ToolApprovalReadOnly || h.stance.get() != control.ToolApprovalAsk {
		t.Fatalf("an agent pane's switch went to the window: roster %q, window %q", ada.Permission, h.stance.get())
	}
}

func TestIdleBackgroundSeatsCloseAndTheLiveCountIsBounded(t *testing.T) {
	names := []string{"Chief"}
	for i := range teamLiveCap + 1 {
		names = append(names, "A"+string(rune('a'+i)))
	}
	h, ids := teamHub(t, names...)
	var opened []string
	for _, n := range names[1:] {
		opened = append(opened, ensureSeat(t, h, team.SeatRef{Agent: ids[n]}).ID)
	}
	if n := len(h.team.background(func(*teamHold) bool { return true })); n != teamLiveCap {
		t.Fatalf("%d background seats open, want %d", n, teamLiveCap)
	}
	if h.Get(opened[0]) != nil {
		t.Fatal("the least recently used seat stayed open past the cap")
	}
	h.team.closeIdle(time.Now().Add(teamIdleClose + time.Minute))
	if n := len(h.team.background(func(*teamHold) bool { return true })); n != 0 {
		t.Fatalf("%d idle seats stayed open past the idle limit", n)
	}
}

func TestASecondProcessSeesTheTeamReadOnly(t *testing.T) {
	writeServeModelConfig(t)
	closeSharedCatalogsOnCleanup(t)
	opts := TeamHostOptions{Dir: testenv.TempDir(t), Workspace: testenv.TempDir(t)}
	release, err := (teamroster.Store{Dir: opts.Dir}).AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	h := NewHub(HubOptions{ProviderResolver: homeResolver(), Team: &opts})
	defer h.Shutdown()
	if h.team.service() != nil || h.team.seatFor(OpenRequest{team: &teamOpen{}}) != nil {
		t.Fatal("a hub without the writer lease runs the team")
	}
	if _, err := h.team.Ensure(context.Background(), team.SeatRef{Agent: "a-0123456789abcdef"}); !errors.Is(err, teamroster.ErrUnknownAgent) && err == nil {
		t.Fatal("a read-only hub opened a seat for an unknown agent")
	}
}
