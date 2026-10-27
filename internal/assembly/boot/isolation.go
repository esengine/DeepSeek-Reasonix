package boot

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
)

// isolationPosture is the session's approval posture, read each time an
// isolated task starts. The child runs under exactly that posture: it has no
// one to ask, so a session that asks gets a child that refuses, and nothing the
// child may do exceeds what the session itself would do unattended.
type isolationPosture struct {
	ctrl atomic.Pointer[control.Controller]
}

func (p *isolationPosture) bind(ctrl *control.Controller) {
	if p != nil {
		p.ctrl.Store(ctrl)
	}
}

func (p *isolationPosture) mode() string {
	if ctrl := p.ctrl.Load(); ctrl != nil {
		return ctrl.ToolApprovalMode()
	}
	return control.ToolApprovalAsk
}

// isolatedResultTTL is how long a result nobody applied or discarded is kept.
const isolatedResultTTL = 14 * 24 * time.Hour

// isolationWiring is what a session offering worktree isolation holds.
type isolationWiring struct {
	store   *isolation.Store
	posture *isolationPosture
}

// addIsolation offers task's worktree isolation, and the tools that settle its
// results, when [agent] worktree_isolation is on. An unattended child never
// offers it, so isolated runs cannot fan out again.
func (b *builder) addIsolation() {
	if !b.cfg.Agent.WorktreeIsolation || b.opts.UnattendedChild || b.tools.taskTool == nil {
		return
	}
	store := isolation.NewStore(filepath.Join(b.roots.DeliveryWorktreeDir(), "isolated"), b.root, b.repo)
	go store.SweepExpired(context.WithoutCancel(b.ctx), isolatedResultTTL)
	posture := &isolationPosture{}
	b.tools.isolation = &isolationWiring{store: store, posture: posture}
	b.tools.reg.Add(isolation.NewApplyTool(store))
	b.tools.reg.Add(isolation.NewDiscardTool(store))
	parent := b.opts
	b.tools.taskTool.SetIsolation(store, b.root, func(ctx context.Context, run isolation.Run, spec delegation.ProfileExecSpec) (isolation.Outcome, error) {
		_, selection, err := subagentProviderSelection(b.cfg, b.providers.effective, b.model.entry, spec.Worker.Model, spec.Worker.Effort)
		if err != nil {
			return isolation.Outcome{}, err
		}
		dir, err := os.MkdirTemp("", "reasonix-isolated-")
		if err != nil {
			return isolation.Outcome{}, err
		}
		defer os.RemoveAll(dir)
		child := &builder{timer: newPhaseTimer(), compositionBinding: newLiveCompositionBinding}
		res, err := child.build(ctx, Options{
			ComputerDriver: parent.ComputerDriver, WorkspaceRoot: run.WorkspaceRoot, Home: parent.Home,
			Model: selection.Ref, EffortOverride: selection.Effort, MaxSteps: spec.Sched.MaxSteps,
			AgentPreset: parent.AgentPreset, ProviderResolver: b.providers.effective, Sink: event.Discard, Stderr: io.Discard,
			SessionDir: dir, HeadlessApprovalMode: posture.mode(), ApprovalTimeout: time.Second,
			GoalTurnsUnreachable: true, UnattendedChild: true,
		})
		if err != nil {
			return isolation.Outcome{}, err
		}
		defer res.Controller.Close()
		if child.tools.taskTool == nil {
			return isolation.Outcome{}, fmt.Errorf("isolated delegation is unavailable")
		}
		child.tools.taskTool.InheritDelegationOwner(b.tools.taskTool)
		answer, err := child.tools.taskTool.RunProfileSpec(ctx, spec)
		return isolation.Outcome{Answer: answer}, err
	})
	// The registry canonicalizes a schema when a tool is added, and task was
	// added before it could offer isolation.
	b.tools.reg.Add(b.tools.taskTool)
}

func (w *isolationWiring) bind(ctrl *control.Controller) {
	if w != nil {
		w.posture.bind(ctrl)
	}
}
