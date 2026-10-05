package boot

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
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
	store := isolation.NewStore(filepath.Join(config.DeliveryWorktreeDir(), "isolated"), b.root, b.repo)
	go store.SweepExpired(context.WithoutCancel(b.ctx), isolatedResultTTL)
	posture := &isolationPosture{}
	b.tools.isolation = &isolationWiring{store: store, posture: posture}
	b.tools.reg.Add(isolation.NewApplyTool(store))
	b.tools.reg.Add(isolation.NewDiscardTool(store))
	parent, sink := b.opts, b.sink
	b.tools.taskTool.SetIsolation(store, b.root, func(ctx context.Context, run isolation.Run) (isolation.Outcome, error) {
		answer, unverified, err := runUnattended(ctx, parent, isolatedUsage{sink}, posture.mode(), run.WorkspaceRoot, run.Prompt, run.Model)
		return isolation.Outcome{Answer: answer, Unverified: unverified}, err
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

// isolatedUsage forwards an isolated run's billable usage to the parent session
// as sub-agent spend and drops everything else it emits.
type isolatedUsage struct{ parent event.Sink }

func (s isolatedUsage) Emit(e event.Event) {
	if e.Kind != event.Usage || s.parent == nil {
		return
	}
	e.UsageSource, e.Source = event.UsageSourceSubagent, event.UsageSourceSubagent
	s.parent.Emit(e)
}
