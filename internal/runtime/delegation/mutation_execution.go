package delegation

import (
	"context"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type mutationExecutionKey struct{}
type mutationExecutionValidator func(context.Context) error
type mutationSessionSource interface {
	CheckMutationSession(context.Context, *sessionstore.Session) error
}

// MutationExecutionValidator exposes only this run's live source check.
// The source workspace still owns permission, path grants and publication.
func MutationExecutionValidator(ctx context.Context) (func(context.Context) error, bool) {
	validate, ok := ctx.Value(mutationExecutionKey{}).(mutationExecutionValidator)
	return validate, ok
}

func carryMutationExecution(ctx, source context.Context) context.Context {
	validate, ok := source.Value(mutationExecutionKey{}).(mutationExecutionValidator)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, mutationExecutionKey{}, validate)
}

func (t *TaskTool) withMutationExecution(ctx context.Context, run *SubagentRun) context.Context {
	var live *liveRun
	var session *sessionstore.Session
	ref := ""
	if run != nil {
		session, ref = run.Session, run.Ref
	}
	var authority *sessionstore.SessionWriteAuthority
	if session != nil {
		authority = session.WriteAuthority()
	}
	if t.transcripts != nil && ref != "" {
		t.transcripts.mu.Lock()
		live = t.transcripts.live[ref]
		t.transcripts.mu.Unlock()
	}
	validate := mutationExecutionValidator(func(checkCtx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkCtx.Err(); err != nil {
			return err
		}
		if live == nil || session == nil || live.sess != session || session.WriteAuthority() != authority {
			return staleMutationExecution()
		}
		t.transcripts.mu.Lock()
		current := t.transcripts.live[ref]
		var source mutationSessionSource
		if current == live {
			source, _ = current.steer.(mutationSessionSource)
		}
		t.transcripts.mu.Unlock()
		if source == nil {
			return staleMutationExecution()
		}
		return source.CheckMutationSession(checkCtx, live.sess)
	})
	return context.WithValue(ctx, mutationExecutionKey{}, validate)
}

func staleMutationExecution() error {
	return tool.Refusal{Code: "isolation.stale_execution", Message: "delegated source execution no longer owns its live session"}
}
