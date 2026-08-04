package scheduler

import "context"

// ctxKey is the call-context key for the session scheduler, mirroring the
// jobs.WithManager / jobs.FromContext injection pattern.
type ctxKey struct{}

// NewContext stamps ctx with the scheduler so the cron_* and schedule_wakeup
// tools can reach it via FromContext.
func NewContext(ctx context.Context, s *Scheduler) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext returns the scheduler stamped by the agent's turn context, if
// any. ok is false for a plain context (headless tests, calls outside the run
// loop).
func FromContext(ctx context.Context) (*Scheduler, bool) {
	s, ok := ctx.Value(ctxKey{}).(*Scheduler)
	return s, ok && s != nil
}
