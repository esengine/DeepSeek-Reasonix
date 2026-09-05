package boot

// The durable skill switch as a live registry input: what it decides for the
// session that flipped it, and where it deliberately stops — a run already
// dispatched keeps the semantics it was dispatched with.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestActivationDecidesInvocationInTheSameSession(t *testing.T) {
	h := newProjectionHarness(t, "activation-invocation", "", "")
	if _, ok := h.ctrl.RunSkill("/explore look around"); !ok {
		t.Fatal("the skill this arm toggles cannot be invoked to begin with")
	}

	if err := h.ctrl.SetSkillEnabled("explore", config.ActivationProject, false); err != nil {
		t.Fatalf("SetSkillEnabled(off): %v", err)
	}
	if _, ok := h.ctrl.RunSkill("/explore look around"); ok {
		t.Error("a disabled skill is still invocable in the session that disabled it")
	}

	if err := h.ctrl.SetSkillEnabled("explore", config.ActivationProject, true); err != nil {
		t.Fatalf("SetSkillEnabled(on): %v", err)
	}
	if _, ok := h.ctrl.RunSkill("/explore look around"); !ok {
		t.Error("re-enabling did not make the skill invocable again")
	}
}

// inflightSwitchProvider flips the switch while the child it dispatched is
// mid-run: the parent asks for a skill, and the answer to the child's own
// request arrives only after that skill has been turned off.
type inflightSwitchProvider struct {
	mu    sync.Mutex
	calls int
	ctrl  *control.Controller
	flip  error
}

func (p *inflightSwitchProvider) Name() string { return "activation-inflight" }

func (p *inflightSwitchProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	ctrl := p.ctrl
	p.mu.Unlock()

	var chunks []provider.Chunk
	switch call {
	case 0:
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "run-1", Name: "run_skill", Arguments: `{"name":"explore","arguments":"look around"}`,
		}}}
	case 1:
		if ctrl != nil {
			err := ctrl.SetSkillEnabled("explore", config.ActivationProject, false)
			p.mu.Lock()
			p.flip = err
			p.mu.Unlock()
		}
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "child answer"}, {Type: provider.ChunkDone}}
	case 2:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "parent done"}, {Type: provider.ChunkDone}}
	default:
		chunks = []provider.Chunk{{Type: provider.ChunkError, Err: fmt.Errorf("unexpected provider call %d", call)}}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

// A switch flipped while a child runs must not reach that child. Resolution
// happens at dispatch and the run holds what it resolved; a registry consulted
// again mid-run would cancel work the user already asked for.
func TestActivationLeavesAnAlreadyDispatchedRunAlone(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	prov := &inflightSwitchProvider{}
	provider.Register("activation-inflight", func(provider.Config) (provider.Provider, error) { return prov, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "activation-inflight"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	prov.mu.Lock()
	prov.ctrl = ctrl
	prov.mu.Unlock()

	if err := ctrl.Run(context.Background(), "delegate and finish"); err != nil {
		t.Fatalf("the turn failed after the switch moved mid-run: %v", err)
	}

	prov.mu.Lock()
	calls, flipErr := prov.calls, prov.flip
	prov.mu.Unlock()
	if flipErr != nil {
		t.Fatalf("the arm never flipped the switch, so it measured nothing: %v", flipErr)
	}
	if calls < 3 {
		t.Fatalf("the run stopped after %d provider calls; the dispatched child did not finish and hand back", calls)
	}
	// Without this the arm passes on a run that never dispatched anything: the
	// flip would still happen, the calls would still reach three, and the child
	// whose survival is the whole subject would never have existed.
	if !toolResultMentions(ctrl.History(), "child answer") {
		t.Fatalf("no dispatched child handed an answer back, so nothing here was in flight:\n%s", historyRoles(ctrl.History()))
	}
	if last := lastAssistantText(ctrl.History()); !strings.Contains(last, "parent done") {
		t.Errorf("the parent did not finish its turn after the child returned: %q", last)
	}

	// The same switch decides the next invocation, which is the half that makes
	// the first half a boundary rather than the switch doing nothing.
	if _, ok := ctrl.RunSkill("/explore look again"); ok {
		t.Error("the next invocation after the flip still resolved a disabled skill")
	}
}

func toolResultMentions(msgs []provider.Message, want string) bool {
	for _, m := range msgs {
		if m.Role == provider.RoleTool && strings.Contains(m.Content, want) {
			return true
		}
	}
	return false
}

func historyRoles(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		content := m.Content
		if len(content) > 60 {
			content = content[:60] + "..."
		}
		fmt.Fprintf(&b, "%s: %q\n", m.Role, content)
	}
	return b.String()
}

func lastAssistantText(msgs []provider.Message) string {
	var last string
	for _, m := range msgs {
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			last = m.Content
		}
	}
	return last
}
