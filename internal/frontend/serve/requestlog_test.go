package serve

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func captureInfoLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestRoutineRequestsStayOutOfTheInfoLog(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{BrowserHost: NewBrowserHost()})
	rt := hubRuntime(t, h, testenv.TempDir(t))
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	logged := captureInfoLog(t)

	do := func(method, path, body string) int {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	for _, req := range []struct{ method, path, body string }{
		{http.MethodPost, "/browser-host/frames", "[]"},
		{http.MethodGet, "/rt/" + rt.ID + "/status", ""},
		{http.MethodGet, "/status", ""},
	} {
		if code := do(req.method, req.path, req.body); code >= 400 {
			t.Fatalf("%s %s = %d, want success", req.method, req.path, code)
		}
	}
	if got := logged.String(); got != "" {
		t.Fatalf("successful routine requests reached the INFO log:\n%s", got)
	}

	if code := do(http.MethodPost, "/browser-host/frames", "not json"); code < 400 {
		t.Fatalf("malformed frames = %d, want a refusal", code)
	}
	do(http.MethodGet, "/sessions", "")
	got := logged.String()
	if !strings.Contains(got, "path=/browser-host/frames") || !strings.Contains(got, "status=400") {
		t.Fatalf("a refused routine request must still be logged, got:\n%s", got)
	}
	if !strings.Contains(got, "path=/sessions") {
		t.Fatalf("an ordinary request must still be logged, got:\n%s", got)
	}
}
