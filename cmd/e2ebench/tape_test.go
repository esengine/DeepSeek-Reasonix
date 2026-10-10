package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const tapedStream = "data: {\"choices\":[{\"delta\":{\"content\":\"fixed\"}}]}\n\n" +
	"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":5,\"prompt_cache_hit_tokens\":40,\"prompt_cache_miss_tokens\":10}}\n\n" +
	"data: [DONE]\n\n"

const tapedRequest = `{"model":"x","stream":true,"messages":[{"role":"system","content":"prefix"},{"role":"user","content":"fix slug.py"}]}`

func tapedMeter(t *testing.T, mode tapeMode, root string, upstreamCalls *atomic.Int32) (*meter, string, func()) {
	t.Helper()
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, tapedStream)
	})
	m, base, stop := meterAgainst(t, upstream, faultScript{})
	tp, err := tapeConfig{mode: mode, root: root}.forRun("bugfix-slug", 1)
	if err != nil {
		t.Fatal(err)
	}
	m.tape = tp
	return m, base, stop
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// A run replayed from its tape gets the recorded stream byte for byte, never
// reaches the provider, and is metered as the recorded run was.
func TestTapeReplaysARecordedRunWithoutTheProvider(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	_, base, stop := tapedMeter(t, tapeRecord, root, &calls)
	live := readAll(t, post(t, base, "/chat/completions", tapedRequest))
	stop()
	if calls.Load() != 1 {
		t.Fatalf("recording made %d upstream calls, want 1", calls.Load())
	}

	m, base, stop := tapedMeter(t, tapeReplay, root, &calls)
	defer stop()
	replayed := readAll(t, post(t, base, "/chat/completions", tapedRequest))
	if replayed != live || !strings.Contains(replayed, "fixed") {
		t.Fatalf("replayed stream differs:\nlive=%q\nreplayed=%q", live, replayed)
	}
	if calls.Load() != 1 {
		t.Fatalf("replay reached the provider: %d upstream calls", calls.Load())
	}
	got := m.snapshot()
	if got.Replayed != 1 || got.DivergedAt != 0 || got.PromptTokens != 50 || got.CacheHitTokens != 40 {
		t.Fatalf("replay usage = %+v", got)
	}
}

// A kernel change that alters what the model is asked shows as the first
// request whose body departs from the tape, named down to the message.
func TestTapeReplayNamesTheFirstRequestThatDiffers(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	_, base, stop := tapedMeter(t, tapeRecord, root, &calls)
	readAll(t, post(t, base, "/chat/completions", tapedRequest))
	stop()

	m, base, stop := tapedMeter(t, tapeReplay, root, &calls)
	defer stop()
	changed := strings.Replace(tapedRequest, "fix slug.py", "fix slug.py please", 1)
	readAll(t, post(t, base, "/chat/completions", changed))
	resp := post(t, base, "/chat/completions", tapedRequest)
	readAll(t, resp)
	got := m.snapshot()
	if got.DivergedAt != 1 || got.Divergence != "message 2 (user)" {
		t.Fatalf("divergence = %d %q, want request 1, message 2 (user)", got.DivergedAt, got.Divergence)
	}
	if resp.StatusCode != http.StatusBadGateway || got.TapeMissing != 1 {
		t.Fatalf("a request past the tape = %d, missing %d; want 502 and counted", resp.StatusCode, got.TapeMissing)
	}
}

