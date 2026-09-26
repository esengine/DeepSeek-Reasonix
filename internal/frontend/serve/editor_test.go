package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func editorServer(t *testing.T, granted bool) *Server {
	t.Helper()
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	if granted {
		s.AllowLocalDesktop()
	}
	return s
}

// The editor launches on the machine running the kernel, so a server reached
// over the network must refuse rather than open a window nobody is sitting at.
// The grant is the gate, and it says which class of refusal this is.
func TestEditorOpenRefusesWithoutAHostGrant(t *testing.T) {
	s := editorServer(t, false)
	rec := httptest.NewRecorder()
	s.openInEditor(rec, httptest.NewRequest(http.MethodPost, "/workspace/editor", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("refusal is not an envelope: %s", rec.Body.String())
	}
	if body.Code != codeEditorNoWindow {
		t.Errorf("code = %q, want %q", body.Code, codeEditorNoWindow)
	}
}

// A machine with no editor is not a failure to launch one: the two ask the
// person for different things, so they carry different codes. An editor the
// user named that does not exist is the missing class on every machine,
// whatever it has installed, which a search of the machine could not promise.
func TestEditorOpenSeparatesMissingFromFailedToStart(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(t.TempDir(), "no-such-editor")
	if err := os.WriteFile(path, []byte("[desktop]\neditor = "+strconv.Quote(absent)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := editorServer(t, true)
	rec := httptest.NewRecorder()
	s.openInEditor(rec, httptest.NewRequest(http.MethodPost, "/workspace/editor", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), codeEditorMissing) {
		t.Errorf("refusal = %s, want %s", rec.Body.String(), codeEditorMissing)
	}
}
