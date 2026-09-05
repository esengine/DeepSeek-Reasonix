package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// Who owns the typed-review obligation — the layer it first becomes true in, not
// the one that enforces it. A fact can be declared in one layer, projected by a
// second and checked by a third, and only the first of those owns it.

// A skill and a profile are the two objects a person edits. Neither has a field
// that could carry this, and a requirement no vocabulary can express is one no
// configuration can own — which leaves only the name.
func TestNoConfigurationLayerCanDeclareTheReviewObligation(t *testing.T) {
	for _, tc := range []struct {
		what string
		v    any
	}{
		{"skill.Skill", skill.Skill{}},
		{"agent.ProfileDefinition", agent.ProfileDefinition{}},
	} {
		if fields := obligationFields(tc.v); len(fields) > 0 {
			t.Errorf("%s declares %v; the obligation may have an owner after all", tc.what, fields)
		}
	}

	// What is left is the spelling. An ordinary worker that borrows the name
	// acquires the obligation and a reviewer that gives the name up loses it,
	// which is a requirement derived from a word rather than a declaration.
	if agent.ReviewReportKindForSkill("review") == "" {
		t.Fatal("the name carries nothing; the derivation is something else")
	}
	if agent.ReviewReportKindForSkill("code-review") != "" {
		t.Fatal("a renamed reviewer kept the obligation; something other than the name carries it")
	}
}

// obligationFields reports any field on a configuration type that could declare
// a delivery obligation. It reads the type rather than one value, so a field
// added later is found whether or not anyone sets it.
func obligationFields(v any) []string {
	var out []string
	for field := range reflect.TypeOf(v).Fields() {
		name := strings.ToLower(field.Name)
		if strings.Contains(name, "report") || strings.Contains(name, "verdict") ||
			strings.Contains(name, "obligation") || strings.Contains(name, "delivery") {
			out = append(out, field.Name)
		}
	}
	return out
}

// The same worker, compiled through both surfaces, read by what each child is
// actually given: a run that owes a typed verdict is handed the tool that
// produces one. The matrix is logged rather than asserted cell by cell — what it
// establishes is where the requirement enters, and a per-cell expectation would
// be asserting the answer it exists to find.
func TestTheReviewObligationEntersAtTheInvocationSurface(t *testing.T) {
	reviewer, ordinary := probeSkill(), probeSkill()
	reviewer.Name = "review"
	ordinary.Name = "helper"

	for _, tc := range []struct {
		cell string
		sk   skill.Skill
		via  string
	}{
		{"review name, via run_skill", reviewer, "skill"},
		{"review name, via task(profile=)", reviewer, "task"},
		{"ordinary name, via run_skill", ordinary, "skill"},
		{"ordinary name, via task(profile=)", ordinary, "task"},
		// The cross-wiring. With nothing but a name to carry the requirement,
		// these two decide it: semantics without the name, and the name without
		// the semantics.
		{"ordinary worker wearing the review name, via run_skill", named(ordinary, "review"), "skill"},
		{"ordinary worker wearing the review name, via task(profile=)", named(ordinary, "review"), "task"},
		{"the reviewer under another name, via run_skill", named(reviewer, "code-review"), "skill"},
	} {
		offered := reviewToolOffered(t, tc.sk, tc.via)
		t.Logf("%-58s skill=no profile=no delivery=%s", tc.cell, yn(offered))
	}
}

func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func named(sk skill.Skill, name string) skill.Skill {
	sk.Name = name
	return sk
}

// reviewToolOffered runs one worker through one surface and reports whether the
// child was handed review_report. That is the requirement made concrete: the
// gate reads a typed verdict, and a child with no way to produce one owes none.
func reviewToolOffered(t *testing.T, sk skill.Skill, via string) bool {
	t.Helper()
	w := newReviewWorld(t, sk)
	var err error
	if via == "skill" {
		_, err = w.skills.run(w.ctx, sk, "look", skill.SubagentRunOptions{})
	} else {
		args, _ := json.Marshal(map[string]any{"prompt": "look", "profile": sk.Name})
		_, err = w.tasks.Execute(w.ctx, json.RawMessage(args))
	}
	if err != nil {
		t.Fatalf("run %s via %s: %v", sk.Name, via, err)
	}
	return slices.Contains(w.provider.offered(), "review_report")
}

type reviewWorld struct {
	tasks    *agent.TaskTool
	skills   *skillSubagents
	provider *reviewProvider
	ctx      context.Context
}

func newReviewWorld(t *testing.T, profiles ...skill.Skill) *reviewWorld {
	t.Helper()
	root, sessions := testenv.TempDir(t), testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(matrixReadTool{})
	prov := &reviewProvider{}
	tasks := agent.NewTaskToolWithOptions(agent.TaskToolOptions{
		Provider: prov, ParentRegistry: reg, MaxSteps: 4, ContextWindow: 8192,
	}).WithTranscripts(agent.NewSubagentStore(filepath.Join(sessions, "subagents")), root, "base", "high").
		WithScheduler(agent.NewSubagentScheduler(2, 2)).
		WithProfileLookup(func(name string) (agent.ProfileDefinition, bool) {
			for _, sk := range profiles {
				if sk.Name == name {
					return agent.ProfileFromSkill(sk), true
				}
			}
			return agent.ProfileDefinition{}, false
		})
	ctx := agent.WithToolCallContext(context.Background(), "review-call", event.Discard, nil, false)
	ctx = agent.WithTurnIdentity(agent.WithParentSession(ctx, "probe"), "turn-1")
	return &reviewWorld{
		tasks:    tasks,
		skills:   &skillSubagents{root: root, cfg: config.Default(), registry: reg, tasks: tasks, maxSteps: 4},
		provider: prov, ctx: ctx,
	}
}

// reviewProvider records the tool names the child was offered, which is the
// compiled requirement made observable without exporting anything for a test.
type reviewProvider struct {
	mu    sync.Mutex
	tools []string
}

func (*reviewProvider) Name() string { return "review-probe" }

func (p *reviewProvider) offered() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.tools...)
}

func (p *reviewProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	if p.tools == nil {
		for _, tl := range req.Tools {
			p.tools = append(p.tools, tl.Name)
		}
	}
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "answered"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
