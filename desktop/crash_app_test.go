package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestScrubSensitiveText(t *testing.T) {
	apiKey := "sk-proj-" + "abcdefghijklmnopqrstuvwxyz1234567890"
	bearer := "abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	longHex := "0123456789abcdef0123456789abcdef"
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.signature"
	got := scrubSensitiveText("user dev@example.com Authorization: Bearer " + bearer + " api_key=" + apiKey + " jwt " + jwt + " hash " + longHex + " env FEISHU_BOT_APP_SECRET WEIXIN_BOT_TOKEN short abc1234 path /Users/alice/x")

	for _, leaked := range []string{"dev@example.com", bearer, apiKey, jwt, longHex, "FEISHU_BOT_APP_SECRET", "WEIXIN_BOT_TOKEN", "alice"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("sensitive text leaked %q in %q", leaked, got)
		}
	}
	for _, want := range []string{"[redacted-email]", "Authorization=[redacted]", "api_key=[redacted]", "[redacted-jwt]", "[redacted-hex]", "[redacted-env]", "short abc1234", "/Users/_/x"} {
		if !strings.Contains(got, want) {
			t.Fatalf("scrubSensitiveText() = %q, want it to contain %q", got, want)
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
	if !reflect.DeepEqual(got, r) {
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

func TestCrashReportFromStructuredDetail(t *testing.T) {
	apiKey := "sk-proj-" + "abcdefghijklmnopqrstuvwxyz1234567890"
	secretHex := "abcdefabcdefabcdefabcdefabcdef12"
	buildCommit := "0123456789abcdef0123456789abcdef01234567"
	payload := frontendCrashPayload{
		SchemaVersion: 2,
		Kind:          "exception",
		Source:        "frontend",
		Label:         "unhandledrejection",
		Message:       "[unhandledrejection]\n\ninvalid argument at C:\\Users\\alice\\app.ts:1 from alice@example.com",
		ErrorType:     "TypeError",
		ErrorMessage:  "invalid argument at /Users/alice/project/app.ts api_key=" + apiKey,
		Stack:         "TypeError: invalid argument\n    at run (/Users/alice/project/app.ts:12:3)\nsecret=" + secretHex,
		TopFrame:      "at run (/Users/alice/project/app.ts:12:3)",
		BuildCommit:   buildCommit,
		Channel:       "canary",
		Language:      "zh-CN",
		View:          "wails://wails.localhost/index.html?token=" + secretHex,
		Breadcrumbs:   []crashBreadcrumb{{T: 1, Cat: "bridge", Msg: "turn SubmitToTab token=" + apiKey}},
	}
	detail, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r, err := crashReportFromDetail("crash", string(detail))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "exception" || r.Source != "frontend" || r.Label != "unhandledrejection" {
		t.Fatalf("structured fields not preserved: %+v", r)
	}
	if strings.Contains(r.Message, "alice") || strings.Contains(r.ErrorMessage, "alice") || strings.Contains(r.Stack, "alice") {
		t.Fatalf("user path was not scrubbed: %+v", r)
	}
	if r.TopFrame == "" || r.BuildCommit != buildCommit || r.Channel != "canary" || len(r.Breadcrumbs) != 1 {
		t.Fatalf("metadata missing: %+v", r)
	}
	freeText := strings.Join([]string{
		r.Message,
		r.ErrorMessage,
		r.Stack,
		r.ComponentStack,
		r.TopFrame,
		r.View,
		r.Breadcrumbs[0].Msg,
	}, "\n")
	for _, leaked := range []string{apiKey, secretHex, "alice@example.com"} {
		if strings.Contains(freeText, leaked) {
			t.Fatalf("sensitive value leaked %q in %+v", leaked, r)
		}
	}
}

func TestCrashReportFromPerformanceDetail(t *testing.T) {
	payload := frontendCrashPayload{
		SchemaVersion: 2,
		Kind:          "performance",
		Source:        "frontend.performance",
		Label:         "performance.pressure",
		Message:       "[performance.pressure]\n\n--- performance context ---\nreason: event loop lag 1300ms",
		ErrorType:     "PerformancePressure",
		ErrorMessage:  "UI responsiveness degraded because the app observed long tasks, event-loop lag, or high JS heap pressure.",
		TopFrame:      "frontend.performance",
	}
	detail, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r, err := crashReportFromDetail("performance", string(detail))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "performance" || r.Source != "frontend.performance" || r.Label != "performance.pressure" {
		t.Fatalf("performance fields not preserved: %+v", r)
	}
	if !strings.Contains(r.Message, "--- native runtime context ---") || !strings.Contains(r.Message, "goroutines:") {
		t.Fatalf("native runtime context missing from performance report: %q", r.Message)
	}
}

func TestCrashReportFromBotDetail(t *testing.T) {
	token := "abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	payload := frontendCrashPayload{
		SchemaVersion: 2,
		Kind:          "bot",
		Source:        "bot.runtime",
		Label:         "bot.feishu.lark.send",
		Message:       "[bot]\n\nfailed at /Users/alice/project with token=" + token,
		ErrorType:     "BotConnectionDiagnostic",
		ErrorMessage:  "send failed with Bearer " + token,
		TopFrame:      "bot.send",
	}
	detail, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r, err := crashReportFromDetail("bot", string(detail))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "bot" || r.Source != "bot.runtime" || r.Label != "bot.feishu.lark.send" {
		t.Fatalf("bot fields not preserved: %+v", r)
	}
	if strings.Contains(r.Message, "alice") || strings.Contains(r.Message, token) || strings.Contains(r.ErrorMessage, token) {
		t.Fatalf("bot report was not scrubbed: %+v", r)
	}
	if strings.Contains(r.Message, "--- native runtime context ---") {
		t.Fatalf("bot report should not include performance runtime context: %q", r.Message)
	}
}
