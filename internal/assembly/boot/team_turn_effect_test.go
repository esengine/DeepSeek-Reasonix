package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/teamroster"
)

// ctrlPane is a seat backed by a real controller, as the hub's will be.
type ctrlPane struct{ ctrl *control.Controller }

func (p ctrlPane) Deliver(d team.Delivery) error { return p.ctrl.DeliverTeam(d) }
func (p ctrlPane) Cancel()                       { p.ctrl.Cancel() }

type mixedPanes struct {
	mu    sync.Mutex
	real  map[team.SeatRef]team.Pane
	stubs teamEffectPanes
}

func (m *mixedPanes) Ensure(ctx context.Context, seat team.SeatRef) (team.Pane, error) {
	m.mu.Lock()
	p, ok := m.real[seat]
	m.mu.Unlock()
	if ok {
		return p, nil
	}
	return m.stubs.Ensure(ctx, seat)
}

func (m *mixedPanes) Live(seat team.SeatRef) (team.Pane, bool) {
	m.mu.Lock()
	p, ok := m.real[seat]
	m.mu.Unlock()
	if ok {
		return p, true
	}
	return m.stubs.Live(seat)
}

// A message one agent sends another runs as a turn in the receiver's real
// session, and when that turn ends its answer comes back to the sender on its
// own, carried by nothing but the controller's settlement.
func TestEffectAnAgentsMessageIsAnsweredThroughTheReceiversRealTurn(t *testing.T) {
	requireTeamExecution(t)
	store := teamroster.Store{Dir: t.TempDir()}
	var ada, ben string
	if _, err := store.Update(context.Background(), func(r *teamroster.Roster) error {
		ada = r.AddAgent(teamroster.Agent{Name: "Ada", Main: true, CreatedBy: teamroster.ByUser}).ID
		ben = r.AddAgent(teamroster.Agent{Name: "Ben", CreatedBy: teamroster.ByUser}).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	panes := &mixedPanes{real: map[team.SeatRef]team.Pane{}}
	svc := team.New(team.Options{Store: store, Panes: panes})

	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &effectRecordingProvider{}
	provider.Register("team-turn-effect", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n[agent]\nsystem_prompt = \"BASE\"\n[[providers]]\nname = \"test-model\"\nkind = \"team-turn-effect\"\nmodel = \"x\"\n")
	approveWorkspace(t, dir)
	var seenMu sync.Mutex
	var started []eventwire.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnStarted {
			seenMu.Lock()
			started = append(started, eventwire.ToWire(e))
			seenMu.Unlock()
		}
	})
	ctrl, err := Build(context.Background(), Options{Home: os.Getenv("REASONIX_HOME"), WorkspaceRoot: dir, Sink: sink, resolvedShell: pinnedEffectShell(), TeamSeat: team.Seat{Service: svc, Ref: team.SeatRef{Agent: ben}}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.EnsureSessionPath()
	panes.mu.Lock()
	panes.real[team.SeatRef{Agent: ben}] = ctrlPane{ctrl}
	panes.mu.Unlock()

	receipt, err := svc.MessageAgent(context.Background(), team.Caller{Agent: ada, Ceiling: "ask"}, team.MessageRequest{To: "Ben", Message: "draft the launch post"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	var back []team.Delivery
	for {
		if p, ok := panes.stubs.Live(team.SeatRef{Agent: ada}); ok {
			back = p.(*teamEffectPane).deliveries()
		}
		if len(back) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(back) != 1 || back[0].Kind != team.DeliverReply || !strings.Contains(back[0].Text, `status="replied"`) || !strings.Contains(back[0].Text, "ok") {
		t.Fatalf("Ada got %+v", back)
	}
	book, _ := store.Exchanges()
	if len(book) != 1 || book[0].ID != receipt.Exchange || book[0].State != teamroster.ExchangeReplied {
		t.Fatalf("book = %+v", book)
	}

	reqs := rec.requests()
	if len(reqs) == 0 {
		t.Fatal("Ben's turn never reached the provider")
	}
	for _, r := range reqs {
		for _, m := range r.Messages {
			if m.Origin != nil {
				t.Fatalf("cache-boundary: a turn's origin reached the provider: %+v", m.Origin)
			}
		}
	}
	got := projectionOf(t, reqs[0], "draft the launch post")
	if !strings.Contains(got, `<agent-message from="Ada"`) && !strings.Contains(reqs[0].Messages[len(reqs[0].Messages)-1].Content, `<agent-message from="Ada"`) {
		t.Fatalf("the framed message did not reach Ben's model:\n%s", got)
	}
	var origin *provider.Origin
	landed := -1
	for i, m := range ctrl.History() {
		if m.Role == provider.RoleUser && m.Origin != nil {
			origin, landed = m.Origin, i
		}
	}
	if origin == nil || origin.Kind != string(team.DeliverMessage) || origin.Agent != ada || origin.Exchange != receipt.Exchange {
		t.Fatalf("the transcript does not say who the turn was for: %+v", origin)
	}

	// The live stream says the same thing the transcript does, at the index the
	// message landed on, so a window watching draws the row a reload would.
	seenMu.Lock()
	defer seenMu.Unlock()
	var live *eventwire.Event
	for i := range started {
		if started[i].Origin != nil {
			live = &started[i]
		}
	}
	if live == nil || live.Origin.Agent != ada || live.Origin.Exchange != receipt.Exchange || live.Origin.Kind != string(team.DeliverMessage) {
		t.Fatalf("no turn_started carried the turn's origin: %+v", started)
	}
	read := sessionstore.UserMessageText(ctrl.History()[landed])
	if !strings.Contains(read, `<agent-message from="Ada"`) || !strings.Contains(read, "draft the launch post") {
		t.Fatalf("history reads the turn as %q, not the message it was handed", read)
	}
	if live.MsgIndex == nil || *live.MsgIndex != landed || live.Text != read {
		t.Fatalf("turn_started names index %v and text %q; history reads %q at %d", live.MsgIndex, live.Text, read, landed)
	}
}

func TestEffectTerminalOutboxIsHostInputWithoutUserAuthority(t *testing.T) {
	requireTeamExecution(t)
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &effectRecordingProvider{}
	provider.Register("team-user-effect", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n[agent]\nsystem_prompt = \"BASE\"\n[[providers]]\nname = \"test-model\"\nkind = \"team-user-effect\"\nmodel = \"x\"\n")
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Home: os.Getenv("REASONIX_HOME"), WorkspaceRoot: dir, resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.EnsureSessionPath()
	if err := ctrl.DeliverTeam(team.Delivery{Key: "user-u-1", Kind: team.DeliverUserMessage, From: teamroster.ByUser, Display: "write the weekly report", Text: "write the weekly report"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(rec.requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	for !time.Now().After(deadline) && ctrl.Running() {
		time.Sleep(20 * time.Millisecond)
	}
	var landed *provider.Message
	for _, m := range ctrl.History() {
		if m.Role == provider.RoleUser && strings.Contains(sessionstore.UserMessageText(m), "write the weekly report") {
			landed = &m
		}
	}
	if landed == nil || !landed.HostAuthored || landed.Origin == nil || landed.Origin.Agent != "terminal" || !strings.Contains(sessionstore.UserMessageText(*landed), "author is not authenticated") {
		t.Fatalf("terminal input claimed user authority: %+v", landed)
	}
}
