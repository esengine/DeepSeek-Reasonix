package control

import (
	"reasonix/internal/contract/event"
	"reasonix/internal/state/sessionstore"
)

// Cleanup is downstream of the root session commit. A deletion failure retains
// its durable intent without stranding the newly published session's authority.
func (c *Controller) finishSessionClear(oldPath string, destroy SessionDestroyHandle, postCleanup bool) {
	if postCleanup && !destroy.Async {
		if err := removeSessionArtifacts(oldPath); err != nil {
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
				Text: "Conversation cleared; old transcript cleanup remains pending.", Detail: err.Error()})
		}
		destroy.Finish()
	}
	if destroy.Async {
		go func() {
			result := destroy.Wait()
			if result.HasTimedOut() && destroy.WaitAll != nil {
				if err := sessionstore.MarkCleanupPending(oldPath, "clear"); err != nil {
					c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "mark cleanup pending failed: " + err.Error()})
				}
				destroy.WaitAll()
			}
			if err := removeSessionArtifacts(oldPath); err != nil {
				c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "clear session cleanup failed: " + err.Error()})
			}
			destroy.Finish()
		}()
	}
}
