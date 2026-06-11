package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestObserveClassifiesEvents(t *testing.T) {
	m := newMetricsAggregator(t.TempDir())
	feed := []event.Event{
		{Kind: event.Usage, Usage: &provider.Usage{FinishReason: "stop", CacheHitTokens: 99, CacheMissTokens: 1}},
		{Kind: event.Usage, Usage: &provider.Usage{FinishReason: "tool_calls", CacheHitTokens: 60, CacheMissTokens: 40}},
		{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Err: "blocked by permission policy"}},
		{Kind: event.CompactionDone},
		{Kind: event.Notice, Text: "empty final answer blocked: model returned no visible answer text; retrying"},
		{Kind: event.TurnDone, Err: errors.New("deepseek-flash: status 429: rate limited")},
		{Kind: event.TurnDone},
	}
	for _, e := range feed {
		m.observe(e)
	}

	want := map[string]map[string]int{
		"finish_reason":  {"stop": 1, "tool_calls": 1},
		"cache_hit":      {"99_100": 1, "50_80": 1},
		"tool_error":     {"permission": 1},
		"compaction":     {"total": 1},
		"empty_final":    {"total": 1},
		"provider_error": {"http_429": 1},
		"turns":          {"total": 2},
	}
	for sig, buckets := range want {
		for b, n := range buckets {
			if got := m.c[sig][b]; got != n {
				t.Errorf("%s/%s = %d, want %d", sig, b, got, n)
			}
		}
	}
}

func TestObserveReadsNoMessageText(t *testing.T) {
	m := newMetricsAggregator(t.TempDir())
	// A notice that merely mentions the phrase mid-string must not count.
	m.observe(event.Event{Kind: event.Notice, Text: "see docs: empty final answer blocked is a guard"})
	if m.c["empty_final"] != nil {
		t.Errorf("empty_final should only match the notice prefix, got %v", m.c["empty_final"])
	}
}

func TestErrorClass(t *testing.T) {
	cases := map[string]string{
		"deepseek: status 400: bad":           "http_400",
		"status 401 unauthorized":             "http_401",
		"status 403 forbidden":                "http_401",
		"status 429 too many":                 "http_429",
		"status 503 unavailable":              "http_5xx",
		"read: connection reset by peer":      "stream_interrupted",
		"stream interrupted mid-flight":       "stream_interrupted",
		"context deadline exceeded (timeout)": "timeout",
		"some unrecognized failure":           "other",
	}
	for msg, want := range cases {
		if got := errorClass(msg); got != want {
			t.Errorf("errorClass(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestCacheBucket(t *testing.T) {
	cases := []struct {
		hit, miss int
		want      string
	}{
		{0, 100, "0_50"},
		{49, 51, "0_50"},
		{60, 40, "50_80"},
		{90, 10, "80_95"},
		{97, 3, "95_99"},
		{999, 1, "99_100"},
	}
	for _, c := range cases {
		if got := cacheBucket(c.hit, c.miss); got != c.want {
			t.Errorf("cacheBucket(%d,%d) = %q, want %q", c.hit, c.miss, got, c.want)
		}
	}
}

func TestPersistMergesAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, metricsPendingFile)

	s1 := newMetricsAggregator(dir)
	s1.observe(event.Event{Kind: event.TurnDone})
	s1.persist()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pending file should exist after persist: %v", err)
	}

	// A second session merges into the same file rather than overwriting.
	s2 := newMetricsAggregator(dir)
	s2.observe(event.Event{Kind: event.TurnDone})
	s2.persist()

	if got := readCounters(path)["turns"]["total"]; got != 2 {
		t.Errorf("merged turns/total = %d, want 2", got)
	}
	if n := len(flatten(readCounters(path))); n != 1 {
		t.Errorf("flatten produced %d counters, want 1", n)
	}
}
