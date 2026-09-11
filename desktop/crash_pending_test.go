package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
	// v1.20+: REASONIX_SAFE_MODE no longer blocks crash flush.
	t.Setenv("REASONIX_SAFE_MODE", "1")
	oldVersion, oldEndpoint := version, crashEndpoint
	t.Cleanup(func() {
		version = oldVersion
		crashEndpoint = oldEndpoint
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

	writePendingCrash("safe", "boom", []byte("stack"))
	NewApp().flushPendingCrash()
	if hits.Load() != 1 {
		t.Fatalf("server hits = %d, want 1", hits.Load())
	}
	if _, ok := readPending(t); ok {
		t.Fatal("safe-mode compatibility left a sent report pending")
	}
}

func TestPendingCrashDoesNotPersistInstallID(t *testing.T) {
	removeAllPendingCrashes()
	t.Cleanup(removeAllPendingCrashes)
	report := baseCrashReport("crash")
	report.Message = "pending"
	if !writePendingReport(report, true) {
		t.Fatal("writePendingReport failed")
	}
	paths := pendingCrashQueuePaths()
	if len(paths) != 1 {
		t.Fatalf("pending paths = %v", paths)
	}
	body, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("installId")) || bytes.Contains(body, []byte("install-id")) {
		t.Fatalf("pending report persisted an installation identity: %s", body)
	}
}

