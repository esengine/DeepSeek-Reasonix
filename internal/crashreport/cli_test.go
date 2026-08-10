package crashreport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCapturePanicWritesBoundedSanitizedReport(t *testing.T) {
	home := t.TempDir()
	secret := "private prompt contents"
	apiKey := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890"
	stack := "goroutine 7 [running]:\n" +
		"reasonix/internal/agent.run(" + secret + ")\n" +
		"\t/Users/alice/private-project/internal/agent/run.go:42 +0x123\n" +
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz1234567890\n" +
		"api_key=" + apiKey

	if err := CapturePanic(home, "v1.20.0", secret+" api_key="+apiKey, []byte(stack)); err != nil {
		t.Fatal(err)
	}
	reports, err := List(home)
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports=%d err=%v", len(reports), err)
	}
	report := reports[0].Report
	if report.Kind != "crash" || report.Source != "cli.go" || report.Label != "panic" || report.SchemaVersion != 2 {
		t.Fatalf("report metadata = %+v", report)
	}
	if !strings.Contains(report.Stack, "reasonix/internal/agent.run(...)") || !strings.Contains(report.Stack, "<path>/run.go:42") {
		t.Fatalf("sanitized stack = %q", report.Stack)
	}
	if report.TopFrame != "reasonix/internal/agent.run <path>/run.go:42" {
		t.Fatalf("top frame = %q", report.TopFrame)
	}
	preview, err := Preview(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{secret, apiKey, "alice", "private-project", "Bearer abcdefghijklmnopqrstuvwxyz1234567890"} {
		if strings.Contains(string(preview), leaked) {
			t.Fatalf("report leaked %q:\n%s", leaked, preview)
		}
	}
	report.ErrorType = "api_key=" + apiKey
	preview, err = Preview(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(preview), apiKey) {
		t.Fatalf("send-time field sanitization leaked a key:\n%s", preview)
	}
	path := filepath.Join(home, dirName, reports[0].ID+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows reports synthesized POSIX permission bits and enforces access
	// through inherited ACLs, so only Unix-like systems can assert mode 0600.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("report mode=%v", info.Mode().Perm())
	}

	for i := range maxReports + 5 {
		if err := CapturePanic(home, "v1.20.0", i, []byte(stack)); err != nil {
			t.Fatal(err)
		}
	}
	reports, err = List(home)
	if err != nil || len(reports) != maxReports {
		t.Fatalf("bounded reports=%d err=%v", len(reports), err)
	}
}

func TestSendUsesSharedProtocolWithoutDeletingLocalReport(t *testing.T) {
	home := t.TempDir()
	if err := CapturePanic(home, "v1.20.0", "boom", []byte("goroutine 1 [running]:\nreasonix.run()\n\t/home/alice/reasonix/main.go:12")); err != nil {
		t.Fatal(err)
	}
	pending, err := Load(home, "")
	if err != nil {
		t.Fatal(err)
	}
	var uploaded Report
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://example.invalid/v1/report" {
			t.Fatalf("request = %s %s", req.Method, req.URL)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type = %q", got)
		}
		if err := json.NewDecoder(req.Body).Decode(&uploaded); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusAccepted, Status: "202 Accepted", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}
	if err := sendWithClient(context.Background(), client, "https://example.invalid/v1/report", pending.Report); err != nil {
		t.Fatal(err)
	}
	if uploaded.Source != "cli.go" || uploaded.Stack == "" || uploaded.TopFrame == "" {
		t.Fatalf("uploaded report = %+v", uploaded)
	}
	if _, err := Load(home, pending.ID); err != nil {
		t.Fatalf("Send removed local report: %v", err)
	}
	if err := Remove(home, pending.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home, ""); !errors.Is(err, ErrNoReports) {
		t.Fatalf("Load after Remove = %v", err)
	}
}

func TestLoadRejectsUnknownIDWithoutPathTraversal(t *testing.T) {
	home := t.TempDir()
	if err := CapturePanic(home, "v1.20.0", "boom", []byte("stack")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home, "../../config.toml"); err == nil {
		t.Fatal("path traversal ID was accepted")
	}
}

func TestConcurrentCaptureKeepsQueueBounded(t *testing.T) {
	home := t.TempDir()
	const writers = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range writers {
		wg.Add(1)
		go func(value int) {
			defer wg.Done()
			<-start
			if err := CapturePanic(home, "v1.20.0", value, []byte("reasonix.run()\n\t/home/alice/main.go:12")); err != nil {
				t.Errorf("CapturePanic: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	reports, err := List(home)
	if err != nil || len(reports) != maxReports {
		t.Fatalf("reports=%d err=%v", len(reports), err)
	}
}

func TestCapturePanicPrunesOnlyCurrentReportFormat(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, dirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	futurePath := filepath.Join(dir, "00000000000000000000-1-0000000000000000.json")
	futureReport := `{"kind":"crash","version":"v2.0.0","os":"linux","arch":"amd64","message":"future","schemaVersion":3,"futureField":"preserve me"}`
	if err := os.WriteFile(futurePath, []byte(futureReport), 0o600); err != nil {
		t.Fatal(err)
	}

	for i := range maxReports + 1 {
		if err := CapturePanic(home, "v1.20.0", i, []byte("reasonix.run()\n\t/home/alice/main.go:12")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(futurePath); err != nil {
		t.Fatalf("future report was removed: %v", err)
	}
	reports, err := List(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != maxReports {
		t.Fatalf("current reports=%d, want %d", len(reports), maxReports)
	}
}

func TestSanitizingLimitPreservesUTF8(t *testing.T) {
	got := sanitizeText(strings.Repeat("界", maxFieldBytes), maxFieldBytes)
	if len(got) > maxFieldBytes || !utf8.ValidString(got) {
		t.Fatalf("sanitized text bytes=%d valid=%v", len(got), utf8.ValidString(got))
	}
}
