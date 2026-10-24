package proc

import "context"

type completionObserverKey struct{}

// WithCompletionObserver lets an execution owner retain resources until every
// tracked OS wait finishes, including waits that outlive cancellation grace.
func WithCompletionObserver(ctx context.Context, observe func(<-chan struct{})) context.Context {
	return context.WithValue(ctx, completionObserverKey{}, observe)
}

func observeCompletion(ctx context.Context, done <-chan struct{}) {
	if observe, _ := ctx.Value(completionObserverKey{}).(func(<-chan struct{})); observe != nil {
		observe(done)
	}
}

// WaitCompleted closes when the OS wait and output collection finish. Nil means
// no tracked wait began. A cancellation grace return does not close this signal.
func (p *TrackedCommand) WaitCompleted() <-chan struct{} {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitDone
}

// AfterWait keeps cleanup ownership until an unsettled process wait completes.
func AfterWait(done <-chan struct{}, release func()) {
	if done == nil {
		release()
		return
	}
	select {
	case <-done:
		release()
	default:
		go func() { <-done; release() }()
	}
}
