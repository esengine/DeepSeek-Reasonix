package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestDeleteSessionValidatesLocalBasenameBeforeCleanup(t *testing.T) {
	dir := t.TempDir()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: filepath.Join(dir, "active.jsonl")})
	handler := New(ctrl, bc, config.ServeConfig{}).Handler()
	post := func(name string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]string{"name": name})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/delete-session", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		return w
	}
	invalid := []string{"", " ", ".", "..", "../escape", `..\escape`, "/absolute", `\absolute`, "nested/session", `nested\session`}
	if runtime.GOOS == "windows" {
		// Device names with an extension are absent: Windows 11 stopped
		// reserving them, and filepath.IsLocal defers to the host's
		// RtlIsDosDeviceName_U, so their answer varies by Windows build.
		invalid = append(invalid, "C:escape", "C:", "CON", "NUL", "AUX", "COM1", "LPT1")
	}
	for _, name := range invalid {
		t.Run("reject "+name, func(t *testing.T) {
			if response := post(name); response.Code != http.StatusBadRequest {
				t.Fatalf("delete %q = %d (%s), want 400", name, response.Code, response.Body.String())
			}
		})
	}
	// IsLocal must not be replaced with a blanket dot-dot substring ban:
	// ordinary names containing dots, spaces and Unicode remain supported.
	for _, name := range []string{"saved..session", "session draft", "会话.v2"} {
		t.Run("delete "+name, func(t *testing.T) {
			path := filepath.Join(dir, name+".jsonl")
			if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if response := post(name); response.Code != http.StatusNoContent {
				t.Fatalf("delete %q = %d (%s), want 204", name, response.Code, response.Body.String())
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("session %q survived deletion: %v", name, err)
			}
		})
	}
}
