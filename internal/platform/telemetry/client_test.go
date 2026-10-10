package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reasonix/internal/contract/hostaudit"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestMain(m *testing.M) {
	endpoint = "http://127.0.0.1:0/v1"
	os.Exit(m.Run())
}

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
	home := testenv.TempDir(t)
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
	home := testenv.TempDir(t)
	var mu sync.Mutex
	var payloads []pingPayload
	client := testClient(home, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != endpoint+"/ping" {
			t.Fatalf("request URL = %q", req.URL)
		}
		if ua := req.Header.Get("User-Agent"); ua != provider.ClientUserAgent() {
			t.Fatalf("User-Agent = %q", ua)
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
	home := testenv.TempDir(t)
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
	home := testenv.TempDir(t)
	for _, counters := range [][]Counter{
		{{Signal: "turns", Bucket: "count", Count: 2}},
		{{Signal: "turns", Bucket: "count", Count: 3}, {Signal: "cli_exit", Bucket: "success", Count: 1}},
	} {
		if err := appendPending(home, pendingPayload{Version: "v1.20.0", OS: "android", Counters: counters}); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	client := testClient(home, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		var payload metricsPayload
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Surface != "cli" || payload.OS != "android" {
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
	home := testenv.TempDir(t)
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
	home := testenv.TempDir(t)
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
	dir := filepath.Join(testenv.TempDir(t), pendingDirName)
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

	staleDir := filepath.Join(testenv.TempDir(t), pendingDirName)
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

// uploadSignals is the real boundary. A counter the sink records but the
// allow-list omits is written to the queue and then dropped in silence, which
// from every local test looks exactly like a feature that works.
func TestTurnTokenCountersSurviveTheUploadAllowList(t *testing.T) {
	home := testenv.TempDir(t)
	sink := (&Reporter{home: home, version: "v1.20.0", surface: surface.Desktop}).Wrap(&readinessSink{})
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 120_000, CompletionTokens: 2_100, CacheHitTokens: 96_000,
	}})
	sink.Emit(event.Event{Kind: event.TurnDone})

	posted := flushCollecting(t, home)
	if len(posted) != 1 {
		t.Fatalf("posted %d payloads, want 1", len(posted))
	}
	if posted[0].Surface != surface.Desktop.String() {
		t.Errorf("surface = %q, want %q", posted[0].Surface, surface.Desktop)
	}
	got := map[string]string{}
	for _, counter := range posted[0].Counters {
		got[counter.Signal] = counter.Bucket
	}
	for signal, want := range map[string]string{
		"turn_prompt_tokens": "64k_256k",
		"turn_output_tokens": "1k_4k",
		"turn_cached_tokens": "64k_256k",
	} {
		if got[signal] != want {
			t.Errorf("%s reached /metrics as %q, want %q", signal, got[signal], want)
		}
	}
}

// One machine's queue is shared by every surface that runs on it. Grouping that
// ignored surface would post a desktop turn as part of a cli payload — the
// exact attribution error this histogram exists to avoid.
func TestFlushDoesNotFoldOneSurfaceIntoAnother(t *testing.T) {
	home := testenv.TempDir(t)
	for _, from := range []surface.Surface{surface.CLI, surface.Desktop, surface.Serve} {
		sink := (&Reporter{home: home, version: "v1.20.0", surface: from}).Wrap(&readinessSink{})
		sink.Emit(event.Event{Kind: event.TurnStarted})
		sink.Emit(event.Event{Kind: event.TurnDone})
	}

	seen := map[string]int{}
	for _, payload := range flushCollecting(t, home) {
		seen[payload.Surface]++
	}
	for _, from := range []surface.Surface{surface.CLI, surface.Desktop, surface.Serve} {
		if seen[from.String()] != 1 {
			t.Errorf("surface %q posted %d times, want 1 (all: %v)", from, seen[from.String()], seen)
		}
	}
}

// A queue inherited from a version that predates the field still uploads, as
// the surface it could only have come from.
func TestFlushTreatsAQueueWithoutSurfaceAsCLI(t *testing.T) {
	home := testenv.TempDir(t)
	if err := appendPending(home, pendingPayload{
		Version: "v1.20.0", OS: runtime.GOOS,
		Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	posted := flushCollecting(t, home)
	if len(posted) != 1 || posted[0].Surface != surface.CLI.String() {
		t.Fatalf("posted = %+v, want one payload with surface cli", posted)
	}
}

func flushCollecting(t *testing.T, home string) []metricsPayload {
	t.Helper()
	var mu sync.Mutex
	var posted []metricsPayload
	client := testClient(home, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != endpoint+"/metrics" {
			t.Errorf("request URL = %q", req.URL)
		}
		var payload metricsPayload
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		posted = append(posted, payload)
		mu.Unlock()
		return telemetryResponse(http.StatusAccepted), nil
	}))
	if err := client.flushPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	return posted
}

// The allow-list is the silent one. A counter the sink records but uploadSignals
// omits is written to the queue and dropped without a word, which from every
// local test looks exactly like a feature that works. This drives a turn through
// each branch that records and asserts the wire accepts everything that came out
// — so a new signal fails here rather than in production telemetry nobody reads.
func TestEverySignalTheSinkRecordsSurvivesTheUploadAllowList(t *testing.T) {
	home := testenv.TempDir(t)
	reporter := &Reporter{home: home, version: "v1.20.0", surface: surface.CLI, static: []Counter{
		{Signal: "client_surface", Bucket: "cli", Count: 1},
		{Signal: "client_version", Bucket: "v1_20_0", Count: 1},
		{Signal: "cli_mode", Bucket: "run", Count: 1},
		{Signal: "cli_profile", Bucket: "balanced", Count: 1},
		{Signal: "cli_permission_mode", Bucket: "ask", Count: 1},
		{Signal: "cli_session_mode", Bucket: "fresh", Count: 1},
		{Signal: "settings_language", Bucket: "en", Count: 1},
	}}
	sink := reporter.Wrap(&readinessSink{})
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		FinishReason: "stop", PromptTokens: 90_000, CompletionTokens: 800, CacheHitTokens: 80_000, CacheMissTokens: 10_000,
	}, CacheDiagnostics: &event.CacheDiagnostics{
		PrefixChanged: true, PrefixChangeReasons: []string{"system", "tools", "compact"},
	}})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Err: "permission denied"}})
	sink.Emit(event.Event{Kind: event.Notice, Code: event.NoticeCodeEmptyFinal})
	sink.Emit(event.Event{Kind: event.CompactionStarted, Compaction: event.Compaction{Trigger: "auto"}})
	event.RecordProtocolRecovery(sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryReplaced})
	sink.Emit(event.Event{Kind: event.TurnDone})
	reporter.RecordRecovery(hostaudit.RecoveryMetrics{FailureEvents: 1, ReviewLatencyMsSum: 400, ReviewLatencyCount: 1})

	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) == 0 {
		t.Fatalf("pending files = %d, err = %v", len(entries), err)
	}
	recorded, dropped := 0, []string{}
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(home, pendingDirName, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var payload pendingPayload
		if err := json.Unmarshal(b, &payload); err != nil {
			t.Fatal(err)
		}
		for _, counter := range payload.Counters {
			recorded++
			if !validCounter(counter) {
				dropped = append(dropped, counter.Signal+"/"+counter.Bucket)
			}
		}
	}
	if len(dropped) > 0 {
		t.Fatalf("the sink records %d counters and the wire drops %d of them: %v", recorded, len(dropped), dropped)
	}
	if recorded < 12 {
		t.Fatalf("only %d counters recorded — the stream stopped exercising the branches this guards", recorded)
	}
}
