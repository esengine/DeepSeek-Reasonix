package control

import (
	"context"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/jobs"
)

// SessionDestroyHandle separates cancelled-job waiting from ending the destroy
// window, allowing persistent artifacts to move between Wait and Finish.
type SessionDestroyHandle struct {
	Wait    func() jobs.TeardownResult
	WaitFor func(time.Duration) jobs.TeardownResult
	WaitAll func()
	Finish  func()
	Async   bool
}

func (c *Controller) BeginDestroySession(sessionPath string) SessionDestroyHandle {
	parentSession := agent.BranchID(sessionPath)
	if c.jobs == nil || parentSession == "" {
		wait := func() jobs.TeardownResult { return jobs.TeardownResult{} }
		noop := func() {}
		return SessionDestroyHandle{Wait: wait, WaitAll: noop, Finish: noop}
	}
	teardown := c.jobs.BeginDestroySession(parentSession)
	return SessionDestroyHandle{
		Wait: func() jobs.TeardownResult {
			return c.jobs.WaitTeardown(context.Background(), teardown, c.jobs.TeardownGrace())
		},
		WaitFor: func(requested time.Duration) jobs.TeardownResult {
			grace := c.jobs.TeardownGrace()
			if requested >= 0 && requested < grace {
				grace = requested
			}
			return c.jobs.WaitTeardown(context.Background(), teardown, grace)
		},
		WaitAll: func() {
			for _, ch := range teardown.DoneChannels() {
				<-ch
			}
		},
		Finish: func() { c.jobs.FinishDestroySession(parentSession) },
		Async:  teardown.Async(),
	}
}

func (c *Controller) IsDestroyingSession(sessionPath string) bool {
	return c.jobs != nil && c.jobs.IsDestroying(agent.BranchID(sessionPath))
}
