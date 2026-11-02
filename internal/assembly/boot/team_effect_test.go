package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

// teamScriptedProvider records every request and issues one scripted tool call
// per round before it answers in text.
type teamScriptedProvider struct {
	mu    sync.Mutex
	reqs  []provider.Request
	calls []provider.ToolCall
}

func (p *teamScriptedProvider) Name() string { return "boot-team-effect" }

func (p *teamScriptedProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	i := len(p.reqs)
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if i < len(p.calls) && p.calls[i].Name != "" {
		call := p.calls[i]
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &call}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *teamScriptedProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

type teamEffectPane struct {
	mu  sync.Mutex
	got []team.Delivery
}

func (p *teamEffectPane) Deliver(d team.Delivery) error {
	p.mu.Lock()
	p.got = append(p.got, d)
	p.mu.Unlock()
	return nil
}

func (*teamEffectPane) Cancel() {}

func (p *teamEffectPane) deliveries() []team.Delivery {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]team.Delivery(nil), p.got...)
}

type teamEffectPanes struct {
	mu   sync.Mutex
	open map[team.SeatRef]*teamEffectPane
}

func (f *teamEffectPanes) Ensure(_ context.Context, seat team.SeatRef) (team.Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.open == nil {
		f.open = map[team.SeatRef]*teamEffectPane{}
	}
	if f.open[seat] == nil {
		f.open[seat] = &teamEffectPane{}
	}
	return f.open[seat], nil
}

func (f *teamEffectPanes) Live(seat team.SeatRef) (team.Pane, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.open[seat]
	return p, ok
}

// teamEffectSession builds one session through the real assembly, seated when
// seat is set, and runs one prompt against p.
func teamEffectSession(t *testing.T, kind string, p *teamScriptedProvider, seat control.TeamSeat) []provider.Request {
	if seat != nil {
		requireTeamExecution(t)
	}
	t.Helper()
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	dir := robustTempDir(t)
	t.Chdir(dir)
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n[agent]\nsystem_prompt = \"BASE\"\n[[providers]]\nname = \"test-model\"\nkind = \""+kind+"\"\nmodel = \"x\"\n")
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Home: os.Getenv("REASONIX_HOME"), WorkspaceRoot: dir, resolvedShell: pinnedEffectShell(), TeamSeat: seat})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "turn-one"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return p.requests()
}

func requestTools(req provider.Request) []string {
	var names []string
	for _, tl := range req.Tools {
		names = append(names, tl.Name)
	}
	return names
}

func toolBlock(t *testing.T, req provider.Request) string {
	t.Helper()
	data, err := json.Marshal(req.Tools)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var teamToolNames = []string{"list_agents", "message_agent", "post_to_group", "pass_turn", "hand_turn", "read_group", "create_agent", "update_agent", "stop_agent", "create_group", "update_group", "delete_group"}

func TestEffectTeamToolsReachOnlyAnAgentsSessions(t *testing.T) {
	plain := teamEffectSession(t, "team-effect-plain", &teamScriptedProvider{}, nil)
	for _, name := range teamToolNames {
		if slices.Contains(requestTools(plain[0]), name) {
			t.Fatalf("an ordinary session offers %s", name)
		}
	}
	seated := teamEffectSession(t, "team-effect-seated", &teamScriptedProvider{}, &fakeTeamSeat{p: team.Projection{Identity: "You are Ada."}})
	for _, name := range teamToolNames {
		if !slices.Contains(requestTools(seated[0]), name) {
			t.Fatalf("an agent session lacks %s: %v", name, requestTools(seated[0]))
		}
	}
	if a, b := systemOf(plain[0]), systemOf(seated[0]); a != b {
		t.Fatalf("cache-boundary: seating a session moved its system prompt:\nfirst diff site: %q", firstDivergence(a, b))
	}
}

func TestEffectAnAgentsChatAndGroupSeatShareOnePrefix(t *testing.T) {
	own := teamEffectSession(t, "team-effect-own", &teamScriptedProvider{}, &fakeTeamSeat{p: team.Projection{Identity: "You are Ada.", Roster: "Ben."}})
	group := teamEffectSession(t, "team-effect-group", &teamScriptedProvider{}, &fakeTeamSeat{p: team.Projection{Identity: "You are Ada.", Roster: "Ben.", Group: "You are in Launch."}})
	if a, b := systemOf(own[0]), systemOf(group[0]); a != b {
		t.Fatalf("cache-boundary: the group seat's system prompt differs from the agent's chat:\nfirst diff site: %q", firstDivergence(a, b))
	}
	if a, b := toolBlock(t, own[0]), toolBlock(t, group[0]); a != b {
		t.Fatalf("cache-boundary: the group seat's tool block differs from the agent's chat:\nfirst diff site: %q", firstDivergence(a, b))
	}
}

func TestEffectMessageAgentReachesTheTeamThroughTheRealBuild(t *testing.T) {
	store := teamroster.Store{Dir: t.TempDir()}
	var ada, ben string
	if _, err := store.Update(context.Background(), func(r *teamroster.Roster) error {
		ada = r.AddAgent(teamroster.Agent{Name: "Ada", Main: true, CreatedBy: teamroster.ByUser}).ID
		ben = r.AddAgent(teamroster.Agent{Name: "Ben", CreatedBy: teamroster.ByUser}).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	panes := &teamEffectPanes{}
	svc := team.New(team.Options{Store: store, Panes: panes})
	p := &teamScriptedProvider{calls: []provider.ToolCall{{ID: "m1", Name: "message_agent", Arguments: `{"to":"Ben","message":"draft the launch post"}`}}}
	reqs := teamEffectSession(t, "team-effect-message", p, team.Seat{Service: svc, Ref: team.SeatRef{Agent: ada}})

	pane, ok := panes.Live(team.SeatRef{Agent: ben})
	if !ok || len(pane.(*teamEffectPane).deliveries()) != 1 {
		t.Fatalf("Ben's session got %+v", pane)
	}
	d := pane.(*teamEffectPane).deliveries()[0]
	if d.Kind != team.DeliverMessage || d.From != ada || !strings.Contains(d.Text, "draft the launch post") || d.Ceiling == "" {
		t.Fatalf("delivery = %+v", d)
	}
	if len(reqs) < 2 {
		t.Fatalf("the tool result never went back to the model: %d requests", len(reqs))
	}
	var result string
	for _, m := range reqs[1].Messages {
		if m.Role == provider.RoleTool {
			result = m.Content
		}
	}
	if !strings.Contains(result, "Delivered to Ben") {
		t.Fatalf("tool result = %q", result)
	}
	first := projectionOf(t, reqs[0], "turn-one")
	if !strings.Contains(blockOf(first, "agent-identity"), "You are Ada") || !strings.Contains(blockOf(first, "team-roster"), "Ben") {
		t.Fatalf("the real seat's projection did not reach the turn:\n%s", first)
	}
}
