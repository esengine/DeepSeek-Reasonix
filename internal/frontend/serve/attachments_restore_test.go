package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func restoreServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: root})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

// Pasting a "copy all" payload asks the host whether each recorded path is
// still an attachment here; the answer is the token the composer puts back.
func TestRestoreAnswersStoredAttachmentsAndMissingOnes(t *testing.T) {
	root := testenv.TempDir(t)
	kept := ".reasonix/attachments/clipboard-20260101-000000.000000-000001.png"
	dir := filepath.Join(root, ".reasonix", "attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clipboard-20260101-000000.000000-000001.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string][]string{"paths": {kept, ".reasonix/attachments/clipboard-gone.png"}})
	resp, err := http.Post(restoreServer(t, root).URL+"/attachments/restore", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /attachments/restore: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /attachments/restore = %d", resp.StatusCode)
	}
	var got []control.RestoredAttachment
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("answer = %+v, want one entry per path", got)
	}
	if got[0].Ref != "@"+kept || !got[0].Image || got[0].Error != "" {
		t.Fatalf("restored = %+v, want the token for the file that is still here", got[0])
	}
	if got[1].Ref != "" || got[1].Error == "" {
		t.Fatalf("missing = %+v, want a refusal instead of a token", got[1])
	}
}

func TestRestoreRefusesAnEmptyList(t *testing.T) {
	resp, err := http.Post(restoreServer(t, testenv.TempDir(t)).URL+"/attachments/restore", "application/json", bytes.NewReader([]byte(`{"paths":[]}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty restore = %d, want 400", resp.StatusCode)
	}
}
