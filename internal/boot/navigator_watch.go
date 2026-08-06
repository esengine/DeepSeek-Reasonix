package boot

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/navigator"
)

// navigatorWatchRunner wraps an agent.Runner so the navigator's background
// environment watch runs for exactly the lifetime of one Run call (bound to
// the ctx the host passes in). When Run returns — success, error, or cancel —
// the watch goroutine stops, so long-lived CLI/desktop sessions never leak
// the watcher.
//
// The watch is the OSWorld 2.0 "dead light under the lamp" defense: changes
// that happen outside tool calls (a download finishing, a notification
// arriving, a background process appearing) are sampled and correlated into
// events the navigator flushes at the next EndAction, so the agent notices
// environment updates it was never told about directly.
type navigatorWatchRunner struct {
	agent.Runner
	kernel *navigator.Navigator
}

// Run starts the watch and delegates to the wrapped runner. The watch is
// bound to a derived context that is cancelled as soon as Run returns (even
// on error or panic), so long-lived hosts and tests never leak the watcher
// goroutine.
func (w *navigatorWatchRunner) Run(ctx context.Context, input string) error {
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	if w.kernel != nil {
		w.kernel.StartBackgroundWatch(watchCtx, navigator.DefaultWatchInterval)
	}
	return w.Runner.Run(ctx, input)
}
