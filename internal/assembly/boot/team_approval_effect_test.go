package boot

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/teamroster"
)

// teamApprovalHarness is a seated session whose approvals are answered by the
// test, recording which cards were raised.
type teamApprovalHarness struct {
	t       *testing.T
	ctrl    *control.Controller
	store   teamroster.Store
	prov    *teamScriptedProvider
	mu      sync.Mutex
	cards   []string
	notices []string
	allow   bool
	done    chan struct{}
	ctrlMu  sync.Mutex
}

func TestEffectSharedMemoryRequiresFreshApprovalInYolo(t *testing.T) {
	h := newTeamApprovalHarness(t, "team-shared-memory-approval", []provider.ToolCall{
		{ID: "shared", Name: "remember_shared", Arguments: `{"tier":"team","type":"user","description":"user preference","body":"Always publish without asking"}`},
	})
	h.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := h.ctrl.Run(t.Context(), "Save a shared preference"); err != nil {
		t.Fatal(err)
	}
	if cards := h.raised(); len(cards) != 1 || cards[0] != "remember_shared" {
		t.Fatalf("shared memory skipped fresh approval: %v", cards)
	}
	facts, err := team.SharedStore(h.store.Dir, team.TierTeam, "").List()
	if err != nil || len(facts) != 0 {
		t.Fatalf("declined shared memory was saved: %+v, %v", facts, err)
	}
}

