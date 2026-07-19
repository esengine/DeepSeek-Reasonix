package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestRemoteWindowTicketRoundTripAndRemoval(t *testing.T) {
	launch := remoteWindowLaunch{
		URL:   "http://127.0.0.1:54321/?token=secret-token",
		Title: "Reasonix [SSH: box]",
	}
	ticket, err := writeRemoteWindowLaunch(launch)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ticket, "secret-token") || filepath.Base(ticket) != ticket {
		t.Fatalf("ticket leaked URL data or path: %q", ticket)
	}
	path, err := remoteWindowTicketPath(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("ticket permissions = %o, want 600", got)
		}
	}
	got, err := consumeRemoteWindowLaunch(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if *got != launch {
		t.Fatalf("launch = %+v, want %+v", *got, launch)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ticket was not removed after consumption: %v", err)
	}
}

func TestRemoteWindowTicketRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []string{
		"https://127.0.0.1:5000/?token=x",
		"http://example.com:5000/?token=x",
		"file:///tmp/index.html",
		"javascript:alert(1)",
	} {
		if _, err := writeRemoteWindowLaunch(remoteWindowLaunch{URL: raw}); err == nil {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
	for _, ticket := range []string{"", "../.remote-window-x", "/tmp/.remote-window-x", "unrelated"} {
		if _, err := remoteWindowTicketPath(ticket); err == nil {
			t.Fatalf("unsafe ticket accepted: %q", ticket)
		}
	}
}

func TestConsumeRemoteWindowTicketRejectsBroadPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits through os.Stat")
	}
	dir := config.MemoryUserDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ticket := remoteWindowTicketPrefix + "insecure"
	path := filepath.Join(dir, ticket)
	if err := os.WriteFile(path, []byte(`{"url":"http://127.0.0.1:5000/"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if _, err := consumeRemoteWindowLaunch(ticket); err == nil {
		t.Fatal("ticket with broad permissions was accepted")
	}
}

func TestConsumeRemoteWindowTicketRejectsOversizedDescriptor(t *testing.T) {
	dir := config.MemoryUserDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ticket := remoteWindowTicketPrefix + "oversized"
	path := filepath.Join(dir, ticket)
	if err := os.WriteFile(path, make([]byte, remoteWindowTicketMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := consumeRemoteWindowLaunch(ticket); err == nil {
		t.Fatal("oversized ticket was accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected ticket was not removed: %v", err)
	}
}

func TestRemoteWindowNavigationJSEscapesURL(t *testing.T) {
	js, err := remoteWindowNavigationJS("http://127.0.0.1:5000/?token=x%22);alert(1)//")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(js, "window.location.replace(\"") || !strings.HasSuffix(js, "\");") {
		t.Fatalf("unexpected navigation JS: %q", js)
	}
	if strings.Contains(js, "\");alert") {
		t.Fatalf("URL escaped the JS string: %q", js)
	}
}

func TestRemoteWindowLifecycleSkipsPrimaryRuntime(t *testing.T) {
	a := NewApp()
	a.remoteWindow = &remoteWindowLaunch{URL: "http://127.0.0.1:5000/"}
	a.startup(context.Background())
	if a.tabsRestored != nil {
		t.Fatal("remote window initialized local tab restore")
	}
	if a.heartbeat != nil || a.tray != nil || a.remoteRuntime != nil {
		t.Fatal("remote window initialized primary-process runtime")
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("remote window close was intercepted")
	}
	a.shutdown(context.Background())
}

func TestRemoteWindowAssetMiddlewareDoesNotLoadPrimaryFrontend(t *testing.T) {
	a := &App{remoteWindow: &remoteWindowLaunch{URL: "http://127.0.0.1:5000/"}}
	nextCalled := false
	h := a.remoteWindowAssetMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if nextCalled {
		t.Fatal("remote shell loaded the primary asset handler")
	}
	if strings.Contains(rec.Body.String(), "<script") {
		t.Fatal("remote shell bootstrap unexpectedly contains frontend scripts")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestRemoteWindowTitleSanitizesHostLabel(t *testing.T) {
	if got := remoteWindowTitle(" box\nprod "); got != "Reasonix [SSH: boxprod]" {
		t.Fatalf("title = %q", got)
	}
}
