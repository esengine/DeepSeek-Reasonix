package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

// metrics_app.go is the opt-in aggregate agent-metrics flush: anonymous
// (signal, bucket) counters observed from the event stream, POSTed once per
// launch. Never carries content, keys, prompts, or paths — only enumerated
// integer counts. Gated on config desktop.metrics (default off), dev-skipped.

var metricsEndpoint = "https://crash.reasonix.io/v1/metrics"

const metricsPendingFile = "metrics-pending.json"

var statusCodePattern = regexp.MustCompile(`status (\d{3})`)

type counters map[string]map[string]int // signal -> bucket -> count

func (c counters) add(signal, bucket string, n int) {
	if c[signal] == nil {
		c[signal] = map[string]int{}
	}
	c[signal][bucket] += n
}

func (c counters) merge(other counters) {
	for sig, buckets := range other {
		for b, n := range buckets {
			c.add(sig, b, n)
		}
	}
}

// metricsAggregator accumulates one session's (signal, bucket) counts and merges
// them into a pending file that flushMetrics drains on the next launch.
type metricsAggregator struct {
	path string
	mu   sync.Mutex
	c    counters
}

func newMetricsAggregator(configDir string) *metricsAggregator {
	return &metricsAggregator{path: filepath.Join(configDir, metricsPendingFile), c: counters{}}
}

func (m *metricsAggregator) inc(signal, bucket string) {
	m.mu.Lock()
	m.c.add(signal, bucket, 1)
	m.mu.Unlock()
}

// observe maps one event to counter increments, reading only enumerated facts
// (finish reason, error class, cache-hit bucket) — never message text.
func (m *metricsAggregator) observe(e event.Event) {
	switch e.Kind {
	case event.Usage:
		if e.Usage == nil {
			return
		}
		if e.Usage.FinishReason != "" {
			m.inc("finish_reason", e.Usage.FinishReason)
		}
		if e.Usage.CacheHitTokens+e.Usage.CacheMissTokens > 0 {
			m.inc("cache_hit", cacheBucket(e.Usage.CacheHitTokens, e.Usage.CacheMissTokens))
		}
	case event.TurnDone:
		m.inc("turns", "total")
		if e.Err != nil {
			m.inc("provider_error", errorClass(e.Err.Error()))
		}
	case event.ToolResult:
		if e.Tool.Err != "" {
			m.inc("tool_error", toolErrorClass(e.Tool.Err))
		}
	case event.CompactionDone:
		m.inc("compaction", "total")
	case event.Notice:
		if strings.HasPrefix(e.Text, "empty final answer blocked") {
			m.inc("empty_final", "total")
		}
	}
}

func cacheBucket(hit, miss int) string {
	pct := float64(hit) / float64(hit+miss) * 100
	switch {
	case pct < 50:
		return "0_50"
	case pct < 80:
		return "50_80"
	case pct < 95:
		return "80_95"
	case pct < 99:
		return "95_99"
	default:
		return "99_100"
	}
}

// errorClass extracts only the failure category — never the message itself, which
// can echo request content back from a provider.
func errorClass(msg string) string {
	if mm := statusCodePattern.FindStringSubmatch(msg); mm != nil {
		switch code := mm[1]; {
		case code == "400":
			return "http_400"
		case code == "401" || code == "403":
			return "http_401"
		case code == "429":
			return "http_429"
		case code[0] == '5':
			return "http_5xx"
		}
	}
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "reset"), strings.Contains(low, "interrupt"), strings.Contains(low, "eof"):
		return "stream_interrupted"
	case strings.Contains(low, "timeout"), strings.Contains(low, "deadline"):
		return "timeout"
	default:
		return "other"
	}
}

func toolErrorClass(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "permission"):
		return "permission"
	case strings.Contains(low, "plan mode"):
		return "planmode"
	case strings.Contains(low, "hook"):
		return "hook"
	case strings.Contains(low, "timeout"), strings.Contains(low, "deadline"):
		return "timeout"
	default:
		return "exec"
	}
}

// persist merges the session delta into the pending file and resets it, so a
// force-kill loses at most the counts since the last turn.
func (m *metricsAggregator) persist() {
	m.mu.Lock()
	if len(m.c) == 0 {
		m.mu.Unlock()
		return
	}
	delta := m.c
	m.c = counters{}
	m.mu.Unlock()

	pending := readCounters(m.path)
	pending.merge(delta)
	writeCounters(m.path, pending)
}

func readCounters(path string) counters {
	b, err := os.ReadFile(path)
	if err != nil {
		return counters{}
	}
	var c counters
	if json.Unmarshal(b, &c) != nil || c == nil {
		return counters{}
	}
	return c
}

func writeCounters(path string, c counters) {
	if b, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

type metricCounter struct {
	Signal string `json:"signal"`
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

type metricsPayload struct {
	Version  string          `json:"version"`
	OS       string          `json:"os"`
	Counters []metricCounter `json:"counters"`
}

func flatten(c counters) []metricCounter {
	out := make([]metricCounter, 0, len(c))
	for sig, buckets := range c {
		for b, n := range buckets {
			if n > 0 {
				out = append(out, metricCounter{Signal: sig, Bucket: b, Count: n})
			}
		}
	}
	return out
}

// flushMetrics drains the pending file from prior sessions and POSTs it, then
// clears it on success or folds it back to retry next launch. Runs at launch
// (mirroring the ping) so the current session's counts ship next time.
func (a *App) flushMetrics() {
	if version == "dev" {
		return
	}
	cfg, err := config.Load()
	if err != nil || !cfg.DesktopMetrics() {
		return
	}
	path := filepath.Join(filepath.Dir(config.UserConfigPath()), metricsPendingFile)
	temp := path + ".sending"
	if os.Rename(path, temp) != nil {
		return // nothing pending
	}
	flat := flatten(readCounters(temp))
	if len(flat) == 0 || a.postMetrics(metricsPayload{Version: version, OS: runtime.GOOS, Counters: flat}) {
		_ = os.Remove(temp)
		return
	}
	pending := readCounters(path)
	pending.merge(readCounters(temp))
	writeCounters(path, pending)
	_ = os.Remove(temp)
}

func (a *App) postMetrics(p metricsPayload) bool {
	body, err := json.Marshal(p)
	if err != nil {
		return false
	}
	c, err := httpClient()
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(a.bootContext(), http.MethodPost, metricsEndpoint, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 300
}
