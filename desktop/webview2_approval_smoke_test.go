package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebView2ApprovalSmokeMiddleware(t *testing.T) {
	activeWebView2ApprovalSmoke.enabled = true
	t.Cleanup(func() { activeWebView2ApprovalSmoke.enabled = false })
	app := &App{}
	handler := app.webView2ApprovalSmokeMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><html><head></head><body></body></html>"))
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(recorder.Body.String(), "__REASONIX_WEBVIEW2_APPROVAL_SMOKE__=true") {
		t.Fatal("smoke marker was not injected before the frontend bundle")
	}
}

func TestWebView2ApprovalSmokeMiddlewareDisabled(t *testing.T) {
	app := &App{}
	handler := app.webView2ApprovalSmokeMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<head></head>"))
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(recorder.Body.String(), "REASONIX_WEBVIEW2_APPROVAL_SMOKE") {
		t.Fatal("ordinary desktop responses must not expose the release smoke marker")
	}
}

func TestWebView2ApprovalSmokeRequiresWindowsAndExplicitOptIn(t *testing.T) {
	tests := []struct {
		name  string
		goos  string
		value string
		want  bool
	}{
		{name: "Windows enabled", goos: "windows", value: "1", want: true},
		{name: "Windows whitespace", goos: "windows", value: " 1 ", want: true},
		{name: "Windows disabled", goos: "windows", value: "0", want: false},
		{name: "macOS cannot impersonate WebView2", goos: "darwin", value: "1", want: false},
		{name: "Linux cannot impersonate WebView2", goos: "linux", value: "1", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := webView2ApprovalSmokeRequestedFor(test.goos, test.value); got != test.want {
				t.Fatalf("webView2ApprovalSmokeRequestedFor(%q, %q) = %v, want %v", test.goos, test.value, got, test.want)
			}
		})
	}
}
