package main

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
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
