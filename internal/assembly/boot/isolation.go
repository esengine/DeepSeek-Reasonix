package boot

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/writeclaim"
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

// isolatedResultTTL is when idle results become eligible for no-change cleanup.
const isolatedResultTTL = 14 * 24 * time.Hour

// isolationWiring is what a session offering worktree isolation holds.
type isolationWiring struct {
	store   *isolation.Store
	posture *isolationPosture
}

// addIsolation installs the selected task environment policy and result tools.
// Legacy runtimes retain the worktree_isolation opt-in and unattended boundary.
func (b *builder) addIsolation() error {
	var store *isolation.Store
	if b.session.mutations != nil && b.repo.Valid() {
		store = isolation.NewStore(filepath.Join(b.roots.DeliveryWorktreeDir(), "isolated"), b.root, b.repo).
			WithMutationCoordinator(b.session.mutations).
			WithEnvironmentCopies(b.cfg.Agent.IsolationCopyPaths, appendUniquePaths(RuntimeForbidReadRoots(b.cfg, b.root), b.opts.inheritedReadDenies...)...)
		b.tools.isolation = &isolationWiring{store: store, posture: &isolationPosture{}}
		b.tools.reg.Add(isolation.NewRecoverTool(b.repo).WithStore(store))
	}
	if b.tools.taskTool == nil || (b.session.mutations == nil && (!b.cfg.Agent.WorktreeIsolation || b.opts.UnattendedChild)) {
		return nil
	}
	if store == nil {
		store = isolation.NewStore(filepath.Join(b.roots.DeliveryWorktreeDir(), "isolated"), b.root, b.repo).
			WithMutationCoordinator(b.session.mutations).
			WithEnvironmentCopies(b.cfg.Agent.IsolationCopyPaths, appendUniquePaths(RuntimeForbidReadRoots(b.cfg, b.root), b.opts.inheritedReadDenies...)...)
	}
	go store.SweepExpired(context.WithoutCancel(b.ctx), isolatedResultTTL)
	posture := &isolationPosture{}
	b.tools.isolation = &isolationWiring{store: store, posture: posture}
	b.tools.reg.Add(isolation.NewApplyTool(store))
	b.tools.reg.Add(isolation.NewDiscardTool(store))
	parent, sink := b.opts, b.sink
	if b.session.mutations != nil {
		frozen, err := b.cfg.CloneResolved()
		if err != nil {
			return err
		}
		parent.frozenConfig = frozen
		parent.DelegationScheduler = b.tools.taskTool.Scheduler()
		parent.SessionDir = b.session.dir
		authority := func(ctx context.Context, grant writeclaim.WritePathSet) isolation.SourceAuthorizer {
			return b.isolationSourceAuthorizer(ctx, posture, grant)
		}
		mode, _ := b.cfg.CollaborationMode()
		b.tools.taskTool.WithEnvironmentFactory(mode, newIsolatedEnvironmentFactory(parent, store, posture, b.root, sink, authority, b.prompt.projectChecks...))
	} else {
		b.tools.taskTool.SetIsolation(store, b.root, func(ctx context.Context, run isolation.Run) (isolation.Outcome, error) {
			answer, unverified, err := runUnattended(ctx, parent, isolatedUsage{sink}, posture.mode(), run.WorkspaceRoot, run.Prompt, run.Model)
			return isolation.Outcome{Answer: answer, Unverified: unverified}, err
		})
	}
	// The registry canonicalizes a schema when a tool is added, and task was
	// added before it could offer isolation.
	b.tools.reg.Add(b.tools.taskTool)
	return nil
}

func (w *isolationWiring) bind(ctrl *control.Controller) {
	if w != nil {
		w.posture.bind(ctrl)
	}
}

func (b *builder) isolatedResultsOwner() control.IsolatedResultsOwner {
	if b.tools.isolation == nil {
		return nil
	}
	return b.tools.isolation.store
}

// isolatedUsage forwards spend and host hook diagnostics without replaying the
// candidate's model stream or tool transcript in the source conversation.
type isolatedUsage struct{ parent event.Sink }

func (s isolatedUsage) Emit(e event.Event) {
	if s.parent == nil {
		return
	}
	if e.Kind == event.Notice {
		switch e.Code {
		case event.NoticeCodeHookFailed, event.NoticeCodeHookBlocked, event.NoticeCodeHookWarned, event.NoticeCodeHookUnevaluable:
			e.Source = event.UsageSourceSubagent
			s.parent.Emit(e)
		}
		return
	}
	if e.Kind != event.Usage {
		return
	}
	e.UsageSource, e.Source = event.UsageSourceSubagent, event.UsageSourceSubagent
	s.parent.Emit(e)
}
