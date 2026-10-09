package serve

import (
	"golang.org/x/sys/windows"
	"net/http"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
	"testing"
)

func TestRevisionPublicationCauseReachesHTTP(t *testing.T) {
	home, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, ".claude-plugin", "plugin.json"), `{"name":"neutral"}`)
	writePluginFile(t, filepath.Join(source, "skills", "neutral", "SKILL.md"), "---\ndescription: Neutral fixture\n---\nBODY")
	if err := pluginpkg.SaveState(home, pluginpkg.State{Version: 1}); err != nil {
		t.Fatal(err)
	}
	path, err := windows.UTF16PtrFromString(pluginpkg.StatePath(home))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	resp := postJSON(t, base+"/plugins/install", map[string]any{"source": source, "replace": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body := decodeInstallSource(t, resp)
	actions, _ := body["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions: %v", body)
	}
	act := actions[0].(map[string]any)
	if act["errorCode"] != "install.publication_failed" {
		t.Fatalf("origin cause lost: %v", body)
	}
	st, err := pluginpkg.LoadState(home)
	if err != nil || len(st.Plugins) != 0 {
		t.Fatalf("failed publication registered: %+v, %v", st, err)
	}
}
