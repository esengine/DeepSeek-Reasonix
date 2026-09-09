package control

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/sessioninbox"
)

func TestControllerShutdownJoinsInboxScanBeforeReturning(t *testing.T) {
	for _, mode := range []string{"close", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			c := New(Options{SessionPath: filepath.Join(t.TempDir(), "session.jsonl")})
			scanReached := make(chan struct{})
			releaseScan := make(chan struct{})
			var releaseOnce sync.Once
			defer func() {
				releaseOnce.Do(func() { close(releaseScan) })
				c.Close()
				c.autosaveWG.Wait()
			}()
			c.SetBeforeInboxDispatch(func(*Controller) (func(), error) { return nil, nil })
			c.inbox.afterDispatchScan = func(bool) {
				close(scanReached)
				<-releaseScan
			}
			c.NotifyInboxRuntimeReady()
			select {
			case <-scanReached:
			case <-time.After(inboxDispatchTestTimeout):
				t.Fatal("dispatcher did not reach the controlled scan boundary")
			}
			closed := make(chan struct{})
			go func() {
				if mode == "replacement" {
					c.ReleaseResources()
				} else {
					c.Close()
				}
				close(closed)
			}()
			// The channel fixes the interleaving; this observation window asserts
			// that shutdown cannot finish until the scan is released.
			select {
			case <-closed:
				t.Fatal("shutdown returned while the inbox scan still owned its sidecar access")
			case <-time.After(100 * time.Millisecond):
			}
			releaseOnce.Do(func() { close(releaseScan) })
			select {
			case <-closed:
			case <-time.After(inboxDispatchTestTimeout):
				t.Fatal("shutdown did not finish after the scan was released")
			}
			c.autosaveWG.Wait()
		})
	}
}

func TestInboxDispatchHostAdmissionCanRetireItsController(t *testing.T) {
	c := New(Options{SessionPath: filepath.Join(t.TempDir(), "session.jsonl")})
	defer func() {
		c.Close()
		c.autosaveWG.Wait()
	}()
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentFollowup, Submit: "queued"}); err != nil {
		t.Fatal(err)
	}
	retired := make(chan struct{})
	c.SetBeforeInboxDispatch(func(current *Controller) (func(), error) {
		current.ReleaseResources()
		close(retired)
		return nil, ErrInboxRuntimeUnpublished
	})
	if err := c.SetInboxPaused(false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-retired:
	case <-time.After(inboxDispatchTestTimeout):
		t.Fatal("host admission could not retire its dispatching controller")
	}
	c.autosaveWG.Wait()
}