func TestFlushPendingCrashDeduplicatesSameVersionAndResendsAfterUpgrade(t *testing.T) {
	removeAllPendingCrashes()
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

	report := baseCrashReport("crash")
	report.Source = "go"
	report.ErrorType = "panic"
	report.TopFrame = "main.go:12"
	report.Message = "same crash"
	firstQueued := writePendingReport(report, false)
	secondQueued := writePendingReport(report, false)
	if !firstQueued || !secondQueued {
		t.Fatal("failed to queue duplicate reports")
	}
	NewApp().flushPendingCrash()
	if got := hits.Load(); got != 1 {
		t.Fatalf("same-version uploads = %d, want 1", got)
	}

	report.Version = "v10.0.0"
	report.EventID, report.DedupKey = "", ""
	if !writePendingReport(report, false) {
		t.Fatal("failed to queue upgraded report")
	}
	NewApp().flushPendingCrash()
	if got := hits.Load(); got != 2 {
		t.Fatalf("uploads after version upgrade = %d, want 2", got)
	}
	info, err := os.Stat(crashLedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("ledger permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestConcurrentFlushPendingCrashUsesOneCrossProcessLedgerOwner(t *testing.T) {
	removeAllPendingCrashes()
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
	writePendingCrash("concurrent", "boom", []byte("stack"))

	start := make(chan struct{})
	var done sync.WaitGroup
	for range 2 {
		done.Go(func() {
			<-start
			NewApp().flushPendingCrash()
		})
	}
	close(start)
	done.Wait()
	if got := hits.Load(); got != 1 {
		t.Fatalf("concurrent uploads = %d, want 1", got)
	}
}

func TestFlushPendingCrashFailureDoesNotRecordOrDelete(t *testing.T) {
	removeAllPendingCrashes()
	oldVersion, oldEndpoint := version, crashEndpoint
	t.Cleanup(func() {
		version, crashEndpoint = oldVersion, oldEndpoint
		removeAllPendingCrashes()
	})
	version = "v9.9.9"
	status := atomic.Int32{}
	status.Store(http.StatusServiceUnavailable)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()
	crashEndpoint = srv.URL
	writePendingCrash("retry", "boom", []byte("stack"))

	NewApp().flushPendingCrash()
	if _, ok := readPending(t); !ok {
		t.Fatal("failed upload removed pending crash")
	}
	if ledger := loadCrashLedger(crashLedgerPath(), time.Now().UTC()); len(ledger.Entries) != 0 {
		t.Fatalf("failed upload recorded ledger entry: %+v", ledger.Entries)
	}
	status.Store(http.StatusAccepted)
	NewApp().flushPendingCrash()
	if hits.Load() != 2 {
		t.Fatalf("retry requests = %d, want 2", hits.Load())
	}
	if _, ok := readPending(t); ok {
		t.Fatal("successful retry left pending crash")
	}
}

func TestFlushPendingCrashBackfillsOldIdentityAndPreservesFutureSchema(t *testing.T) {
	removeAllPendingCrashes()
	oldVersion, oldEndpoint := version, crashEndpoint
	t.Cleanup(func() {
		version, crashEndpoint = oldVersion, oldEndpoint
		removeAllPendingCrashes()
	})
	version = "v9.9.9"
	if err := os.MkdirAll(pendingCrashDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(pendingCrashDir(), "001-old.json")
	futurePath := filepath.Join(pendingCrashDir(), "002-future.json")
	oldReport := `{"kind":"crash","version":"v9.9.9","os":"linux","arch":"amd64","message":"old","schemaVersion":2,"source":"go","errorType":"panic","topFrame":"main.go:12"}`
	futureReport := `{"kind":"crash","version":"v10.0.0","os":"linux","arch":"amd64","message":"future","schemaVersion":99,"futureField":"keep"}`
	if err := os.WriteFile(oldPath, []byte(oldReport), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(futurePath, []byte(futureReport), 0o600); err != nil {
		t.Fatal(err)
	}
	var uploaded crashReport
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&uploaded); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	crashEndpoint = srv.URL

	NewApp().flushPendingCrash()
	if len(uploaded.EventID) != 32 || len(uploaded.DedupKey) != 64 {
		t.Fatalf("old report identity not backfilled: %+v", uploaded)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old report was not removed after send: %v", err)
	}
	body, err := os.ReadFile(futurePath)
	if err != nil || !bytes.Contains(body, []byte(`"futureField":"keep"`)) {
		t.Fatalf("future schema was not preserved: body=%s err=%v", body, err)
	}
}

func TestFlushPendingCrashUploadsCurrentSchemaThree(t *testing.T) {
	removeAllPendingCrashes()
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

	report := desktopLifecycleReport(desktopLifecycleObservation{
		Version: "v9.9.8",
		Phase:   "ready",
	})
	if report.SchemaVersion != currentCrashSchema {
		t.Fatalf("current producer schema = %d, supported = %d", report.SchemaVersion, currentCrashSchema)
	}
	if !writePendingReport(report, false) {
		t.Fatal("failed to queue current-schema report")
	}
	NewApp().flushPendingCrash()
	if got := hits.Load(); got != 1 {
		t.Fatalf("current-schema uploads = %d, want 1", got)
	}
	if got := len(pendingCrashPaths()); got != 0 {
		t.Fatalf("pending reports after upload = %d, want 0", got)
	}
}

func TestWritePendingReportPrunesKnownReportsWithoutDeletingFutureSchema(t *testing.T) {
	removeAllPendingCrashes()
	t.Cleanup(removeAllPendingCrashes)
	if err := os.MkdirAll(pendingCrashDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	futurePath := filepath.Join(pendingCrashDir(), "000-future.json")
	futureReport := `{"schemaVersion":99,"futureField":"keep"}`
	if err := os.WriteFile(futurePath, []byte(futureReport), 0o600); err != nil {
		t.Fatal(err)
	}
	for index := range maxPendingCrashes {
		report := baseCrashReport("crash")
		report.SchemaVersion = currentCrashSchema
		report.Source = "go"
		report.Label = fmt.Sprintf("panic-%d", index)
		report.Message = "bounded current report"
		if !writePendingReport(report, false) {
			t.Fatalf("failed to queue current report %d", index)
		}
	}
	body, err := os.ReadFile(futurePath)
	if err != nil || !bytes.Contains(body, []byte(`"futureField":"keep"`)) {
		t.Fatalf("future schema was pruned: body=%s err=%v", body, err)
	}
	if got := len(pendingCrashQueuePaths()); got != maxPendingCrashes {
		t.Fatalf("pending reports = %d, want bounded queue of %d", got, maxPendingCrashes)
	}
}
