package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync/atomic"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/event"
)

// delegationLifecycle is one lone delegation's record and its picture, kept as
// one thing. It decides no state of its own: the scheduler says what was queued
// and what was granted a slot, the run's ending says what it came to, and this
// writes each down before drawing it. A fan-out's items are excluded — their
// group opened them, and a second opening would redraw a worker as a group.
type delegationLifecycle struct {
	tool    *TaskTool
	sink    event.Sink
	track   *subagentProgressTracker
	id      string
	parent  string
	settled atomic.Bool
}

// openDelegation records that work entered orchestration and draws it pending.
// Called once every deterministic failure is behind it — a prompt that would
// not settle or a runtime that would not resolve is not work the orchestration
// ever held, and a journal that said otherwise would be inventing history.
func (t *TaskTool) openDelegation(ctx context.Context, spec *ProfileExecSpec, req *AcquireRequest, trk *subagentProgressTracker) (*delegationLifecycle, error) {
	parentID, sink, _, _ := CallContext(ctx)
	_, declared := graphNodeDeclared(ctx)
	// An ephemeral run opens nothing, picture included: a node no record
	// justifies leaves the screen at the next resync. A fan-out item's group
	// opened it already.
	if declared || spec.Context.Ephemeral {
		return nil, nil
	}
	// Descending from a call and having somewhere to draw are separate facts.
	// Work the host started descends from no call: it is its own root, with an
	// execution of its own. The picture goes only where a sink exists.
	group, id := parentID, parallelNodeID(parentID, 0)
	if spec.Context.TopLevel || parentID == "" {
		group, id = "", newHostExecutionID()
	}
	life := &delegationLifecycle{tool: t, sink: sink, track: trk, parent: group, id: id}
	worker := agentgraph.Node{
		ID: life.id, ParentID: group, Kind: agentgraph.KindWorker,
		// Pending, not running: nothing has asked the scheduler yet, and a
		// picture that says otherwise is wrong for as long as the wait lasts.
		State: agentgraph.StatePending, Label: delegationTaskLabel(*spec),
		Profile: spec.Worker.Profile, Model: spec.Worker.Model, Effort: spec.Worker.Effort,
		Grant: grantOf(spec.Grant.ReadOnly),
	}
	opening := agentgraph.Delta{Nodes: []agentgraph.Node{worker}}
	if group != "" {
		opening = fanOutOpeningDelta(group, delegationLabel(*spec), []agentgraph.Node{worker})
	}
	// The same fact, twice: the journal is written from the delta the graph is
	// drawn from, so the two cannot come to disagree about what was opened.
	if err := t.openExecutions(ctx, group, fanOutOpenings(opening)); err != nil {
		return nil, err
	}
	publishGraph(sink, opening)
	// The scheduler's two moments belong to this delegation now. The request
	// carries the refusal hook because that is where the scheduler reads it.
	spec.Sched.OnQueued, spec.Sched.OnStart = life.hooks(ctx)
	req.OnQueued = spec.Sched.OnQueued
	return life, nil
}

// settleOnReturn closes a delegation the caller still owns. A handoff has moved
// that ownership to the job, and settling here would record work as let go of
// while its child is still running.
func (d *delegationLifecycle) settleOnReturn(ctx context.Context, handedOff *bool, out *string, err *error) {
	if *handedOff {
		return
	}
	d.settle(ctx, *out, *err)
}

// executionID is what this run is recorded under. A lone delegation's child is
// stored against its own node, so the journal, the graph and the store's parent
// are one identity and a rebuild can join them; a fan-out item already runs
// under its node id and keeps it.
func (d *delegationLifecycle) executionID(callID string) string {
	if d == nil {
		return callID
	}
	return d.id
}

