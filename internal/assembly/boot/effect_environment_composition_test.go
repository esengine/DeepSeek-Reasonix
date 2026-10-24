package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/agentpreset"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

type fallbackCompositionProvider struct {
	parentCalls atomic.Int32
	child       chan provider.Request
}

func (*fallbackCompositionProvider) Name() string { return "fallback-composition" }

func (p *fallbackCompositionProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	child := false
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser && strings.Contains(msg.Content, "CHILD OBJECTIVE") {
			child = true
		}
	}
	ch := make(chan provider.Chunk, 2)
	if child {
		p.child <- req
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "child complete"}
	} else if p.parentCalls.Add(1) == 1 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "delegate", Name: "task", Arguments: `{"prompt":"CHILD OBJECTIVE","isolation":"auto","tools":["read_file"],"run_in_background":true}`}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "parent complete"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectAutomaticBackgroundFallbackKeepsComposition(t *testing.T) {
	p := &fallbackCompositionProvider{child: make(chan provider.Request, 4)}
	provider.Register("fallback-composition", func(provider.Config) (provider.Provider, error) { return p, nil })
	home, root := compositionTestWorkspace(t, "fallback-composition")
	writeFile(t, home, "config.toml", "[agent]\ncollaboration = 'auto'\n")
	d := agentcomposition.Builtins()[0]
	d.ID, d.Name, d.Persona.Prefix = "user:delegator", "Delegator", "FROZEN DELEGATOR PERSONA"
	d.Tools = agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{{ID: "task"}, {ID: "read_file"}}}
	empty := agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{}}
	d.Skills, d.MCP, d.Extensions, d.Hooks, d.Commands = empty, empty, empty, empty, empty
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-q", "-m", "init")
	b := newBuilder()
	built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, AgentComposition: d.ID, Sink: event.Discard, resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Controller.Close()
	built.Controller.EnsureSessionPath()
	built.Controller.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := built.Controller.RunTurn(t.Context(), "Delegate the review in the background"); err != nil {
		t.Fatal(err)
	}
	var req provider.Request
	select {
	case req = <-p.child:
	case <-time.After(10 * time.Second):
		history, _ := json.Marshal(built.Controller.History())
		t.Fatalf("background child never reached provider: %s", history)
	}
	if !strings.Contains(req.Messages[0].Content, d.Persona.Prefix) {
		t.Fatal("fallback lost frozen persona")
	}
	for _, def := range req.Tools {
		if def.Name == "bash" || def.Name == "write_file" {
			t.Fatalf("fallback widened tools: %s", def.Name)
		}
	}
	history, _ := json.Marshal(built.Controller.History())
	jobID := regexp.MustCompile(`task-[0-9]+`).FindString(string(history))
	if jobID == "" {
		t.Fatalf("missing job id: %s", history)
	}
	owner := sessionstore.BranchID(built.Controller.SessionPath())
	results, _ := b.session.jobs.WaitForSession(t.Context(), owner, []string{jobID}, jobs.WaitOptions{Timeout: 10 * time.Second})
	if len(results) != 1 || results[0].Status != jobs.Done {
		t.Fatalf("fallback job: %+v", results)
	}
	runs, err := b.tools.subagents.Runs(owner, b.root)
	if err != nil || len(runs) != 1 {
		t.Fatalf("fallback transcript: %+v %v", runs, err)
	}
	sess, err := sessionstore.LoadSession(filepath.Join(b.session.dir, "subagents", runs[0].Ref+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if binding := sess.Composition(); binding == nil || binding.Digest != built.Controller.CompositionBinding().Digest {
		t.Fatal("fallback transcript lost exact composition identity")
	}
}
