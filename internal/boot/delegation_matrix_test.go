package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/agentgraph"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/execgraph"
	"reasonix/internal/execjournal"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// The delegation matrix, frozen. A durable delegation is opened before it is
// visible, started before it may act, stored once it has, and reconstructable
// afterwards; an ephemeral run asks the same scheduler and claims nothing a
// snapshot cannot justify. The runtime-resume probe is the evidence — this
// holds the classification, so a new entry point cannot join the wrong class.
func TestDelegationClassesStayApart(t *testing.T) {
	for _, tc := range delegationMatrix() {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("asks the session scheduler, and stores nothing until it is admitted", func(t *testing.T) {
				w := newMatrixWorld(t)
				release, err := w.scheduler.Acquire(context.Background(), agent.AcquireRequest{})
				if err != nil {
					t.Fatalf("hold the only slot: %v", err)
				}
				defer release()

				held, cancel := context.WithTimeout(w.ctx, 400*time.Millisecond)
				defer cancel()
				_, _ = tc.call(held, w)

				if w.provider.entered() {
					t.Fatal("a child reached the provider while the session's only slot was held")
				}
				if children := w.children(t); len(children) != 0 {
					t.Fatalf("store holds %d child record(s) for work that was never admitted", len(children))
				}
				for _, e := range execjournal.History(w.sessionPath) {
					if e.Started() {
						t.Fatalf("execution %s was recorded as started while no slot was free", e.ID)
					}
				}
			})

			t.Run("leaves its class's records", func(t *testing.T) {
				w := newMatrixWorld(t)
				if _, err := tc.call(w.ctx, w); err != nil {
					t.Fatalf("run: %v", err)
				}
				if !w.provider.entered() {
					t.Fatal("no child reached the provider, so the row measures nothing")
				}
				entries := execjournal.History(w.sessionPath)
				children := w.children(t)
				rebuilt := rebuiltWorkers(entries, children)
				running := w.provider.storedWhileRunning()
				if !tc.durable {
					assertEphemeral(t, entries, children, rebuilt, w.drawn(), running)
					return
				}
				assertDurable(t, entries, children, rebuilt, w.drawn(), running)
			})
		})
	}
}

// assertDurable is the durable class's whole contract: an opening precedes the
// picture, a slot grant precedes the work, the store answers for what ran, and
// the two identities join so a rebuild can place it.
func assertDurable(t *testing.T, entries []execjournal.Entry, children []agent.SubagentArtifact, rebuilt, drawn, running []string) {
	t.Helper()
	if len(running) == 0 {
		t.Fatal("a child was executing and the store had never heard of it: nothing could be asked about work in flight")
	}
	if len(entries) == 0 {
		t.Fatal("nothing was opened: no reader after this process can say the work existed")
	}
	opened := map[string]bool{}
	for _, e := range entries {
		if !e.Started() {
			t.Fatalf("execution %s ran without a recorded slot grant", e.ID)
		}
		if e.SettledAt.IsZero() {
			t.Fatalf("execution %s was never let go of", e.ID)
		}
		opened[e.ID] = true
	}
	if len(drawn) == 0 {
		t.Fatal("nothing was drawn for work the authority can account for")
	}
	if len(children) == 0 {
		t.Fatal("the store answers for nothing that ran")
	}
	for _, c := range children {
		if !opened[c.Meta.ParentToolCallID] {
			t.Fatalf("child %s is stored under %q, which no opening names: nothing can join them",
				c.Ref, c.Meta.ParentToolCallID)
		}
	}
	if len(rebuilt) == 0 {
		t.Fatal("a rebuild over the durable facts draws nothing")
	}
}