// hooks are the scheduler's two moments. Each writes the durable record before
// anything can observe the transition, and refuses rather than proceeding: a
// slot grant nothing recorded is a run a restart would call interrupted-before
// -start while it was executing.
func (d *delegationLifecycle) hooks(ctx context.Context) (func(agentgraph.WaitCause) error, func() error) {
	if d == nil {
		return nil, nil
	}
	return func(cause agentgraph.WaitCause) error {
		if err := d.tool.queueExecution(ctx, d.id, cause); err != nil {
			return err
		}
		publishGraph(d.sink, delegationQueuedDelta(d.id, cause))
		// The parent hears "queued" from the refusal and from nowhere else:
		// a status set because a job was registered says nothing about
		// whether the scheduler ever held this run back.
		d.track.queued()
		return nil
	}, func() error { return d.tool.startExecution(ctx, d.id) }
}

// beginExecution is the boundary a run crosses once it holds a slot and has a
// transcript to write into. Every delegated execution crosses it, whoever
// opened it: the store is what an execution is addressed by afterwards, and an
// entry point that skipped this left work that had really run absent from every
// surface the host can be asked about.
func (t *TaskTool) beginExecution(ctx context.Context, life *delegationLifecycle, ephemeral bool, trk *subagentProgressTracker, run *SubagentRun) error {
	// Recorded before drawn, so nothing shows a run as executing that the store
	// has no record of. A refusal stops the run before its child acts, leaving a
	// start on disk nothing used — honest residue, not a state to roll back.
	if !ephemeral && t != nil && t.transcripts != nil {
		if err := t.transcripts.MarkRunning(run); err != nil {
			return err
		}
	}
	if node, drawn := executionNode(ctx, life); drawn {
		publishGraph(node.sink, fanOutItemRunningDelta(node.id))
	}
	trk.running()
	return nil
}

// executionNode is the node this run is drawn as, from whichever side drew it: a
// fan-out declares its item's on the group's own sink, and a lone delegation's
// lifecycle holds its own. The boundary asks both rather than assuming one —
// neither is reachable from the other, because the sink a fan-out item runs
// under forwards tool events and drops graph deltas.
func executionNode(ctx context.Context, life *delegationLifecycle) (declaredGraphNode, bool) {
	if node, declared := graphNodeDeclared(ctx); declared {
		return node, true
	}
	if life == nil {
		return declaredGraphNode{}, false
	}
	return declaredGraphNode{sink: life.sink, id: life.id}, true
}

// settle closes the delegation once, whoever owns it by then. A foreground run
// is settled by the call that made it; a backgrounded one by the job it was
// handed to, which is the only owner that knows when the child stopped.
func (d *delegationLifecycle) settle(ctx context.Context, out string, err error) {
	if d == nil || d.settled.Swap(true) {
		return
	}
	state := delegationState(ctx, err)
	_, ref := splitSubagentRunResult(out)
	d.tool.settleExecution(ctx, d.id)
	publishGraph(d.sink, fanOutItemSettledDelta(d.id, state, ref, err))
	if d.parent != "" {
		publishGraph(d.sink, fanOutOutcomeDelta(d.parent, state, nil))
	}
}

// newHostExecutionID names an execution the host started. It is minted before
// the work is opened, because that is when the work becomes real: a store ref
// cannot stand in for it — the scheduler can be holding an execution back long
// before any transcript exists — and the synthetic id the host uses to nest a
// child's events is UI identity that must never reach a durable record.
func newHostExecutionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "host-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "host-" + hex.EncodeToString(b[:])
}

// delegationQueuedDelta reports a refusal the scheduler made: the state and the
// cause together, because a lone delegation is drawn pending until then and a
// cause on a pending node would name a wait nothing says it is in.
func delegationQueuedDelta(id string, cause agentgraph.WaitCause) agentgraph.Delta {
	return agentgraph.Delta{Nodes: []agentgraph.Node{
		{ID: id, State: agentgraph.StateQueued, QueuedAt: nowMilli(), Wait: cause},
	}}
}
