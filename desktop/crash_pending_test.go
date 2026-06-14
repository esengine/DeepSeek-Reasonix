package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func readPending(t *testing.T) (crashReport, bool) {
	t.Helper()
	body, err := os.ReadFile(pendingCrashPath())
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
	t.Cleanup(func() { os.Remove(pendingCrashPath()) })

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
	if !strings.Contains(r.Message, "[go panic] unit:") {
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
	t.Cleanup(func() { os.Remove(pendingCrashPath()) })
	writePendingCrash("big", "x", []byte(strings.Repeat("a", 64<<10)))
	r, ok := readPending(t)
	if !ok {
		t.Fatal("expected a pending crash file")
	}
	if len(r.Message) > maxCrashDetailBytes {
		t.Errorf("message len = %d, want <= %d", len(r.Message), maxCrashDetailBytes)
	}
}

func TestWritePendingCrashScrubsSensitiveText(t *testing.T) {
	t.Cleanup(func() { os.Remove(pendingCrashPath()) })
	apiKey := "sk-proj-" + "abcdefghijklmnopqrstuvwxyz1234567890"
	bearer := "abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	longHex := "0123456789abcdef0123456789abcdef"

	writePendingCrash("unit", "boom api_key="+apiKey+" user alice@example.com", []byte("goroutine\nAuthorization: Bearer "+bearer+"\n/home/alice/project/x.go:12\nhash "+longHex))
	r, ok := readPending(t)
	if !ok {
		t.Fatal("expected a pending crash file")
	}
	freeText := strings.Join([]string{r.Message, r.ErrorMessage, r.Stack, r.TopFrame}, "\n")
	for _, leaked := range []string{apiKey, bearer, longHex, "alice@example.com", "/home/alice"} {
		if strings.Contains(freeText, leaked) {
			t.Fatalf("sensitive value leaked %q in %+v", leaked, r)
		}
	}
}

func TestFlushPendingCrashSendsAndClears(t *testing.T) {
	oldVersion, oldEndpoint := version, crashEndpoint
	t.Cleanup(func() {
		version, crashEndpoint = oldVersion, oldEndpoint
		os.Remove(pendingCrashPath())
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
		os.Remove(pendingCrashPath())
	})
	version = "dev"

	writePendingCrash("dev", "boom", []byte("stack"))
	NewApp().flushPendingCrash()

	if _, ok := readPending(t); !ok {
		t.Error("dev build must leave the pending file untouched")
	}
}