// assertEphemeral is the other contract, and it is a promise of absence. The run
// really happened — the provider was entered — so each empty column here is a
// claim not made rather than a fact not reached.
func assertEphemeral(t *testing.T, entries []execjournal.Entry, children []agent.SubagentArtifact, rebuilt, drawn, running []string) {
	t.Helper()
	if len(running) != 0 {
		t.Fatalf("an ephemeral run was recorded as %v while executing; it promises no record at all", running)
	}
	if len(entries) != 0 {
		t.Fatalf("an ephemeral run opened %d execution(s); it promises none", len(entries))
	}
	if len(children) != 0 {
		t.Fatalf("an ephemeral run stored %d child record(s); it promises none", len(children))
	}
	if len(drawn) != 0 {
		t.Fatalf("an ephemeral run drew %v; no snapshot can produce those nodes", drawn)
	}
	if len(rebuilt) != 0 {
		t.Fatalf("a rebuild produced %v for a run that recorded nothing", rebuilt)
	}
}

type matrixRow struct {
	name    string
	durable bool
	call    func(ctx context.Context, w *matrixWorld) (string, error)
}

// delegationMatrix names every surface that spawns a child. run_skill appears
// as the model-invoked entry point only: a slash invocation has no
// provider-visible parent call, so it is not a delegation and this table makes
// no claim about it.
func delegationMatrix() []matrixRow {
	return []matrixRow{
		{name: "task", durable: true, call: func(ctx context.Context, w *matrixWorld) (string, error) {
			return w.tasks.Execute(ctx, json.RawMessage(`{"prompt":"look","description":"one"}`))
		}},
		{name: "fleet", durable: true, call: func(ctx context.Context, w *matrixWorld) (string, error) {
			return agent.NewFleetTool(w.tasks).Execute(ctx, json.RawMessage(
				`{"tasks":[{"prompt":"one","read_only":true},{"prompt":"two","read_only":true}]}`))
		}},
		{name: "parallel_tasks", durable: true, call: func(ctx context.Context, w *matrixWorld) (string, error) {
			return agent.NewParallelTasksTool(w.tasks, w.registry).Execute(ctx, json.RawMessage(
				`{"tasks":[{"prompt":"one"},{"prompt":"two"}]}`))
		}},
		{name: "run_skill (model-invoked)", durable: true, call: func(ctx context.Context, w *matrixWorld) (string, error) {
			return w.skills.run(ctx, probeSkill(), "look", skill.SubagentRunOptions{})
		}},
		{name: "read_only_task", durable: false, call: func(ctx context.Context, w *matrixWorld) (string, error) {
			return agent.NewReadOnlyTaskTool(w.tasks).Execute(ctx, json.RawMessage(`{"prompt":"look","description":"one"}`))
		}},
		{name: "read_only_skill", durable: false, call: func(ctx context.Context, w *matrixWorld) (string, error) {
			return w.skills.runReadOnly(ctx, probeSkill(), "look", skill.SubagentRunOptions{})
		}},
	}
}

// matrixWorld is one session with one slot: a real store, a real scheduler, and
// a sink that records what was drawn.
type matrixWorld struct {
	tasks       *agent.TaskTool
	skills      *skillSubagents
	registry    *tool.Registry
	scheduler   *agent.SubagentScheduler
	provider    *matrixProvider
	sink        *matrixSink
	ctx         context.Context
	sessions    string
	sessionPath string
}

