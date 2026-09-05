package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// What a worker owes when it finishes is declared by whoever defined it,
// projected into the execution spec, and enforced by delivery. The three are
// separate layers and this holds the first one to being the only source: the
// requirement was once a switch on the worker's name, which meant an ordinary
// worker borrowing the name acquired it and a renamed reviewer lost it.
func TestDeliveryObligationFollowsTheDeclarationAndNotTheName(t *testing.T) {
	reviewer := probeSkill()
	reviewer.Name = "code-review-under-another-name"
	reviewer.Delivery = skill.DeliveryContract{ReviewReport: skill.ReviewReportReview}

	impostor := probeSkill()
	impostor.Name = "review"

	for _, tc := range []struct {
		cell string
		sk   skill.Skill
		via  string
		owes bool
	}{
		// The declaration travels with the worker, through either surface.
		{"a declared reviewer, renamed, via run_skill", reviewer, "skill", true},
		{"a declared reviewer, renamed, via task(profile=)", reviewer, "task", true},
		// The name confers nothing, through either surface.
		{"an ordinary worker named review, via run_skill", impostor, "skill", false},
		{"an ordinary worker named review, via task(profile=)", impostor, "task", false},
	} {
		t.Run(tc.cell, func(t *testing.T) {
			if got := reviewToolOffered(t, tc.sk, tc.via); got != tc.owes {
				t.Fatalf("owes a typed verdict = %v, want %v", got, tc.owes)
			}
		})
	}
}

// The built-in reviewers declare their own contract. Read from the store the
// way a session reads it, so a definition that lost its declaration fails here
// rather than silently stopping a verdict from being required.
func TestBuiltInReviewersDeclareTheirVerdict(t *testing.T) {
	store := skill.New(skill.Options{})
	for name, want := range map[string]string{
		"review":          skill.ReviewReportReview,
		"security-review": skill.ReviewReportSecurity,
		"explore":         "",
		"research":        "",
	} {
		sk, ok := store.Read(name)
		if !ok {
			t.Fatalf("built-in %q is missing", name)
		}
		if got := sk.Delivery.ReviewReport; got != want {
			t.Errorf("built-in %q declares %q, want %q", name, got, want)
		}
		if got := agent.ProfileFromSkill(sk).Delivery.ReviewReport; got != evidence.ReviewKind(want) {
			t.Errorf("built-in %q projects %q, want %q", name, got, want)
		}
		// What it may prove is declared beside what it owes, and neither is
		// read off the other: a reviewer authorized for its own obligation and
		// for nothing else is the whole point of the pair.
		authority := agent.ProfileFromSkill(sk).Authority.Review
		for _, kind := range evidence.ReviewKinds() {
			if got, expect := authority.Proves(kind), want != "" && evidence.ReviewKind(want) == kind; got != expect {
				t.Errorf("built-in %q proves %q = %v, want %v", name, kind, got, expect)
			}
		}
	}
}

// The grant has to survive the real assembly, not only the projection: a
// built-in reviewer running through the shipped runner is mounted a report tool
// bound to its own obligation, so the kind it submits is checked against what
// the host assigned rather than taken as the answer.
func TestMountedReportToolRefusesAnUnassignedKindThroughTheRealRunner(t *testing.T) {
	store := skill.New(skill.Options{})
	reviewer, ok := store.Read("review")
	if !ok {
		t.Fatal("built-in review is missing")
	}
	reviewer.AllowedTools = nil

	prov := &authorityProbeProvider{}
	w := newReviewWorldWith(t, prov, reviewer)
	args, _ := json.Marshal(map[string]any{"prompt": "review it", "profile": reviewer.Name})
	if _, err := w.tasks.Execute(w.ctx, json.RawMessage(args)); err != nil {
		t.Fatalf("run review: %v", err)
	}
	transcript := prov.transcript()
	if !strings.Contains(transcript, "a run cannot choose which review it counts as") {
		t.Fatalf("a review worker's security submission was not refused by the mounted tool; transcript = %q", transcript)
	}
	if strings.Contains(transcript, "review_report accepted") {
		t.Fatal("a review worker must not have a security report accepted")
	}
}

// authorityProbeProvider makes the child submit a report for an obligation it
// was never granted, then hands back the transcript the refusal landed in.
type authorityProbeProvider struct {
	mu    sync.Mutex
	turn  int
	seen  []provider.Message
	tools []string
}

func (*authorityProbeProvider) Name() string { return "authority-probe" }

func (p *authorityProbeProvider) transcript() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b strings.Builder
	for _, m := range p.seen {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func (p *authorityProbeProvider) offered() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.tools...)
}

func (p *authorityProbeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.turn++
	turn := p.turn
	p.seen = append([]provider.Message(nil), req.Messages...)
	if p.tools == nil {
		for _, tl := range req.Tools {
			p.tools = append(p.tools, tl.Name)
		}
	}
	p.mu.Unlock()

	ch := make(chan provider.Chunk, 2)
	if turn == 1 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "r1", Name: "review_report",
			Arguments: `{"kind":"security","verdict":"pass","reviewed_paths":["a.go"]}`}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "verdict: pass"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// reviewToolOffered runs one worker through one surface and reports whether the
// child was handed review_report. That is the requirement made concrete: the
// gate reads a typed verdict, and a child with no way to produce one owes none.
// One field drives both the tool and the option, so the two cannot disagree.
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
	provider reviewProbeProvider
	ctx      context.Context
}

// reviewProbeProvider is a provider that also reports the tools it was offered.
type reviewProbeProvider interface {
	provider.Provider
	offered() []string
}

func newReviewWorld(t *testing.T, profiles ...skill.Skill) *reviewWorld {
	t.Helper()
	return newReviewWorldWith(t, &reviewProvider{}, profiles...)
}

func newReviewWorldWith(t *testing.T, prov reviewProbeProvider, profiles ...skill.Skill) *reviewWorld {
	t.Helper()
	root, sessions := testenv.TempDir(t), testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(matrixReadTool{})
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
		tasks: tasks,
		skills: &skillSubagents{
			root: root, cfg: config.Default(), registry: reg, tasks: tasks, maxSteps: 4,
			maxDepth: 3, provider: prov, entry: &config.ProviderEntry{},
			scheduler: agent.NewSubagentScheduler(2, 2),
			identity:  func(m, e string) (string, string) { return m, e },
			runOptions: func(_ context.Context, steps int, price *provider.Pricing, ctxWin, depth int) agent.Options {
				return agent.Options{MaxSteps: steps, Pricing: price, ContextWindow: ctxWin, SubagentDepth: depth}
			},
		},
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
