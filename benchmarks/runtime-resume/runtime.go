package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// armRoot names the four directories one arm owns. Home and Workspace are
// separate so a lever can change what the prefix folds without touching the
// transcript, and Sessions is explicit so the probe never writes to the
// developer's own session directory.
type armRoot struct{ Home, Workspace, Sessions, Dir string }

func rootFor(dir string) armRoot {
	return armRoot{
		Dir:       dir,
		Home:      filepath.Join(dir, "home"),
		Workspace: filepath.Join(dir, "workspace"),
		Sessions:  filepath.Join(dir, "sessions"),
	}
}

func (r armRoot) create() error {
	for _, d := range []string{r.Home, r.Workspace, r.Sessions} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// writeProjectConfig puts a project config beside the workspace, which is where
// a person configures these ceilings. The scheduler arms need them small enough
// to reach a specific refusal, and a poked field would prove nothing about the
// judgement production makes.
func (r armRoot) writeProjectConfig(total, writers int) error {
	body := fmt.Sprintf("[agent]\nmax_subagent_concurrency = %d\nmax_parallel_writers = %d\n", total, writers)
	return os.WriteFile(filepath.Join(r.Workspace, "reasonix.toml"), []byte(body), 0o644)
}

// requireFresh refuses a root a previous run left behind. A reused root is how
// a lever's effect ends up already present in the construct phase, which reads
// as "the lever changed nothing" — a false negative that looks like a result.
func (r armRoot) requireFresh() error {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("arm root %s is not empty; each run needs a clean root", r.Dir)
	}
	return nil
}

// graphSink records what the run graph published while this process was alive.
// It is the only "before" the graph row has: nothing else in a session's
// artifacts is known to carry it, and a row with no before is not measured.
type graphSink struct {
	mu     sync.Mutex
	graph  agentgraph.Graph
	deltas int
	// waits is every wait cause published per node, in order. The fold keeps
	// only the latest, which cannot say whether a cause was reported once or
	// replaced — and that is exactly what the transition arm asks.
	waits map[string][]string
	// phases is every subagent status the parent was told, per call. A run
	// handed to a job publishes no graph node at all, so for those this is the
	// only live evidence that anything happened.
	phases map[string][]string
	// tools is when each call was dispatched and when it answered. A stop that
	// reached a running tool is visible as the second arriving early.
	tools map[string][2]time.Time
}

func (s *graphSink) Emit(e event.Event) {
	if e.Kind == event.ToolDispatch || e.Kind == event.ToolResult {
		s.noteTool(e)
		return
	}
	if id, phase, ok := progressPhase(e); ok {
		s.mu.Lock()
		if s.phases == nil {
			s.phases = map[string][]string{}
		}
		s.phases[id] = append(s.phases[id], phase)
		s.mu.Unlock()
		return
	}
	if e.Kind != event.GraphDelta || e.Graph == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deltas++
	for _, n := range e.Graph.Nodes {
		if n.Wait == "" {
			continue
		}
		if s.waits == nil {
			s.waits = map[string][]string{}
		}
		s.waits[n.ID] = append(s.waits[n.ID], string(n.Wait))
	}
	s.graph.Apply(*e.Graph)
}

func (s *graphSink) snapshot() (agentgraph.Graph, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.graph, s.deltas
}

// noteTool records the two edges of a tool call this probe may be asked about.
func (s *graphSink) noteTool(e event.Event) {
	if e.Tool.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tools == nil {
		s.tools = map[string][2]time.Time{}
	}
	at := s.tools[e.Tool.ID]
	if e.Kind == event.ToolDispatch {
		at[0] = time.Now()
	} else {
		at[1] = time.Now()
	}
	s.tools[e.Tool.ID] = at
}

func (s *graphSink) toolDispatched(id string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.tools[id]
	return at[0], ok && !at[0].IsZero()
}

func (s *graphSink) toolResult(id string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.tools[id]
	return at[1], ok && !at[1].IsZero()
}

// phaseSeries is every status published for one call, in publication order.
func (s *graphSink) phaseSeries() map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]string{}
	for id, seq := range s.phases {
		out[id] = append([]string(nil), seq...)
	}
	return out
}

// waitSeries is every cause published for one node, in publication order.
func (s *graphSink) waitSeries() map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]string{}
	for id, seq := range s.waits {
		out[id] = append([]string(nil), seq...)
	}
	return out
}

// buildRuntime assembles a real runtime bound to this arm's roots. Everything a
// frontend would supply is supplied; only the provider is scripted, so the
// probe never depends on a network or a key.
func buildRuntime(ctx context.Context, root armRoot, arm string, sink event.Sink) (*control.Controller, *scripted, error) {
	prov := newScripted(arm)
	ctrl, err := boot.Build(ctx, boot.Options{
		Version:              "runtime-resume-probe",
		Model:                probeModelRef,
		Sink:                 sink,
		WorkspaceRoot:        root.Workspace,
		Home:                 root.Home,
		SessionDir:           root.Sessions,
		ProviderResolver:     resolverFor(prov),
		Stderr:               io.Discard,
		HeadlessApprovalMode: approvalMode(arm),
	})
	if err != nil {
		return nil, nil, err
	}
	ctrl.ApplyHeadlessApprovalMode(approvalMode(arm))
	return ctrl, prov, nil
}

// approvalMode is the gate this arm needs. Everything else runs denied, the
// strictest a headless frontend can be. A fleet is not read-only, so under a
// denied gate the dispatch is refused before any child starts and the arm
// measures a permission decision instead of a process boundary.
func approvalMode(arm string) string {
	if graphArm(arm) || uiArm(arm) || loneTaskArm(arm) || cancelRoutingArm(arm) || lineageArm(arm) ||
		schedulerWaitArm(arm) || terminalArm(arm) || deriveArm(arm) || activeStoreArm(arm) {
		return control.ToolApprovalAuto
	}
	return "deny"
}

// childEnv binds the child process to this arm's home through the environment
// as well as through Options.Home: a Controller's later re-reads and the shared
// history index still read the process, not the assembly.
func childEnv(root armRoot) []string {
	env := append(os.Environ(),
		"REASONIX_HOME="+root.Home,
		"REASONIX_STATE_HOME="+root.Home,
	)
	// A child runs in its own workspace, so the one arm that has to build the
	// frontend is told where the checkout is rather than searching for it.
	if repo := repoRoot(); repo != "" {
		env = append(env, envProbeRepo+"="+repo)
	}
	return env
}

// repoRoot walks up from the orchestrator's working directory for the module
// this probe lives in. Empty when it is not run from a checkout, which the arm
// reports as not measured rather than as a failure.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil &&
			strings.HasPrefix(string(b), "module reasonix") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
