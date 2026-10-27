package delegation

import (
	"context"
	"reasonix/internal/contract/event"
	"sync/atomic"
)

func (t *TaskTool) adaptiveRunner(run func(context.Context, event.Sink, bool) (string, error)) func(context.Context, event.Sink, bool) (string, error) {
	return func(ctx context.Context, sink event.Sink, registered bool) (answer string, err error) {
		sink, complete := t.adaptiveSink(sink)
		defer func() { complete(err) }()
		return run(ctx, sink, registered)
	}
}

func (t *TaskTool) adaptiveSink(sink event.Sink) (event.Sink, func(error)) {
	var throttled atomic.Bool
	return event.FuncSink(func(e event.Event) {
			if e.Kind == event.Retrying && (e.RetryStatus == 429 || e.RetryStatus == 503) && !throttled.Swap(true) {
				t.scheduler.ObserveBackpressure()
			}
			sink.Emit(e)
		}), func(err error) {
			if err == nil && !throttled.Load() {
				t.scheduler.ObserveSuccess()
			}
		}
}
