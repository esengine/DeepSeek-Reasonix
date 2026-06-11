package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScrubUserPaths(t *testing.T) {
	cases := map[string]string{
		`at C:\Users\yuhua\proj\app.ts:12:3`:      `at C:\Users\_\proj\app.ts:12:3`,
		`at c:\users\someone\x.go`:                `at c:\users\_\x.go`,
		`/home/bob/.config/reasonix/config.toml`:  `/home/_/.config/reasonix/config.toml`,
		`/Users/alice/Library/Logs`:               `/Users/_/Library/Logs`,
		`Error: ENOENT open '/home/bob/secret'`:   `Error: ENOENT open '/home/_/secret'`,
		`no user path here: /usr/lib/node`:        `no user path here: /usr/lib/node`,
		"first /home/a/x\nsecond C:\\Users\\b\\y": "first /home/_/x\nsecond C:\\Users\\_\\y",
	}
	for in, want := range cases {
		if got := scrubUserPaths(in); got != want {
			t.Errorf("scrubUserPaths(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPostCrashReport(t *testing.T) {
	var got crashReport
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body not JSON: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	r := crashReport{Kind: "crash", Version: "v9.9.9", OS: "windows", Arch: "amd64", Message: "[react]\nboom"}
	if err := postCrashReport(context.Background(), srv.Client(), srv.URL, r); err != nil {
		t.Fatal(err)
	}
	if got != r {
		t.Errorf("server received %+v, want %+v", got, r)
	}
}

func TestPostCrashReportRejectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	err := postCrashReport(context.Background(), srv.Client(), srv.URL, crashReport{Kind: "crash"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("want 429 error, got %v", err)
	}
}

func TestReportCrashRejectsBadInput(t *testing.T) {
	app := NewApp()
	if err := app.ReportCrash("telemetry", "x"); err == nil {
		t.Error("unknown kind should be rejected")
	}
	if err := app.ReportCrash("crash", ""); err == nil {
		t.Error("empty detail should be rejected")
	}
}
