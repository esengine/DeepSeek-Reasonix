package serve

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/agentpreset"
	"reasonix/internal/state/sessionstore"
)

type restartCompositionProvider struct {
	mu       sync.Mutex
	requests []provider.Request
}

func (*restartCompositionProvider) Name() string { return "composition-restart" }
func (p *restartCompositionProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "Task complete."}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestCustomCompositionSurvivesHubRestart(t *testing.T) {
	for _, scope := range []string{"user", "project"} {
		t.Run(scope, func(t *testing.T) {
			for _, selection := range []string{"new", "picker"} {
				for _, reopen := range []string{"open", "reuse-blank"} {
					t.Run(selection+"/"+reopen, func(t *testing.T) {
						testCustomCompositionRestart(t, scope, selection, reopen)
					})
				}
			}
		})
	}
}

func testCustomCompositionRestart(t *testing.T, scope, selection, reopen string) {
	t.Helper()
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Chdir(root)
	closeSharedCatalogsOnCleanup(t)
	d := agentcomposition.Builtins()[0]
	d.ID, d.Name = scope+":restart", "Restart reviewer"
	d.Persona.Prefix = "You are the restart regression reviewer."
	empty := agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{}}
	d.Tools, d.Skills, d.MCP, d.Extensions, d.Hooks, d.Commands = empty, empty, empty, empty, empty, empty
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
	p := &restartCompositionProvider{}
	resolver := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "neutral/fixture", Model: "fixture", Default: true}},
		Providers:   map[string]provider.Provider{"neutral/fixture": p},
	}
	h := NewHub(HubOptions{ProviderResolver: resolver})
	t.Cleanup(h.Shutdown)
	req := OpenRequest{Root: root}
	if selection == "new" {
		req.AgentComposition = d.ID
	}
	rt, err := h.Open(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if selection == "picker" {
		if err := rt.Server.Controller().SelectAgentComposition(t.Context(), d.ID); err != nil {
			t.Fatal(err)
		}
	}
	ctrl := rt.Server.Controller()
	want := ctrl.CompositionBinding()
	if want == nil || want.Version.Definition.ID != d.ID {
		t.Fatal("new runtime lost binding")
	}
	if err := ctrl.RunTurn(t.Context(), "Say hello."); err != nil {
		t.Fatal(err)
	}
	path := ctrl.SessionPath()
	if got := ctrl.CompositionBinding(); got == nil || got.Digest != want.Digest {
		t.Fatal("turn lost binding")
	}
	if err := rt.Server.switchModel(t.Context(), "neutral/fixture"); err != nil {
		t.Fatal(err)
	}
	if got := rt.Server.Controller().CompositionBinding(); got == nil || got.Digest != want.Digest {
		t.Fatal("model rebuild lost binding")
	}
	h.Shutdown()
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Composition(); got == nil || got.Digest != want.Digest || !got.Started {
		t.Fatalf("disk lost binding: %+v", got)
	}
	// A saved conversation keeps its version even when the preset is edited.
	d.Name, d.Persona.Prefix = "Edited reviewer", "A different persona."
	if _, err := agentpreset.New(home, root).Save(d, want.Version.Revision); err != nil {
		t.Fatal(err)
	}
	restarted := NewHub(HubOptions{ProviderResolver: resolver})
	t.Cleanup(restarted.Shutdown)
	req = OpenRequest{Root: root}
	if reopen == "open" {
		req.SessionPath = path
	}
	opened, err := restarted.Open(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if reopen == "reuse-blank" {
		if _, err := opened.Server.resumeInto(path); err != nil {
			t.Fatal(err)
		}
	}
	if got := opened.Server.Controller().CompositionBinding(); got == nil || got.Digest != want.Digest {
		t.Fatalf("reopened runtime lost binding: %+v", got)
	}
	savedFile, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	resumedFile, err := os.Stat(opened.Server.Controller().SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(savedFile, resumedFile) {
		t.Fatal("restart opened a replacement conversation instead of the saved file")
	}
	srv := httptest.NewServer(operatorHandler(restarted))
	defer srv.Close()
	view := hubGet[*compositionSummary](t, srv, "/rt/"+opened.ID+"/agent-composition")
	status := hubGet[struct {
		AgentComposition *compositionSummary `json:"agentComposition"`
	}](t, srv, "/rt/"+opened.ID+"/status")
	for _, got := range []*compositionSummary{view, status.AgentComposition} {
		if got == nil || got.ID != want.Version.Definition.ID || got.Name != want.Version.Definition.Name || got.Digest != want.Digest || !got.Started {
			t.Fatalf("frontend projection lost saved persona: %+v", got)
		}
	}
	if err := opened.Server.Controller().RunTurn(t.Context(), "Say hello again."); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) != 2 {
		t.Fatalf("provider requests = %d, want two", len(p.requests))
	}
	before, after := p.requests[0], p.requests[1]
	if before.Messages[0].Content != after.Messages[0].Content || !strings.Contains(after.Messages[0].Content, want.Resolved.Persona.Prefix) {
		t.Fatal("restart changed provider-visible persona")
	}
	for _, request := range p.requests {
		for _, message := range request.Messages {
			if message.AgentComposition != nil {
				t.Fatal("local binding leaked to provider")
			}
		}
	}
	beforeTools, _ := json.Marshal(before.Tools)
	afterTools, _ := json.Marshal(after.Tools)
	if string(beforeTools) != string(afterTools) {
		t.Fatal("restart changed tool schema bytes")
	}
}
