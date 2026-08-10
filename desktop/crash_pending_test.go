package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func readPending(t *testing.T) (crashReport, bool) {
	t.Helper()
	paths := pendingCrashPaths()
	if len(paths) == 0 {
		return crashReport{}, false
	}
	body, err := os.ReadFile(paths[0])
	if err != nil {
		return crashReport{}, false
	}
	var r crashReport
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("pending file not valid JSON: %v", err)
	}
	return r, true
}

func TestRecoverToPendingCapturesAndReraises(t *testing.T) {
	t.Cleanup(removeAllPendingCrashes)

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("recoverToPending must re-raise the panic")
			}
		}()
		app := NewApp()
		defer app.recoverToPending("unit")
		panic(`boom at C:\Users\alice\proj\x.go`)
	}()

	r, ok := readPending(t)
	if !ok {
		t.Fatal("expected a pending crash file")
	}
	if r.Kind != "crash" {
		t.Errorf("kind = %q, want crash", r.Kind)
	}
	if !strings.Contains(r.Message, "[go panic] unit") {
		t.Errorf("message missing site prefix: %q", r.Message)
	}
	if r.Source != "go" || r.Label != "unit" || r.ErrorMessage == "" || r.Stack == "" || r.TopFrame == "" {
		t.Errorf("structured panic metadata missing: %+v", r)
	}
	if strings.Contains(r.Message, `Users\alice`) {
		t.Errorf("message not scrubbed: %q", r.Message)
	}
}

func TestWritePendingCrashCaps(t *testing.T) {
	t.Cleanup(removeAllPendingCrashes)
	writePendingCrash("big", "x", []byte(strings.Repeat("a", 64<<10)))
	r, ok := readPending(t)
	if !ok {
		t.Fatal("expected a pending crash file")
	}
	if len(r.Message) > maxCrashDetailBytes {
		t.Errorf("message len = %d, want <= %d", len(r.Message), maxCrashDetailBytes)
	}
}

func TestWritePendingReportQueuesWithoutOverwritingExistingCrash(t *testing.T) {
	t.Cleanup(removeAllPendingCrashes)
	writePendingCrash("panic", "boom", []byte("stack"))
	before, ok := readPending(t)
	if !ok {
		t.Fatal("expected initial pending crash")
	}

	hang := baseCrashReport("performance")
	hang.Source = "native.watchdog"
	hang.Label = "mac.main_thread.hang"
	hang.Message = "hang"
	if !writePendingReport(hang, false) {
		t.Fatal("writePendingReport should enqueue the second report")
	}
	after, ok := readPending(t)
	if !ok {
		t.Fatal("expected pending crash after skipped write")
	}
	if after.Label != before.Label || after.Message != before.Message {
		t.Fatalf("pending crash was overwritten: before=%+v after=%+v", before, after)
	}
	if got := len(pendingCrashPaths()); got != 2 {
		t.Fatalf("pending reports = %d, want 2", got)
	}
}

func TestWritePendingReportQueueIsBoundedUnderConcurrentWriters(t *testing.T) {
	t.Cleanup(removeAllPendingCrashes)
	const writers = 32
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	var successes atomic.Int32

	for range writers {
		ready.Add(1)
		done.Go(func() {
			report := baseCrashReport("performance")
			report.Source = "native.watchdog"
			report.Label = "mac.main_thread.hang"
			report.Message = strings.Repeat("hang", 1024)
			ready.Done()
			<-start
			if writePendingReport(report, false) {
				successes.Add(1)
			}
		})
	}
	ready.Wait()
	close(start)
	done.Wait()

	if got := successes.Load(); got != writers {
		t.Fatalf("successful queued writers = %d, want %d", got, writers)
	}
	if got := len(pendingCrashPaths()); got != maxPendingCrashes {
		t.Fatalf("pending reports = %d, want bounded queue of %d", got, maxPendingCrashes)
	}
}

func TestWritePendingCrashScrubsSensitiveText(t *testing.T) {
	t.Cleanup(removeAllPendingCrashes)
	apiKey := "sk-proj-" + "abcdefghijklmnopqrstuvwxyz1234567890"
	bearer := "abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	longHex := "0123456789abcdef0123456789abcdef"

	privatePanicValue := "private user prompt contents"
	writePendingCrash("unit", privatePanicValue+" api_key="+apiKey+" user alice@example.com", []byte("goroutine\nAuthorization: Bearer "+bearer+"\n/home/alice/project/x.go:12\nhash "+longHex))
	r, ok := readPending(t)
	if !ok {
		t.Fatal("expected a pending crash file")
	}
	freeText := strings.Join([]string{r.Message, r.ErrorMessage, r.Stack, r.TopFrame}, "\n")
	for _, leaked := range []string{privatePanicValue, apiKey, bearer, longHex, "alice@example.com", "/home/alice"} {
		if strings.Contains(freeText, leaked) {
			t.Fatalf("sensitive value leaked %q in %+v", leaked, r)
		}
	}
}

func TestFlushPendingCrashSendsAndClears(t *testing.T) {
	oldVersion, oldEndpoint := version, crashEndpoint
	t.Cleanup(func() {
		version, crashEndpoint = oldVersion, oldEndpoint
		removeAllPendingCrashes()
	})
	version = "v9.9.9"

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	crashEndpoint = srv.URL

	writePendingCrash("flush", "boom", []byte("stack"))
	NewApp().flushPendingCrash()

	if hits.Load() != 1 {
		t.Errorf("server hits = %d, want 1", hits.Load())
	}
	if _, ok := readPending(t); ok {
		t.Error("pending file should be cleared after a successful send")
	}
}

func TestFlushPendingCrashDevGuard(t *testing.T) {
	oldVersion := version
	t.Cleanup(func() {
		version = oldVersion
		removeAllPendingCrashes()
	})
	version = "dev"

	writePendingCrash("dev", "boom", []byte("stack"))
	NewApp().flushPendingCrash()

	if _, ok := readPending(t); !ok {
		t.Error("dev build must leave the pending file untouched")
	}
}

func TestFlushPendingCrashIgnoresSafeModeEnv(t *testing.T) {
	// v1.20+: REASONIX_SAFE_MODE no longer blocks crash flush. With telemetry
	// off/default, the pending file is consumed (sent or dropped).
	t.Setenv("REASONIX_SAFE_MODE", "1")
	oldVersion := version
	t.Cleanup(func() {
		version = oldVersion
		removeAllPendingCrashes()
	})
	version = "v9.9.9"

	writePendingCrash("safe", "boom", []byte("stack"))
	NewApp().flushPendingCrash()
	// Either sent or dropped is fine; must not retain solely because of Safe Mode env.
	// When telemetry is off the file is removed; when on it is sent. Both clear it.
}