func TestRequestDiffReadsStructureBeforeBytes(t *testing.T) {
	cases := map[string][2]string{
		"":                     {`{"a":1}`, `{"a":1}`},
		"message count 1 -> 2": {`{"messages":[{"role":"user"}]}`, `{"messages":[{"role":"user"},{"role":"assistant"}]}`},
		"tools":                {`{"messages":[],"tools":[1]}`, `{"messages":[],"tools":[2]}`},
		"field temperature":    {`{"messages":[],"temperature":0}`, `{"messages":[],"temperature":1}`},
		"message 1 (system)":   {`{"messages":[{"role":"system","content":"a"}]}`, `{"messages":[{"role":"system","content":"b"}]}`},
		"request body":         {`not json`, `{"a":1}`},
	}
	for want, pair := range cases {
		if got := requestDiff([]byte(pair[0]), []byte(pair[1])); got != want {
			t.Errorf("requestDiff(%s, %s) = %q, want %q", pair[0], pair[1], got, want)
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A recording whose request file cannot be written is reported rather than
// treated as a tape: request.json is what marks the exchange complete.
func TestTapeSaveFailureIsReportedOnTheMeter(t *testing.T) {
	root := t.TempDir()
	orig := tapeWriteFile
	tapeWriteFile = func(path string, body []byte, mode os.FileMode) error {
		if strings.HasSuffix(path, "request.json") {
			return errors.New("disk full")
		}
		return orig(path, body, mode)
	}
	t.Cleanup(func() { tapeWriteFile = orig })

	var calls atomic.Int32
	m, base, stop := tapedMeter(t, tapeRecord, root, &calls)
	defer stop()
	readAll(t, post(t, base, "/chat/completions", tapedRequest))

	var got meterUsage
	waitFor(t, "the failed write to be reported", func() bool {
		got = m.snapshot()
		return got.TapeIncomplete > 0
	})
	if !strings.Contains(got.TapeError, "disk full") {
		t.Fatalf("incomplete recording = %+v, want the write error named", got)
	}
	if markers, _ := filepath.Glob(filepath.Join(root, "bugfix-slug", "trial-1", "*.request.json")); len(markers) != 0 {
		t.Fatalf("the completeness marker was written although the request write failed: %v", markers)
	}
	line := renderTapeRecord([]result{{task: task{ID: "bugfix-slug"}, Meter: &got}})
	if !strings.Contains(line, "Tape record incomplete") || !strings.Contains(line, "bugfix-slug") {
		t.Fatalf("report line = %q", line)
	}
	if renderTapeRecord(nil) != "" {
		t.Fatal("an empty run set produced a tape line")
	}
}

// A tape with no recorded request is incomplete, not a divergence: there is
// nothing to compare against, and blaming the harness would be wrong.
func TestTapeMissingRequestIsIncompleteNotADivergence(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	_, base, stop := tapedMeter(t, tapeRecord, root, &calls)
	readAll(t, post(t, base, "/chat/completions", tapedRequest))
	dir := filepath.Join(root, "bugfix-slug", "trial-1")
	var markers []string
	waitFor(t, "the recorded request", func() bool {
		markers, _ = filepath.Glob(filepath.Join(dir, "*.request.json"))
		return len(markers) > 0
	})
	stop()
	if err := os.Remove(markers[0]); err != nil {
		t.Fatal(err)
	}

	m, base, stop := tapedMeter(t, tapeReplay, root, &calls)
	defer stop()
	resp := post(t, base, "/chat/completions", tapedRequest)
	readAll(t, resp)
	got := m.snapshot()
	if resp.StatusCode != http.StatusBadGateway || got.TapeMissing != 1 {
		t.Fatalf("incomplete tape = %d, missing %d; want 502 and counted", resp.StatusCode, got.TapeMissing)
	}
	if got.DivergedAt != 0 || got.Divergence != "" {
		t.Fatalf("an incomplete tape was reported as a divergence: %d %q", got.DivergedAt, got.Divergence)
	}
}

// The run path snapshots the meter through record, which must stop the meter
// first: stopping is what waits for the handlers whose deferred writes carry the
// tape counters. Snapshotting before that misses the last exchange.
func TestMeterRecordDrainsHandlersBeforeSnapshot(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	m, base, stop := tapedMeter(t, tapeRecord, root, &calls)
	defer stop()
	readAll(t, post(t, base, "/chat/completions", tapedRequest))

	var r result
	runMeter{m: m, stop: stop}.record(&r)
	// No waitFor: the drain inside record is what makes this observable.
	if r.Meter == nil {
		t.Fatal("record attached no meter")
	}
	if resp, err := http.Post(base+"/chat/completions", "application/json", strings.NewReader(tapedRequest)); err == nil {
		resp.Body.Close()
		t.Fatal("the meter was still serving after record, so its handlers were not drained")
	}
}

// Re-recording over an existing tape must not let the previous run's
// completeness marker make a partial new exchange look complete.
func TestTapeReRecordClearsStaleCompletenessMarkers(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	_, base, stop := tapedMeter(t, tapeRecord, root, &calls)
	readAll(t, post(t, base, "/chat/completions", tapedRequest))
	dir := filepath.Join(root, "bugfix-slug", "trial-1")
	waitFor(t, "the first recording", func() bool {
		m, _ := filepath.Glob(filepath.Join(dir, "*.request.json"))
		return len(m) > 0
	})
	stop()

	// The second recording fails while writing the body, after the request has
	// already been replaced in memory.
	orig := tapeWriteFile
	tapeWriteFile = func(path string, body []byte, mode os.FileMode) error {
		if strings.HasSuffix(path, "response.body") {
			return errors.New("disk full")
		}
		return orig(path, body, mode)
	}
	t.Cleanup(func() { tapeWriteFile = orig })

	var calls2 atomic.Int32
	m2, base2, stop2 := tapedMeter(t, tapeRecord, root, &calls2)
	defer stop2()
	readAll(t, post(t, base2, "/chat/completions", tapedRequest))
	var r result
	runMeter{m: m2, stop: stop2}.record(&r)
	if markers, _ := filepath.Glob(filepath.Join(dir, "*.request.json")); len(markers) != 0 {
		t.Fatalf("a failed re-record left completeness markers: %v", markers)
	}
	// Replay must call the exchange incomplete, not compare a stale request with
	// a partial response.
	m3, base3, stop3 := tapedMeter(t, tapeReplay, root, &calls2)
	defer stop3()
	resp := post(t, base3, "/chat/completions", tapedRequest)
	readAll(t, resp)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("replay of a partial tape = %d, want 502", resp.StatusCode)
	}
	if got := m3.snapshot(); got.DivergedAt != 0 {
		t.Fatalf("a partial tape was reported as a divergence: %d %q", got.DivergedAt, got.Divergence)
	}
}
