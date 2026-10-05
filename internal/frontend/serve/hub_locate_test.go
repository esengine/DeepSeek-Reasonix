package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func locateWorkspaceAt(h *Hub, root string) (int, workspaceLocation, Reason) {
	rec := httptest.NewRecorder()
	h.locateWorkspace(rec, httptest.NewRequest(http.MethodGet, "/host/workspaces/locate?root="+url.QueryEscape(root), nil))
	var at workspaceLocation
	var why Reason
	if rec.Code == http.StatusOK {
		_ = json.Unmarshal(rec.Body.Bytes(), &at)
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &why)
	}
	return rec.Code, at, why
}

func TestLocateWorkspaceNamesOnlyListedProjects(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	listed, other := testenv.TempDir(t), testenv.TempDir(t)
	rememberWorkspace(listed)
	h := NewHub(HubOptions{})

	abs, _ := filepath.Abs(listed)
	if code, at, why := locateWorkspaceAt(h, listed); code != http.StatusOK || at.Path != abs || !at.Dir {
		t.Fatalf("a listed project = %d %+v %+v, want %q", code, at, why, abs)
	}
	if code, _, why := locateWorkspaceAt(h, other); code != http.StatusNotFound || why.Code != codeWorkspaceUnknown {
		t.Fatalf("an unlisted folder = %d %+v, want %s", code, why, codeWorkspaceUnknown)
	}
	if code, _, why := locateWorkspaceAt(h, ""); code != http.StatusBadRequest {
		t.Fatalf("no root = %d %+v, want a refusal", code, why)
	}
}

func TestLocateWorkspaceReportsAFolderThatIsGone(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	gone := filepath.Join(testenv.TempDir(t), "gone")
	rememberWorkspace(gone)
	if code, _, why := locateWorkspaceAt(NewHub(HubOptions{}), gone); code != http.StatusNotFound || why.Code != codeWorkspaceMissing {
		t.Fatalf("a missing folder = %d %+v, want %s", code, why, codeWorkspaceMissing)
	}
}
