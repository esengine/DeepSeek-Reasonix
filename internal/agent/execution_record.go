package agent

import (
	"context"
	"fmt"

	"reasonix/internal/agentgraph"
	"reasonix/internal/event"
	"reasonix/internal/execjournal"
)

type turnIdentityContextKey struct{}

// WithTurnIdentity carries the parent turn's durable identity through the tool
// calls it makes. It is the in-flight marker's id rather than a transcript
// position: the transcript has no position for a turn that has not ended, which
// is exactly when a delegation is opened.
func WithTurnIdentity(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, turnIdentityContextKey{}, id)
}

// TurnIdentity returns the parent turn identity carried by a turn context.
func TurnIdentity(ctx context.Context) string {
	id, _ := ctx.Value(turnIdentityContextKey{}).(string)
	return id
}

// executionSessionPath is the transcript a delegation belongs to, resolved the
// way the sub-agent store resolves it. Empty when this run has no persisted
// parent — a headless run keeps nothing, so there is nowhere to record to.
func (t *TaskTool) executionSessionPath(ctx context.Context) string {
	if t == nil || t.transcripts == nil {
		return ""
	}
	path, ok := t.transcripts.parentSessionPath(ParentSession(ctx))
	if !ok {
		return ""
	}
	return path
}

// openExecutions records a fan-out's items before any of them may run. It fails
// the dispatch rather than logging: the journal is the only thing that would
// survive to say the work was opened, so an item it could not record must not
// start.
func (t *TaskTool) openExecutions(ctx context.Context, group string, openings []execjournal.Opening) error {
	path := t.executionSessionPath(ctx)
	if path == "" {
		return nil
	}
	turn := TurnIdentity(ctx)
	for _, opening := range openings {
		opening.Group, opening.Turn = group, turn
		if err := execjournal.Open(path, opening); err != nil {
			return fmt.Errorf("record delegated execution %s: %w", opening.ID, err)
		}
	}
	return nil
}

// itemHooks are the scheduler's two moments with the durable record placed
// ahead of the second. A granted slot is the point the child becomes able to
// act, so STARTED reaches disk before anything observes it running and before
// the run does anything at all.
func (t *TaskTool) itemHooks(ctx context.Context, sink event.Sink, id string) (func(agentgraph.WaitCause), func() error) {
	onWait, onSlot := fanOutItemHooks(sink, id)
	return onWait, func() error {
		if err := t.startExecution(ctx, id); err != nil {
			return err
		}
		return onSlot()
	}
}

// startExecution records the slot grant. An item that never reaches one — a
// branch its dependency cut — is never started, which is the difference a
// restart reads to tell what was executing from what merely existed.
func (t *TaskTool) startExecution(ctx context.Context, id string) error {
	path := t.executionSessionPath(ctx)
	if path == "" {
		return nil
	}
	if err := execjournal.Start(path, id); err != nil {
		return fmt.Errorf("record delegated execution start %s: %w", id, err)
	}
	return nil
}

// settleExecution records that the orchestration has let go of one item. What
// the item produced is not restated here: the sub-agent store already owns that
// answer, and a second copy is a second authority to reconcile.
func (t *TaskTool) settleExecution(ctx context.Context, id string) {
	if path := t.executionSessionPath(ctx); path != "" {
		execjournal.Settle(path, id)
	}
}

// adoptedDisposition marks an item that stands in for running. Nothing executes
// under it, so no owner can go missing and it is closed the moment it opens.
func adoptedDisposition(adopted bool) string {
	if adopted {
		return execjournal.DispositionAdopted
	}
	return execjournal.DispositionPending
}

// fanOutOpenings turns the nodes a fan-out just declared into journal openings.
// It reads the same node list the graph delta carries, so the two cannot drift:
// an item the graph draws is an item the journal recorded.
func fanOutOpenings(workers []agentgraph.Node) []execjournal.Opening {
	out := make([]execjournal.Opening, 0, len(workers))
	for _, w := range workers {
		if w.Kind != agentgraph.KindWorker {
			continue
		}
		out = append(out, execjournal.Opening{
			ID: w.ID, Kind: string(w.Kind), Name: w.Label, Grant: string(w.Grant),
			Disposition: adoptedDisposition(w.State == agentgraph.StateAdopted),
		})
	}
	return out
}
