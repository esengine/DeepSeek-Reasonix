package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestNativeImportRejectsUnknownWorkspaceBeforeFilesystemLookup(t *testing.T) {
	h, _, _, _ := twoProjects(t)
	unknown := filepath.Join(testenv.TempDir(t), "missing-workspace")
	r := httptest.NewRequest(http.MethodPost, "/tree/sessions/import-native?workspace="+url.QueryEscape(unknown), strings.NewReader("{}"))
	w := httptest.NewRecorder()
	h.importNativeSession(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unregistered workspace reached filesystem lookup: %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusForbidden || body["code"] != "workspace.unknown" {
		t.Fatalf("unregistered workspace reached filesystem lookup: %d %v", w.Code, body)
	}
}

func TestNativeImportRequiresAnExplicitWorkspace(t *testing.T) {
	h, _, _, _ := twoProjects(t)
	r := httptest.NewRequest(http.MethodPost, "/tree/sessions/import-native?workspace=", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	h.importNativeSession(w, r)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest || body["code"] != codeMissingField {
		t.Fatalf("missing workspace selected a default: %d %v", w.Code, body)
	}
}