func newMatrixWorld(t *testing.T) *matrixWorld {
	t.Helper()
	root, sessions := testenv.TempDir(t), testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(matrixReadTool{})
	prov := &matrixProvider{}
	sched := agent.NewSubagentScheduler(1, 1)
	tasks := agent.NewTaskToolWithOptions(agent.TaskToolOptions{
		Provider: prov, ParentRegistry: reg, MaxSteps: 6, ContextWindow: 8192,
	}).WithTranscripts(agent.NewSubagentStore(filepath.Join(sessions, "subagents")), root, "base", "high").
		WithScheduler(sched)
	prov.inspect = func() []string {
		var out []string
		facts, err := agent.ListSubagentsByParent(sessions, "probe")
		if err != nil {
			return []string{"error: " + err.Error()}
		}
		for _, f := range facts {
			out = append(out, f.Meta.ParentToolCallID+"="+string(f.Meta.Status))
		}
		return out
	}
	sink := &matrixSink{}
	ctx := agent.WithToolCallContext(context.Background(), "matrix-call", sink, nil, false)
	ctx = agent.WithTurnIdentity(agent.WithParentSession(ctx, "probe"), "turn-1")
	return &matrixWorld{
		tasks: tasks,
		skills: &skillSubagents{
			root: root, cfg: config.Default(), registry: reg, tasks: tasks, scheduler: sched,
			maxSteps: 6, maxDepth: 2, provider: prov, entry: &config.ProviderEntry{ContextWindow: 8192},
			identity: func(m, e string) (string, string) { return m, e },
			runOptions: func(_ context.Context, steps int, price *provider.Pricing, ctxWin, depth int) agent.Options {
				return agent.Options{MaxSteps: steps, Pricing: price, ContextWindow: ctxWin, SubagentDepth: depth, MaxSubagentDepth: 2}
			},
		},
		registry: reg, scheduler: sched, provider: prov, sink: sink, ctx: ctx,
		sessions: sessions, sessionPath: filepath.Join(sessions, "probe.jsonl"),
	}
}

func (w *matrixWorld) children(t *testing.T) []agent.SubagentArtifact {
	t.Helper()
	facts, err := agent.ListSubagentsByParent(w.sessions, "probe")
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	return facts
}

func (w *matrixWorld) drawn() []string { return w.sink.workers() }

func rebuiltWorkers(entries []execjournal.Entry, children []agent.SubagentArtifact) []string {
	outcomes := make([]execgraph.ChildOutcome, 0, len(children))
	for _, c := range children {
		outcomes = append(outcomes, execgraph.ChildOutcome{
			Execution: c.Meta.ParentToolCallID, Ref: c.Ref, Status: string(c.Meta.Status),
		})
	}
	var out []string
	for _, n := range execgraph.Rebuild(entries, outcomes, nil).Graph.Nodes {
		if n.Kind == agentgraph.KindWorker {
			out = append(out, n.ID)
		}
	}
	return out
}

// matrixSink records the worker nodes published under this call, which is what
// a reader would be shown while the run was happening.
type matrixSink struct {
	event.Sink
	graph agentgraph.Graph
}

func (s *matrixSink) Emit(e event.Event) {
	if e.Kind == event.GraphDelta && e.Graph != nil {
		s.graph.Apply(*e.Graph)
	}
}

func (s *matrixSink) workers() []string {
	var out []string
	for _, n := range s.graph.Nodes {
		if n.Kind == agentgraph.KindWorker && strings.HasPrefix(n.ID, "matrix-call") {
			out = append(out, n.ID)
		}
	}
	return out
}

// matrixReadTool is the one read-only tool a strict child needs to exist: a
// registry with none refuses the run before any class question is reached.
type matrixReadTool struct{}

func (matrixReadTool) Name() string            { return "matrix_read" }
func (matrixReadTool) Description() string     { return "Read something, for the matrix guard." }
func (matrixReadTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (matrixReadTool) ReadOnly() bool          { return true }
func (matrixReadTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "read", nil
}

type matrixProvider struct {
	mu sync.Mutex
	// atEntry is taken at the one instant nothing later reproduces: a child
	// inside the provider. Completion writes a record either way, so a check
	// made afterwards cannot tell a recorded execution from an unrecorded one.
	seen    bool
	atEntry []string
	inspect func() []string
}

func (*matrixProvider) Name() string { return "matrix" }

func (p *matrixProvider) entered() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen
}

// storedWhileRunning is what the store held the first time a child was inside
// the provider.
func (p *matrixProvider) storedWhileRunning() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.atEntry...)
}

func (p *matrixProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	first, inspect := !p.seen, p.inspect
	p.seen = true
	p.mu.Unlock()
	if first && inspect != nil {
		held := inspect()
		p.mu.Lock()
		p.atEntry = held
		p.mu.Unlock()
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "answered"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
