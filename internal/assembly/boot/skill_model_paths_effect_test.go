package boot

import (
	"context"
	"fmt"
	"maps"
	"reasonix/internal/session/control"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
)

type scriptedCall struct{ name, args string }

// scriptedCallProvider issues one tool call per round, then finishes, and keeps
// every tool result the model was shown, keyed by call id.
type scriptedCallProvider struct {
	mu    sync.Mutex
	calls []scriptedCall
	round int
	reqs  []provider.Request
}

func (p *scriptedCallProvider) Name() string { return "boot-skill-paths" }

func (p *scriptedCallProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	i := p.round
	p.round++
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if i < len(p.calls) {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("path-%d", i), Name: p.calls[i].name, Arguments: p.calls[i].args,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *scriptedCallProvider) resultOf(i int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	last := p.reqs[len(p.reqs)-1]
	id := fmt.Sprintf("path-%d", i)
	for _, m := range last.Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == id {
			return m.Content
		}
	}
	return ""
}

const (
	userOnlySubagent = "---\nname: ship-sub\ndescription: delivers as a worker\nrunAs: subagent\ndisable-model-invocation: true\n---\nSUB BODY\n"
	routedUserOnly   = "---\nname: ship-routed\ndescription: routed deploy\ntriggers: deploy, ship\nauto-use: require\ndisable-model-invocation: true\n---\nROUTED BODY\n"
)

func runScriptedSkillTurn(t *testing.T, kind string, calls []scriptedCall, prompt string) (*scriptedCallProvider, *projectionEventLog) {
	t.Helper()
	return runScriptedSkillTurnWith(t, kind, calls, prompt, map[string]string{"ship-it": userOnlySkill}, nil)
}

func runScriptedSkillTurnWith(t *testing.T, kind string, calls []scriptedCall, prompt string, skills map[string]string, afterBuild func(dir string, ctrl *control.Controller)) (*scriptedCallProvider, *projectionEventLog) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	p := &scriptedCallProvider{calls: calls}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)
	base := map[string]string{"ship-sub": userOnlySubagent, "ship-routed": routedUserOnly, "plain-one": plainSkill}
	maps.Copy(base, skills)
	for name, body := range base {
		writeFile(t, dir, ".reasonix/skills/"+name+"/SKILL.md", body)
	}
	log := &projectionEventLog{}
	ctrl, err := Build(context.Background(), Options{Sink: log})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if afterBuild != nil {
		afterBuild(dir, ctrl)
	}
	if err := ctrl.Run(context.Background(), prompt); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return p, log
}

// Every way the model can start a user-only skill, through the real assembly.
// Each row is one path in the PR's table; a refusal must name the boundary and
// must never carry the skill body.
func TestEffectModelCannotReachUserOnlySkillByAnyPath(t *testing.T) {
	calls := []scriptedCall{
		{"run_skill", `{"name":"ship-it"}`},
		{"read_skill", `{"name":"ship-it"}`},
		{"read_only_skill", `{"name":"ship-it"}`},
		{"use_capability", `{"action":"call","capability_id":"skill:ship-it"}`},
		{"use_capability", `{"action":"inspect","capability_id":"skill:ship-it"}`},
		{"use_capability", `{"action":"search","query":"deploys to production ship-it"}`},
		{"use_capability", `{"action":"list"}`},
		{"slash_command", `{"command":"ship-it"}`},
		{"slash_command", `{"command":"list"}`},
		{"task", `{"prompt":"do it","profile":"ship-sub"}`},
		{"run_skill", `{"name":"ship-sub","arguments":"go"}`},
	}
	p, _ := runScriptedSkillTurn(t, "skillpaths-effect", calls, "check")
	refused := map[int]bool{0: true, 1: true, 2: true, 3: true, 9: true, 10: true}
	for i, c := range calls {
		res := p.resultOf(i)
		if res == "" {
			t.Fatalf("call %d (%s %s) produced no tool result", i, c.name, c.args)
		}
		for _, body := range []string{"SHIP BODY", "SUB BODY", "ROUTED BODY"} {
			if strings.Contains(res, body) {
				t.Errorf("call %d (%s %s) leaked a user-only body:\n%s", i, c.name, c.args, res)
			}
		}
		if refused[i] && !strings.Contains(res, "reserved for the user") {
			t.Errorf("call %d (%s %s) was not refused with the typed boundary error:\n%s", i, c.name, c.args, res)
		}
	}
	if res := p.resultOf(4); !strings.Contains(res, "unknown") {
		t.Errorf("inspect of a user-only skill must read as unknown:\n%s", res)
	}
	for _, i := range []int{5, 6} {
		res := p.resultOf(i)
		for _, id := range []string{"skill:ship-it", "skill:ship-sub", "skill:ship-routed"} {
			if strings.Contains(res, id) {
				t.Errorf("call %d (%s) surfaced %s in the capability catalog", i, calls[i].args, id)
			}
		}
	}
	if res := p.resultOf(7); !strings.Contains(res, "no slash command") {
		t.Errorf("slash_command ship-it was not treated as unknown:\n%s", res)
	}
	list := p.resultOf(8)
	if strings.Contains(list, "ship-") || !strings.Contains(list, "plain-one") {
		t.Errorf("slash_command list must hold the ordinary skill and no user-only one:\n%s", list)
	}
}

