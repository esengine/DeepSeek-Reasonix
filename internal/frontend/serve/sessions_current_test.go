package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// The same session file reaches the two endpoints spelled differently: the
// project slug is case-folded on Windows, while /resume stores the path
// EvalSymlinks resolved back to the on-disk casing. Comparing with
// filepath.Clean marked no row current, so the sidebar highlighted nothing and
// showed a phantom "new session" row for a session that was right there.
func TestSessionsMarksCurrentAcrossPathSpelling(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("session paths are case-folded on Windows only")
	}
	dir := testenv.TempDir(t)
	name := "20260813-120000.000000000-model.jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("{\"role\":\"user\",\"content\":\"hi\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// What the controller carries after a resume: a differently-cased spelling
	// of the very path the listing builds from its own session dir.
	ctrl := control.New(control.Options{
		SessionDir:  dir,
		SessionPath: filepath.Join(strings.ToUpper(dir), name),
	})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []struct {
		Path    string `json:"path"`
		Current bool   `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("listed %d sessions, want 1", len(got))
	}
	if !got[0].Current {
		t.Errorf("session %s not marked current; the sidebar has nothing to select", got[0].Path)
	}
}
