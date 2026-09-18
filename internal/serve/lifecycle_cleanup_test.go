package serve

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/session"
)

// Close the host rather than its initial controller: resume and model changes
// can install a replacement that owns a different session writer.
func newLifecycleTestServer(t *testing.T, ctrl control.SessionAPI, bc *Broadcaster, cfg config.ServeConfig) *Server {
	t.Helper()
	server := New(ctrl, bc, cfg)
	t.Cleanup(func() {
		var service *session.Service
		var runtime *session.Runtime
		if owner, ok := server.ctl().(interface {
			SessionBinding() (*session.Service, *session.Runtime, bool)
		}); ok {
			service, runtime, _ = owner.SessionBinding()
		}
		server.Close()
		if service == nil || runtime == nil {
			return
		}
		// The controller releases its client binding synchronously; the host
		// retires the idle runtime on a TTL. Force it so a leaked binding
		// (ErrRuntimeBound) still fails while normal closes settle at once.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			err := service.Close(context.Background(), runtime.Ref())
			if err == nil || errors.Is(err, session.ErrSessionNotRunning) {
				return
			}
			if !errors.Is(err, session.ErrRuntimeBound) && !errors.Is(err, session.ErrRuntimeBusy) {
				t.Errorf("server session writer did not release after close: %v", err)
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Error("server session writer did not retire after close")
	})
	return server
}
