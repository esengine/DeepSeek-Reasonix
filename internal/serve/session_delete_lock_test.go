package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestDeleteSessionSerializesWithForegroundPromotion(t *testing.T) {
	dir := t.TempDir()
	active, target := filepath.Join(dir, "active.jsonl"), filepath.Join(dir, "target.jsonl")
	for _, path := range []string{active, target} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bc := NewBroadcaster()
	first := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active})
	promoted := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: target})
	server := New(first, bc, config.ServeConfig{})
	reachedLock := make(chan struct{})
	deleteSessionBeforeOwnershipLockHookForTest = func() { close(reachedLock) }
	t.Cleanup(func() { deleteSessionBeforeOwnershipLockHookForTest = nil })
	server.bindMu.Lock()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.deleteSession(rec, httptest.NewRequest(http.MethodPost, "/delete-session", strings.NewReader(`{"name":"target"}`)))
		close(done)
	}()
	select {
	case <-reachedLock:
	case <-time.After(2 * time.Second):
		server.bindMu.Unlock()
		t.Fatal("delete did not reach ownership boundary")
	}
	if !server.publishControllerSwap(first, promoted, target) {
		server.bindMu.Unlock()
		t.Fatal("foreground promotion failed")
	}
	server.bindMu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("delete remained blocked after promotion")
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete promoted session status = %d, want 409", rec.Code)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("promoted session was deleted: %v", err)
	}
}
