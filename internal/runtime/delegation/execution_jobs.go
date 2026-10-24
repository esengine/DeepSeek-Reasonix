package delegation

import (
	"context"
	"errors"

	"reasonix/internal/contract/event"
	"reasonix/internal/tools/jobs"
)

type executionJobsKey struct{}

func (p *profileExecution) ownExecutionJobs(ctx context.Context, sink event.Sink) (context.Context, func(*error)) {
	var manager *jobs.Manager
	if p.task.environmentID != "" && !p.spec.Grant.ReadOnly {
		manager = jobs.NewManager(sink)
	}
	ctx = context.WithValue(ctx, executionJobsKey{}, manager)
	ctx = jobs.WithExecutionOwner(ctx, manager)
	if manager == nil {
		return ctx, func(*error) {}
	}
	stop := context.AfterFunc(ctx, manager.Cancel)
	return ctx, func(runErr *error) {
		caught := recover()
		defer manager.Close()
		defer stop()
		if *runErr != nil || caught != nil {
			manager.Cancel()
		}
		*runErr = errors.Join(*runErr, manager.SealAndWait(ctx))
		if caught != nil {
			panic(caught)
		}
	}
}
