package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPickLocalFolderHTTPReturnsChosenPath(t *testing.T) {
	old := openLocalFolderPicker
	t.Cleanup(func() { openLocalFolderPicker = old })
	openLocalFolderPicker = func(_ context.Context, start string) (string, error) {
		if start != `D:\work` {
			t.Fatalf("start = %q", start)
		}
		return `D:\work\project`, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/host/pick-folder", strings.NewReader(`{"startIn":"D:\\work"}`))
	rec := httptest.NewRecorder()
	new(Hub).pickLocalFolderHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Path != `D:\work\project` {
		t.Fatalf("path = %q", body.Path)
	}
}

func TestPickLocalFolderHTTPReportsUnsupported(t *testing.T) {
	old := openLocalFolderPicker
	t.Cleanup(func() { openLocalFolderPicker = old })
	openLocalFolderPicker = func(context.Context, string) (string, error) {
		return "", errFolderPickerUnsupported
	}

	rec := httptest.NewRecorder()
	new(Hub).pickLocalFolderHTTP(rec, httptest.NewRequest(http.MethodPost, "/host/pick-folder", nil))
	if rec.Code != 501 {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHostCapabilitiesReportTheHeadlessFallbackPath(t *testing.T) {
	old := folderPickerAvailable
	folderPickerAvailable = func() bool { return false }
	t.Cleanup(func() { folderPickerAvailable = old })
	t.Setenv("REASONIX_HOME", t.TempDir())

	srv := httptest.NewServer(operatorHandler(NewHub(HubOptions{})))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/host/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /host/capabilities = %d", resp.StatusCode)
	}
	var got struct {
		PickFolder   bool `json:"pickFolder"`
		AddWorkspace bool `json:"addWorkspace"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.PickFolder {
		t.Fatal("headless kernel reported a native picker")
	}
	if !got.AddWorkspace {
		t.Fatal("kernel refused the path-based workspace API")
	}
}
