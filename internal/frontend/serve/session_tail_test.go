package serve

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/session/control"
)

func tailServer(t *testing.T) *Server {
	t.Helper()
	dir := testenv.TempDir(t)
	ctrl := control.New(control.Options{WorkspaceRoot: testenv.TempDir(t), SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl")})
	t.Cleanup(ctrl.Close)
	return &Server{ctrl: ctrl}
}

func TestSessionTailNeedsAPath(t *testing.T) {
	rec := httptest.NewRecorder()
	tailServer(t).sessionTail(rec, httptest.NewRequest(http.MethodPost, "/session/tail", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a request with no path = %d, want 400", rec.Code)
	}
}

func TestSessionTailRefusesAPathOutsideTheSessionDirectory(t *testing.T) {
	outside := filepath.Join(testenv.TempDir(t), "20261027-100000.000000000-m.jsonl")
	rec := httptest.NewRecorder()
	tailServer(t).sessionTail(rec, httptest.NewRequest(http.MethodPost, "/session/tail", strings.NewReader(`{"path":"`+outside+`"}`)))
	if rec.Code == http.StatusOK || rec.Code == http.StatusInternalServerError {
		t.Fatalf("a path outside the session directory = %d, want a refusal", rec.Code)
	}
}
