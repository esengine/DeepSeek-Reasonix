package serve

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/platform/terminal"
	"reasonix/internal/session/control"
)

type rootCtl struct {
	control.SessionAPI
	root string
}

func (r rootCtl) WorkspaceRoot() string { return r.root }

func terminalServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	s := &Server{ctrl: rootCtl{root: t.TempDir()}, terminals: terminal.NewManager()}
	t.Cleanup(s.terminals.CloseAll)
	mux := http.NewServeMux()
	s.registerTerminalRoutes(mux)
	return s, mux
}

func terminalCall(mux *http.ServeMux, method, path, body string, device bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if device {
		req = req.WithContext(withDeviceReach(req.Context(), "phone-1", 1))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAPairedDeviceCannotUseTheHostsTerminals(t *testing.T) {
	s, mux := terminalServer(t)
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/terminals/shells"},
		{http.MethodGet, "/terminals"},
		{http.MethodPost, "/terminals"},
		{http.MethodDelete, "/terminals/x"},
		{http.MethodPost, "/terminals/x/restart"},
		{http.MethodPost, "/terminals/x/resize"},
		{http.MethodPost, "/terminals/x/input"},
		{http.MethodGet, "/terminals/x/stream"},
	} {
		rec := terminalCall(mux, rt.method, rt.path, `{}`, true)
		var reason Reason
		_ = json.Unmarshal(rec.Body.Bytes(), &reason)
		if rec.Code != http.StatusNotFound || reason.Code != codeDeviceHostOnly {
			t.Errorf("%s %s from a device: status %d code %q, want 404 %s", rt.method, rt.path, rec.Code, reason.Code, codeDeviceHostOnly)
		}
	}
	if n := len(s.terminals.List()); n != 0 {
		t.Fatalf("a refused device opened %d terminals", n)
	}
}

func TestAnUnknownTerminalIsRefusedByCode(t *testing.T) {
	_, mux := terminalServer(t)
	for _, rt := range []struct{ method, path string }{
		{http.MethodDelete, "/terminals/nope"},
		{http.MethodPost, "/terminals/nope/input"},
		{http.MethodPost, "/terminals/nope/resize"},
		{http.MethodPost, "/terminals/nope/restart"},
	} {
		rec := terminalCall(mux, rt.method, rt.path, `{"data":"x"}`, false)
		var reason Reason
		_ = json.Unmarshal(rec.Body.Bytes(), &reason)
		if rec.Code != http.StatusNotFound || reason.Code != codeTerminalNotFound {
			t.Errorf("%s %s: status %d code %q, want 404 %s", rt.method, rt.path, rec.Code, reason.Code, codeTerminalNotFound)
		}
	}
}

func TestATerminalOpensTakesInputAndStreamsItsOutputToTheEnd(t *testing.T) {
	shell := "cmd"
	exit := "exit\r\n"
	echo := "echo served-probe\r\n"
	if runtime.GOOS != "windows" {
		shell, exit, echo = "sh", "exit\n", "echo served-probe\n"
	}
	_, mux := terminalServer(t)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := http.Post(srv.URL+"/terminals", "application/json", strings.NewReader(`{"shell":"`+shell+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var info terminal.Info
	_ = json.NewDecoder(res.Body).Decode(&info)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || info.ID == "" {
		t.Fatalf("open: status %d info %+v", res.StatusCode, info)
	}

	stream, err := http.Get(srv.URL + "/terminals/" + info.ID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()

	for _, keys := range []string{echo, exit} {
		body, _ := json.Marshal(map[string]string{"data": keys})
		r, err := http.Post(srv.URL+"/terminals/"+info.ID+"/input", "application/json", strings.NewReader(string(body)))
		if err != nil || r.StatusCode != http.StatusOK {
			t.Fatalf("input: %v status %v", err, r)
		}
		r.Body.Close()
	}

	var out strings.Builder
	var last terminalFrame
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stream.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var f terminalFrame
			if json.Unmarshal([]byte(line), &f) != nil {
				continue
			}
			if b, err := base64.StdEncoding.DecodeString(f.Data); err == nil {
				out.Write(b)
			}
			last = f
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("the stream did not end; output so far %q", out.String())
	}
	if !strings.Contains(out.String(), "served-probe") {
		t.Fatalf("stream %q lacks the echoed text", out.String())
	}
	if last.Kind != "exit" || last.Code == nil || *last.Code != 0 {
		t.Fatalf("the stream must end with the exit code, got %+v", last)
	}
}

// A remote pane's terminals run on the far machine: the host's requests are
// forwarded to its kernel, and a paired device's never reach it.
func TestARemotePanesTerminalsAreForwardedForTheHostAndNeverForADevice(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	rk := fakeRemoteKernel(t)
	h := NewHub(HubOptions{})
	rt := remotePane(t, h, rk.Listener.Addr().String())
	prefix := rt.view().Base
	serve := func(method, path string, device bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, prefix+path, strings.NewReader("{}"))
		if device {
			req = req.WithContext(withDeviceReach(req.Context(), "phone-1", 1))
		}
		rec := httptest.NewRecorder()
		rt.handler.ServeHTTP(rec, req)
		return rec
	}
	for len(rk.cookies) > 0 {
		<-rk.cookies
	}
	for _, p := range []string{"/terminals", "/terminals/shells"} {
		if rec := serve(http.MethodGet, p, true); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), codeDeviceHostOnly) {
			t.Errorf("device GET %s = %d %s", p, rec.Code, rec.Body.String())
		}
	}
	if len(rk.cookies) != 0 {
		t.Fatal("a device's request reached the far kernel")
	}
	serve(http.MethodGet, "/terminals", false)
	if len(rk.cookies) != 1 {
		t.Fatalf("the host's request was forwarded %d times, want once", len(rk.cookies))
	}
}

func TestAMissingProjectFolderIsRefusedByCodeNotByTheSystemsSentence(t *testing.T) {
	_, err := resolveWorkspaceDir(filepath.Join(t.TempDir(), "gone", "worktree"))
	var c *coded
	if !errors.As(err, &c) || c.reason.Code != codeWorkspaceMissing || c.status != http.StatusBadRequest {
		t.Fatalf("a folder that is not there = %v, want a %s refusal with 400", err, codeWorkspaceMissing)
	}
	if c.reason.Params["path"] == "" {
		t.Fatal("the refusal must carry the path so the page can say which folder")
	}
}
