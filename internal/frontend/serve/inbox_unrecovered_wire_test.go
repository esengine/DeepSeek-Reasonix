package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/session/control"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/store"
)

// breakInboxForServe stages the portable "bodies cannot be read" state: the
// queue holds one durable body, then a regular file replaces the blobs
// directory and the manifest is replaced with undecodable bytes.
func breakInboxForServe(t *testing.T, session string) {
	t.Helper()
	s, err := sessioninbox.Open(session, sessioninbox.Limits{})
	if err != nil {
		t.Fatalf("open a fresh inbox: %v", err)
	}
	if _, err := s.Enqueue(sessioninbox.EnqueueRequest{Envelope: sessioninbox.PromptEnvelope{SubmitText: "the user's lost prompt"}}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	s.Close()

	inboxDir := store.SessionInboxDir(session)
	blobs := filepath.Join(inboxDir, "blobs")
	if err := os.Rename(blobs, filepath.Join(inboxDir, "blobs-preserved")); err != nil {
		t.Fatalf("preserve blobs: %v", err)
	}
	if err := os.WriteFile(blobs, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("replace blobs with a file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inboxDir, "manifest.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}
}

func decodeInboxBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("GET /inbox body is not JSON: %v\n%s", err, rec.Body.String())
	}
	return raw
}

// The window reads GET /inbox to decide whether the panel is worth drawing at
// all. An unreadable queue whose bodies are still on disk must reach the wire
// as unknown, not as the empty list that hides them.
func TestInboxWireSaysUnreadableNotEmpty(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	breakInboxForServe(t, session)

	s := &Server{ctrl: control.New(control.Options{SessionDir: dir, SessionPath: session})}
	defer s.ctrl.Close()

	rec := httptest.NewRecorder()
	s.inboxList(rec, httptest.NewRequest(http.MethodGet, "/inbox", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /inbox = %d %s", rec.Code, rec.Body.String())
	}
	raw := decodeInboxBody(t, rec)
	if raw["unrecovered"] != true {
		t.Fatalf("GET /inbox did not say the contents are unknown: %s", rec.Body.String())
	}
	if raw["paused"] != true {
		t.Fatalf("an unknown queue must arrive paused: %s", rec.Body.String())
	}
	if items, ok := raw["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("unknown queue items = %v, want an empty array", raw["items"])
	}
	if strings.Contains(rec.Body.String(), `"recovered":true`) {
		t.Fatalf("unknown queue claimed a recovery: %s", rec.Body.String())
	}
}

// An empty-but-readable queue is the case the unknown marker must not touch:
// the window has to keep drawing "nothing waiting" there.
func TestInboxWireEmptyQueueDoesNotClaimUnknown(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{ctrl: control.New(control.Options{SessionDir: dir, SessionPath: session})}
	defer s.ctrl.Close()

	rec := httptest.NewRecorder()
	s.inboxList(rec, httptest.NewRequest(http.MethodGet, "/inbox", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /inbox = %d %s", rec.Code, rec.Body.String())
	}
	raw := decodeInboxBody(t, rec)
	if _, present := raw["unrecovered"]; present {
		t.Fatalf("an empty readable queue carried the unknown marker: %s", rec.Body.String())
	}
	if raw["paused"] == true {
		t.Fatalf("an empty readable queue arrived paused: %s", rec.Body.String())
	}
}
