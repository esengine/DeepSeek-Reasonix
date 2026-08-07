package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func telemetryResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}
}

func testClient(home string, transport http.RoundTripper) *Client {
	return &Client{
		home:      home,
		version:   "v1.20.0",
		installID: strings.Repeat("a", 32),
		http:      &http.Client{Transport: transport},
	}
}

func TestInstallIDRepairsMalformedOwnedFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "cli-telemetry-install-id")
	if err := os.WriteFile(path, []byte("truncated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	id, err := installID(home)
	if err != nil {
		t.Fatalf("installID: %v", err)
	}
	if !validInstallID(id) {
		t.Fatalf("repaired install id = %q", id)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != id {
		t.Fatalf("persisted install id = %q, want %q", got, id)
	}
}

func TestDailyPingSendsOnceWithCLISurface(t *testing.T) {
	home := t.TempDir()
	var mu sync.Mutex
	var payloads []pingPayload
	client := testClient(home, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != endpoint+"/ping" {
			t.Fatalf("request URL = %q", req.URL)
		}
		var payload pingPayload
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		payloads = append(payloads, payload)
		mu.Unlock()
		return telemetryResponse(http.StatusAccepted), nil
	}))

	if err := client.sendDailyPing(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.sendDailyPing(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 1 {
		t.Fatalf("ping requests = %d, want 1", len(payloads))
	}
	if payloads[0].Surface != "cli" || payloads[0].InstallID != client.installID {
		t.Fatalf("ping payload = %+v", payloads[0])
	}
}

func TestFailedDailyPingRemovesClaimAndRetries(t *testing.T) {
	home := t.TempDir()
	calls := 0
	client := testClient(home, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("offline")
		}
		return telemetryResponse(http.StatusAccepted), nil
	}))

	if err := client.sendDailyPing(context.Background()); err == nil {
		t.Fatal("first ping unexpectedly succeeded")
	}
	claim := filepath.Join(home, "cli-telemetry-ping-"+time.Now().UTC().Format("2006-01-02"))
	if _, err := os.Stat(claim); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed ping claim remains: %v", err)
	}
	if err := client.sendDailyPing(context.Background()); err != nil {
		t.Fatalf("retry ping: %v", err)
	}
	if calls != 2 {
		t.Fatalf("ping calls = %d, want 2", calls)
	}
}

func TestFlushPendingAggregatesAndDeletesOnlyAfterSuccess(t *testing.T) {
	home := t.TempDir()
	for _, counters := range [][]Counter{
		{{Signal: "turns", Bucket: "count", Count: 2}},
		{{Signal: "turns", Bucket: "count", Count: 3}, {Signal: "cli_exit", Bucket: "success", Count: 1}},
	} {
		if err := appendPending(home, pendingPayload{Version: "v1.20.0", OS: "android", Counters: counters}); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	var client *Client
	client = testClient(home, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		var payload metricsPayload
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Surface != "cli" || payload.OS != "android" || payload.InstallID != client.installID {
			t.Fatalf("metrics payload = %+v", payload)
		}
		got := map[string]int{}
		for _, counter := range payload.Counters {
			got[counter.Signal+"/"+counter.Bucket] = counter.Count
		}
		if got["turns/count"] != 5 || got["cli_exit/success"] != 1 {
			t.Fatalf("aggregated counters = %#v", got)
		}
		return telemetryResponse(http.StatusAccepted), nil
	}))

	if err := client.flushPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("metrics requests = %d, want 1", requests)
	}
	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 0 {
		t.Fatalf("pending entries after success = %d, err = %v", len(entries), err)
	}
}

func TestFailedFlushRestoresClaimsForRetry(t *testing.T) {
	home := t.TempDir()
	if err := appendPending(home, pendingPayload{
		Version: "v1.20.0", OS: "linux", Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := testClient(home, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return telemetryResponse(http.StatusServiceUnavailable), nil
		}
		return telemetryResponse(http.StatusAccepted), nil
	}))

	if err := client.flushPending(context.Background()); err == nil {
		t.Fatal("first flush unexpectedly succeeded")
	}
	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Fatalf("failed flush entries = %v, err = %v", entries, err)
	}
	if err := client.flushPending(context.Background()); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	entries, err = os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 0 {
		t.Fatalf("pending entries after retry = %d, err = %v", len(entries), err)
	}
}

func TestPendingClaimsAreExclusiveAcrossFlushers(t *testing.T) {
	home := t.TempDir()
	if err := appendPending(home, pendingPayload{
		Version: "v1.20.0", OS: "linux", Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, pendingDirName)
	first, err := claimPendingFiles(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := claimPendingFiles(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("claims: first=%v second=%v", first, second)
	}
}

func TestPendingValidationAcceptsAndroid(t *testing.T) {
	if !validPendingPayload(pendingPayload{
		Version: "v1.20.0", OS: "android", Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}},
	}) {
		t.Fatal("Android CLI payload was rejected")
	}
}

func TestPendingQueueCountsActiveAndRecoversStaleClaims(t *testing.T) {
	dir := filepath.Join(t.TempDir(), pendingDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := range maxPending {
		path := filepath.Join(dir, strings.Repeat("a", 16)+"-"+time.Unix(int64(i), 0).Format("150405")+".json.uploading")
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := appendPending(filepath.Dir(dir), pendingPayload{
		Version: "v1.20.0", OS: "linux", Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != maxPending {
		t.Fatalf("bounded queue entries = %d, err = %v", len(entries), err)
	}

	staleDir := filepath.Join(t.TempDir(), pendingDirName)
	if err := os.MkdirAll(staleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staleClaim := filepath.Join(staleDir, "sample.json.uploading")
	if err := os.WriteFile(staleClaim, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(staleClaim, stale, stale); err != nil {
		t.Fatal(err)
	}
	if !prunePending(staleDir, time.Now()) {
		t.Fatal("stale claim recovery did not make a queue slot")
	}
	if _, err := os.Stat(strings.TrimSuffix(staleClaim, ".uploading")); err != nil {
		t.Fatalf("stale claim was not recovered: %v", err)
	}
}
