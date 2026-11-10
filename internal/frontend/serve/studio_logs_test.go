package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestStudioLogsNamesTheDirectoryAndTheCrashFilesWaitingThere(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	logs := testenv.TempDir(t)
	crash := filepath.Join(logs, "crash")
	if err := os.MkdirAll(crash, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "host-20261010T070806Z-2.31.0-100.log"
	if err := os.WriteFile(filepath.Join(crash, name), []byte("fatal error: out of memory"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(NewHub(HubOptions{LogsDir: logs})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/studio/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got studioLogs
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d err %v", resp.StatusCode, err)
	}
	if got.Dir != logs || got.CrashDir != crash || len(got.Crashes) != 1 || got.Crashes[0].Name != name || got.Crashes[0].Bytes == 0 {
		t.Fatalf("answer = %+v", got)
	}
}

func TestStudioLogsRefusesByNameWhenNoShellDeclaredOne(t *testing.T) {
	srv := studioServer(t, nil)
	resp, err := http.Get(srv.URL + "/studio/logs")
	if err != nil {
		t.Fatal(err)
	}
	if code := refusalCode(t, resp); code != codeNoLogs {
		t.Fatalf("code = %q, want %q", code, codeNoLogs)
	}
}
