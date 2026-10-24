package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestPresentFilesSettingsPersistWithoutRebuilding(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	controller := s.ctl()
	initial := config.LoadForEdit(config.UserConfigPath()).Tools.PresentFiles
	if got := readJSON[map[string]any](t, srv.URL, "/present-files"); got["enabled"] != initial {
		t.Fatalf("read = %v, want saved choice %v", got, initial)
	}
	for _, enabled := range []bool{!initial, initial} {
		resp := postProvider(t, srv.URL, "/present-files", fmt.Sprintf(`{"enabled":%t}`, enabled))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("save = %d", resp.StatusCode)
		}
		if config.LoadForEdit(config.UserConfigPath()).Tools.PresentFiles != enabled {
			t.Fatal("choice did not reach user config")
		}
		if got := readJSON[map[string]any](t, srv.URL, "/present-files"); got["enabled"] != enabled {
			t.Fatalf("reread = %v, want %v", got, enabled)
		}
		if s.ctl() != controller {
			t.Fatal("saving rebuilt the running session")
		}
	}
}

func TestPresentFilesSettingsRejectInvalidAndUnauthorizedWrites(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	initial := config.LoadForEdit(config.UserConfigPath()).Tools.PresentFiles
	resp := postProvider(t, srv.URL, "/present-files", fmt.Sprintf(`{"enabled":%t}`, !initial))
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ungranted save = %d", resp.StatusCode)
	}
	s.AllowProviderEdit()
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"true"}`, `{`} {
		resp = postProvider(t, srv.URL, "/present-files", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s: status %d", body, resp.StatusCode)
		}
	}
	if config.LoadForEdit(config.UserConfigPath()).Tools.PresentFiles != initial {
		t.Fatal("rejected request changed user config")
	}
}

func TestPresentFilesSettingsPreserveOtherUserSettings(t *testing.T) {
	srv := newRichProviderServer(t)
	enabled := !config.LoadForEdit(config.UserConfigPath()).Tools.PresentFiles
	resp := postProvider(t, srv.URL, "/present-files", fmt.Sprintf(`{"enabled":%t}`, enabled))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", resp.StatusCode)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	provider, ok := cfg.Provider("rich")
	if !ok || provider.ContextWindow != 131072 || provider.Prices["alpha"].Input != 1 {
		t.Fatal("file-delivery toggle changed provider configuration")
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), fmt.Sprintf("present_files = %t", enabled)) {
		t.Fatal("missing saved choice")
	}
}
