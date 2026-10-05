package acp

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type skillRawFactory struct {
	provider *testutil.MockProvider
	ctrl     chan *control.Controller
	dir      string
}

func (f *skillRawFactory) SessionDir() string { return f.dir }

func (f *skillRawFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	executor := agent.New(f.provider, tool.NewRegistry(), sessionstore.NewSession("test agent"),
		agent.Options{MaxSteps: 2}, p.Sink)
	ctrl := control.New(control.Options{
		Runner: executor, Executor: executor, Sink: p.Sink,
		SessionDir: f.dir,
		Skills: []skill.Skill{
			{Name: "probe", Description: "probe skill", Body: "PROBE BODY\nthen /other", Triggers: []string{"probe", "tidy"}, AutoUse: "require"},
			{Name: "other", Description: "other skill", Body: "OTHER BODY", Triggers: []string{"other"}, AutoUse: "require"},
		},
	})
	f.ctrl <- ctrl
	return ctrl, nil
}

func TestACPSkillPromptKeepsTypedTextAsRawInput(t *testing.T) {
	factory := &skillRawFactory{
		provider: testutil.NewMock("test", testutil.Turn{Text: "done"}),
		ctrl:     make(chan *control.Controller, 1),
		dir:      t.TempDir(),
	}
	client, stop := startServer(t, factory)
	defer stop()
	sid := openSession(t, client)
	ctrl := <-factory.ctrl

	const typed = "/probe tidy the notes"
	prompt := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: sid,
		Prompt:    []ContentBlock{{Type: "text", Text: typed}},
	})
	_, resp := drainPrompt(t, client, prompt)
	if resp.Error != nil {
		t.Fatalf("session/prompt: %v", resp.Error)
	}

	req := factory.provider.LastRequest()
	if req == nil {
		t.Fatal("no provider request")
	}
	var delivered string
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser {
			delivered = msg.Content
		}
	}
	if !strings.Contains(delivered, "PROBE BODY") || !strings.Contains(delivered, "Arguments: tidy the notes") {
		t.Fatalf("provider did not receive the rendered skill invocation: %q", delivered)
	}
	if strings.Contains(delivered, "<capability-route") {
		t.Fatalf("already-pinned skill was routed again: %q", delivered)
	}
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleUser {
			if msg.RawContent != typed {
				t.Fatalf("raw user input = %q, want %q", msg.RawContent, typed)
			}
			return
		}
	}
	t.Fatal("no user turn in history")
}
