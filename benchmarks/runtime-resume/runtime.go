package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

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
}

func (s *graphSink) Emit(e event.Event) {
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
	if graphArm(arm) || schedulerWaitArm(arm) || terminalArm(arm) {
		return control.ToolApprovalAuto
	}
	return "deny"
}

// childEnv binds the child process to this arm's home through the environment
// as well as through Options.Home: a Controller's later re-reads and the shared
// history index still read the process, not the assembly.
func childEnv(root armRoot) []string {
	return append(os.Environ(),
		"REASONIX_HOME="+root.Home,
		"REASONIX_STATE_HOME="+root.Home,
	)
}