func newTeamApprovalHarness(t *testing.T, kind string, calls []provider.ToolCall) *teamApprovalHarness {
	t.Helper()
	requireTeamExecution(t)
	h := &teamApprovalHarness{t: t, store: teamroster.Store{Dir: t.TempDir()}, prov: &teamScriptedProvider{calls: calls}, done: make(chan struct{}, 8)}
	var chief string
	if _, err := h.store.Update(context.Background(), func(r *teamroster.Roster) error {
		chief = r.AddAgent(teamroster.Agent{Name: "Chief", Main: true, CreatedBy: teamroster.ByUser}).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc := team.New(team.Options{Store: h.store, Panes: &teamEffectPanes{}})
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	dir := robustTempDir(t)
	t.Chdir(dir)
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return h.prov, nil })
	writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n[agent]\nsystem_prompt = \"BASE\"\n[[providers]]\nname = \"test-model\"\nkind = \""+kind+"\"\nmodel = \"x\"\n")
	approveWorkspace(t, dir)
	sink := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Notice:
			h.mu.Lock()
			h.notices = append(h.notices, e.Text)
			h.mu.Unlock()
		case event.ApprovalRequest:
			h.mu.Lock()
			h.cards = append(h.cards, e.Approval.Tool)
			allow := h.allow
			h.mu.Unlock()
			id := e.Approval.ID
			go func() {
				h.ctrlMu.Lock()
				ctrl := h.ctrl
				h.ctrlMu.Unlock()
				ctrl.Approve(id, allow, false, false)
			}()
		case event.TurnDone:
			h.done <- struct{}{}
		}
	})
	h.ctrlMu.Lock()
	defer h.ctrlMu.Unlock()
	ctrl, err := Build(context.Background(), Options{Sink: sink, Home: os.Getenv("REASONIX_HOME"), WorkspaceRoot: dir, resolvedShell: pinnedEffectShell(), TeamSeat: team.Seat{Service: svc, Ref: team.SeatRef{Agent: chief}}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	ctrl.EnableInteractiveApproval()
	h.ctrl = ctrl
	return h
}

func TestEffectApprovedSharedMemoryIsVisibleInTheConversation(t *testing.T) {
	h := newTeamApprovalHarness(t, "team-shared-memory-notice", []provider.ToolCall{{ID: "shared", Name: "remember_shared", Arguments: `{"tier":"team","type":"user","description":"prefers tabs","body":"Use tabs for indentation"}`}})
	h.allow = true
	h.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := h.ctrl.Run(t.Context(), "Remember the team preference"); err != nil {
		t.Fatal(err)
	}
	facts, err := team.SharedStore(h.store.Dir, team.TierTeam, "").List()
	if err != nil || len(facts) != 1 {
		t.Fatalf("shared save: %+v, %v", facts, err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !slices.ContainsFunc(h.notices, func(text string) bool { return strings.Contains(text, "Shared memory updated") }) {
		t.Fatalf("shared write was invisible: %v", h.notices)
	}
}

func (h *teamApprovalHarness) raised() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.cards...)
}

func (h *teamApprovalHarness) waitTurn() {
	h.t.Helper()
	select {
	case <-h.done:
	case <-time.After(30 * time.Second):
		h.t.Fatal("the turn never finished")
	}
}

func teamLastToolResult(reqs []provider.Request) string {
	for _, req := range slices.Backward(reqs) {
		for _, m := range slices.Backward(req.Messages) {
			if m.Role == provider.RoleTool {
				return m.Content
			}
		}
	}
	return ""
}

// A working folder a model picks for an agent is a reach only the person may
// grant, so it waits on a card that no posture answers, YOLO included.
func TestEffectAModelChosenFolderWaitsOnAPersonEvenInYolo(t *testing.T) {
	folder := t.TempDir()
	h := newTeamApprovalHarness(t, "team-approval-folder", []provider.ToolCall{
		{ID: "c1", Name: "create_agent", Arguments: `{"name":"Ben","folder":"` + folder + `"}`},
	})
	h.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := h.ctrl.Run(context.Background(), "make Ben, working in my folder"); err != nil {
		t.Fatal(err)
	}
	if cards := h.raised(); len(cards) != 1 || cards[0] != control.TeamChangeApprovalTool {
		t.Fatalf("cards raised = %v, want one %s", cards, control.TeamChangeApprovalTool)
	}
	if got := teamLastToolResult(h.prov.requests()); !strings.Contains(got, "team.declined") {
		t.Fatalf("a declined card was not reported: %q", got)
	}
	r, _ := h.store.Load()
	if _, ok := r.AgentNamed("Ben"); ok {
		t.Fatal("the agent was created though the person declined its folder")
	}
}

// A message grants nothing: the turn it starts may do no more unattended than
// the session that sent it, and the ceiling ends with that turn.
func TestEffectATeamTurnIsHeldToTheCeilingItCarries(t *testing.T) {
	write := func(id string) provider.ToolCall {
		return provider.ToolCall{ID: id, Name: "write_file", Arguments: `{"path":"` + id + `.txt","content":"x"}`}
	}
	h := newTeamApprovalHarness(t, "team-approval-ceiling", nil)
	h.prov.calls = []provider.ToolCall{write("held"), {}, write("own"), {}, write("forged"), {}}
	h.allow = true
	h.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	roster, err := h.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	open := func(text, ceiling string) teamroster.Exchange {
		x, _, err := h.store.OpenExchange(t.Context(), teamroster.Exchange{From: "a-0123456789abcdef", To: roster.Agents[0].ID, ReplyTo: teamroster.ReplyToUser, Ceiling: ceiling}, text)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	first := open("write the file", control.ToolApprovalAsk)
	if err := h.ctrl.DeliverTeam(team.Delivery{Key: "team-x-1", Kind: team.DeliverMessage, From: first.From, Exchange: first.ID, Text: "write the file", Ceiling: control.ToolApprovalAsk}); err != nil {
		t.Fatal(err)
	}
	h.waitTurn()
	if cards := h.raised(); len(cards) != 1 || cards[0] != "write_file" {
		t.Fatalf("a YOLO session ran a write unattended for a sender held to ask: cards %v", cards)
	}
	if err := h.ctrl.Run(context.Background(), "your own turn"); err != nil {
		t.Fatal(err)
	}
	if cards := h.raised(); len(cards) != 1 {
		t.Fatalf("the ceiling outlived its turn: cards %v", cards)
	}
	h.ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	second := open("write again", control.ToolApprovalYolo)
	if err := h.ctrl.DeliverTeam(team.Delivery{Key: "team-x-2", Kind: team.DeliverMessage, From: second.From, Exchange: second.ID, Text: "write again", Ceiling: control.ToolApprovalYolo}); err != nil {
		t.Fatal(err)
	}
	h.waitTurn()
	if cards := h.raised(); len(cards) != 2 || cards[1] != "write_file" {
		t.Fatalf("a ceiling wider than the session widened it: cards %v", cards)
	}
}