// The per-turn capability router nudges the model toward skills whose triggers
// match; a user-only skill must not be nudged, whatever auto-use says.
func TestEffectCapabilityRouteNeverNamesUserOnlySkill(t *testing.T) {
	p, _ := runScriptedSkillTurn(t, "skillpaths-route", nil, "please deploy and ship this")
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.reqs {
		for _, m := range req.Messages {
			if m.Role == provider.RoleUser && (strings.Contains(m.Content, "ship-routed") || strings.Contains(m.Content, "<capability-route")) {
				t.Fatalf("the router or listing named a user-only skill:\n%s", m.Content)
			}
		}
	}
}

// slash_command entries are built once at boot; flipping the declaration on disk
// mid-session must still stop the model from reading the body through them.
func TestEffectSlashCommandFollowsAFlagFlippedMidSession(t *testing.T) {
	calls := []scriptedCall{{"slash_command", `{"command":"ship-it"}`}, {"slash_command", `{"command":"list"}`}}
	open := strings.Replace(userOnlySkill, "disable-model-invocation: true\n", "", 1)
	p, _ := runScriptedSkillTurnWith(t, "skillpaths-slashflip", calls, "check",
		map[string]string{"ship-it": open},
		func(dir string, _ *control.Controller) {
			writeFile(t, dir, ".reasonix/skills/ship-it/SKILL.md", userOnlySkill)
		})
	if res := p.resultOf(0); strings.Contains(res, "SHIP BODY") || !strings.Contains(res, "reserved for the user") {
		t.Fatalf("slash_command rendered a skill reserved for the user after the flip:\n%s", res)
	}
	if res := p.resultOf(1); strings.Contains(res, "ship-it") {
		t.Fatalf("slash_command list still names it after the flip:\n%s", res)
	}
}

// /reload-cmd rebuilds the slash_command tool; its entries must follow a flag
// flipped after the reload just as the boot-time ones do.
func TestEffectReloadedSlashCommandFollowsAFlagFlippedMidSession(t *testing.T) {
	calls := []scriptedCall{{"slash_command", `{"command":"ship-it"}`}}
	open := strings.Replace(userOnlySkill, "disable-model-invocation: true\n", "", 1)
	p, _ := runScriptedSkillTurnWith(t, "skillpaths-reloadflip", calls, "check",
		map[string]string{"ship-it": open},
		func(dir string, ctrl *control.Controller) {
			if err := ctrl.ReloadCommands(context.Background()); err != nil {
				t.Fatal(err)
			}
			writeFile(t, dir, ".reasonix/skills/ship-it/SKILL.md", userOnlySkill)
		})
	if res := p.resultOf(0); strings.Contains(res, "SHIP BODY") || !strings.Contains(res, "reserved for the user") {
		t.Fatalf("the reloaded slash_command rendered a skill reserved for the user:\n%s", res)
	}
}
