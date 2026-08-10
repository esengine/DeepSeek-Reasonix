package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func meterAgainst(t *testing.T, upstream http.Handler, faults faultScript) (*meter, string, func()) {
	t.Helper()
	up := httptest.NewServer(upstream)
	m, err := newMeter(up.URL, faults)
	if err != nil {
		t.Fatalf("newMeter: %v", err)
	}
	base, stop, err := m.serve()
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	return m, base, func() { stop(); up.Close() }
}

func post(t *testing.T, base, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(base+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestMeterCountsNonStreamingUsage(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_cache_hit_tokens":64,"prompt_cache_miss_tokens":36}}`)
	})
	m, base, stop := meterAgainst(t, upstream, faultScript{})
	defer stop()

	resp := post(t, base, "/chat/completions", `{"model":"x"}`)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Contains(body, []byte(`"content":"hi"`)) {
		t.Fatalf("response not forwarded: %s", body)
	}
	got := m.snapshot()
	if got.Requests != 1 || got.PromptTokens != 100 || got.CompletionTokens != 20 {
		t.Fatalf("usage = %+v", got)
	}
	if got.CacheHitTokens != 64 || got.CacheMissTokens != 36 {
		t.Fatalf("cache split = %d/%d, want 64/36", got.CacheHitTokens, got.CacheMissTokens)
	}
	if got.WithoutUsage != 0 {
		t.Fatalf("usage was present; WithoutUsage = %d", got.WithoutUsage)
	}
}

func TestMeterReadsOpenAICachedTokensSpelling(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"usage":{"prompt_tokens":90,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":30}}}`)
	})
	m, base, stop := meterAgainst(t, upstream, faultScript{})
	defer stop()
	post(t, base, "/chat/completions", `{"model":"x"}`).Body.Close()

	got := m.snapshot()
	if got.CacheHitTokens != 30 || got.CacheMissTokens != 60 {
		t.Fatalf("cache split = %d/%d, want 30/60 derived from prompt_tokens", got.CacheHitTokens, got.CacheMissTokens)
	}
}

func TestMeterCountsStreamedUsageAndForwardsFrames(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	m, base, stop := meterAgainst(t, upstream, faultScript{})
	defer stop()

	resp := post(t, base, "/chat/completions", `{"model":"x","stream":true}`)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "[DONE]") || !strings.Contains(string(body), `"content":"a"`) {
		t.Fatalf("frames not forwarded verbatim: %q", body)
	}
	got := m.snapshot()
	if got.PromptTokens != 7 || got.CompletionTokens != 3 || got.WithoutUsage != 0 {
		t.Fatalf("streamed usage = %+v", got)
	}
}

// A harness that never asks for usage would otherwise measure as free.
func TestMeterOptsStreamedRequestsIntoUsage(t *testing.T) {
	var seen []byte
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	_, base, stop := meterAgainst(t, upstream, faultScript{})
	defer stop()
	post(t, base, "/chat/completions", `{"model":"x","stream":true}`).Body.Close()

	var payload map[string]any
	if err := json.Unmarshal(seen, &payload); err != nil {
		t.Fatalf("upstream body: %v", err)
	}
	opts, ok := payload["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Fatalf("stream_options not injected: %s", seen)
	}
}

func TestMeterLeavesNonStreamedRequestsAlone(t *testing.T) {
	var seen []byte
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	_, base, stop := meterAgainst(t, upstream, faultScript{})
	defer stop()
	post(t, base, "/chat/completions", `{"model":"x"}`).Body.Close()

	if strings.Contains(string(seen), "stream_options") {
		t.Fatalf("non-streamed request was rewritten: %s", seen)
	}
}

func TestMeterReportsResponsesWithoutUsage(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[]}`)
	})
	m, base, stop := meterAgainst(t, upstream, faultScript{})
	defer stop()
	post(t, base, "/chat/completions", `{"model":"x"}`).Body.Close()

	if got := m.snapshot(); got.WithoutUsage != 1 || got.PromptTokens != 0 {
		t.Fatalf("unmeasured response must be reported, not zeroed: %+v", got)
	}
}

func TestMeterInjectsFaultsByRequestIndex(t *testing.T) {
	reached := 0
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	m, base, stop := meterAgainst(t, upstream, faultScript{at: map[int]int{2: 429}})
	defer stop()

	for i := range 3 {
		resp := post(t, base, "/chat/completions", `{"model":"x"}`)
		want := http.StatusOK
		if i == 1 {
			want = http.StatusTooManyRequests
		}
		if resp.StatusCode != want {
			t.Fatalf("request %d status = %d, want %d", i+1, resp.StatusCode, want)
		}
		resp.Body.Close()
	}
	if reached != 2 {
		t.Fatalf("upstream saw %d requests, want 2 — the faulted one must not be forwarded", reached)
	}
	if got := m.snapshot(); got.Injected != 1 || got.Requests != 3 {
		t.Fatalf("meter = %+v, want 3 requests with 1 injected", got)
	}
}

func TestParseFaultScript(t *testing.T) {
	got, err := parseFaultScript(" 3:429 , 7:500 ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.at[3] != 429 || got.at[7] != 500 || len(got.at) != 2 {
		t.Fatalf("faults = %v", got)
	}
	if got, err := parseFaultScript(""); err != nil || !got.empty() {
		t.Fatalf("empty spec = %v, %v", got, err)
	}
	for _, bad := range []string{"3", "0:429", "3:200", "x:429", "3:999"} {
		if _, err := parseFaultScript(bad); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestNewMeterRejectsRelativeUpstream(t *testing.T) {
	if _, err := newMeter("/v1", faultScript{}); err == nil {
		t.Fatal("a relative upstream must be rejected")
	}
}

// okUpstream is a minimal usage-reporting upstream for fault tests.
func okUpstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
}
