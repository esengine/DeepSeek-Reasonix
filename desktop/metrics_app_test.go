package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/recovery"
)

type recoveryMetricsDeltaStub struct {
	deltas []recovery.Metrics
}

func (s *recoveryMetricsDeltaStub) DrainRecoveryMetrics() recovery.Metrics {
	if len(s.deltas) == 0 {
		return recovery.Metrics{}
	}
	next := s.deltas[0]
	s.deltas = s.deltas[1:]
	return next
}

func TestObserveClassifiesEvents(t *testing.T) {
	m := newMetricsAggregator(t.TempDir())
	feed := []event.Event{
		{Kind: event.Usage, Usage: &provider.Usage{FinishReason: "stop", CacheHitTokens: 99, CacheMissTokens: 1}},
		{Kind: event.Usage, Usage: &provider.Usage{FinishReason: "tool_calls", CacheHitTokens: 60, CacheMissTokens: 40}},
		{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Err: "blocked by permission policy"}},
		{Kind: event.CompactionDone},
		{Kind: event.Notice, Text: "No visible answer was produced; asking the assistant to respond again.", Detail: "empty final answer blocked: model returned no visible answer text; retrying"},
		{Kind: event.TurnDone, Err: errors.New("deepseek-flash: status 429: rate limited")},
		{Kind: event.TurnDone, Err: errors.New("automatic recovery paused"), Outcome: event.TurnOutcomeRecoveryPaused},
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
		"turns":          {"total": 3},
	}
	for sig, buckets := range want {
		for b, n := range buckets {
			if got := m.c[sig][b]; got != n {
				t.Errorf("%s/%s = %d, want %d", sig, b, got, n)
			}
		}
	}
}

func TestObserveControllerRecoveryMetricsConsumesOnlyNewDelta(t *testing.T) {
	m := newMetricsAggregator(t.TempDir())
	ctrl := &recoveryMetricsDeltaStub{deltas: []recovery.Metrics{
		{FailureEvents: 1, HumanPrompts: 1, ReviewLatencyMsSum: 750, ReviewLatencyCount: 1},
		{},
	}}

	observeControllerRecoveryMetrics(m, ctrl)
	observeControllerRecoveryMetrics(m, ctrl)

	if got := m.c["recovery_failure"]["total"]; got != 1 {
		t.Fatalf("recovery_failure/total = %d, want 1", got)
	}
	if got := m.c["recovery_human_prompt"]["total"]; got != 1 {
		t.Fatalf("recovery_human_prompt/total = %d, want 1", got)
	}
	if got := m.c["recovery_review_latency"]["lt_2s"]; got != 1 {
		t.Fatalf("recovery_review_latency/lt_2s = %d, want 1", got)
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

func TestObserveSettingsSnapshotUsesSafeBuckets(t *testing.T) {
	cfg := config.Default()
	if err := cfg.SetDesktopLanguage(""); err != nil {
		t.Fatalf("SetDesktopLanguage: %v", err)
	}
	if err := cfg.SetDesktopLayoutStyle("workbench"); err != nil {
		t.Fatalf("SetDesktopLayoutStyle: %v", err)
	}
	if err := cfg.SetDesktopAppearance("dark", "graphite"); err != nil {
		t.Fatalf("SetDesktopAppearance: %v", err)
	}
	if err := cfg.SetDesktopCloseBehavior("quit"); err != nil {
		t.Fatalf("SetDesktopCloseBehavior: %v", err)
	}
	if err := cfg.SetDesktopDisplayMode("compact"); err != nil {
		t.Fatalf("SetDesktopDisplayMode: %v", err)
	}
	if err := cfg.SetDesktopStatusBarStyle("icon"); err != nil {
		t.Fatalf("SetDesktopStatusBarStyle: %v", err)
	}
	if err := cfg.SetDesktopStatusBarItems([]string{"model", "cache", "balance"}); err != nil {
		t.Fatalf("SetDesktopStatusBarItems: %v", err)
	}
	if err := cfg.SetDesktopCheckUpdates(false); err != nil {
		t.Fatalf("SetDesktopCheckUpdates: %v", err)
	}
	customProvider := "Local OpenAI"
	customModel := "Qwen-72B-Instruct.private"
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{
		Name:    customProvider,
		Kind:    "openai",
		BaseURL: "http://127.0.0.1:9999/v1",
		Models:  []string{customModel},
		Default: customModel,
	})
	cfg.Agent.PlannerModel = customProvider + "/" + customModel
	cfg.Desktop.ProviderAccess = []string{customProvider}
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		Provider: "feishu",
		Enabled:  true,
		Status:   "connected",
		Model:    customProvider + "/" + customModel,
	}}

	m := newMetricsAggregator(t.TempDir())
	m.observeSettingsSnapshot(cfg)

	want := map[string]string{
		"settings_language":                "auto",
		"client_surface":                   "desktop",
		"client_version":                   metricBucket(version),
		"settings_desktop_layout":          "workbench",
		"settings_theme":                   "dark",
		"settings_theme_style":             "graphite",
		"settings_close_behavior":          "quit",
		"settings_display_mode":            "compact",
		"settings_status_bar_style":        "icon",
		"settings_status_bar_items_count":  "n_3",
		"settings_check_updates":           "off",
		"settings_default_model":           "deepseek_deepseek_v4_flash",
		"settings_planner_model":           metricBucket("custom_" + customProvider + "_" + customModel),
		"settings_provider_access":         metricBucket("custom_" + customProvider),
		"settings_bot_enabled":             "off",
		"settings_bot_connection_count":    "n_1",
		"settings_bot_connection_provider": "feishu",
		"settings_bot_connection_enabled":  "on",
		"settings_bot_connection_status":   "connected",
		"settings_bot_connection_model":    metricBucket("custom_" + customProvider + "_" + customModel),
	}
	for signal, bucket := range want {
		if got := m.c[signal][bucket]; got != 1 {
			t.Errorf("%s/%s = %d, want 1", signal, bucket, got)
		}
	}
}

func TestObserveSettingsSnapshotCountsDisabledPlannerAsOff(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.PlannerModel = ""

	m := newMetricsAggregator(t.TempDir())
	m.observeSettingsSnapshot(cfg)

	if got := m.c["settings_planner_model"]["off"]; got != 1 {
		t.Fatalf("settings_planner_model/off = %d, want 1", got)
	}
	if got := m.c["settings_planner_model"][safeModelBucket(cfg, cfg.DefaultModel)]; got != 0 {
		t.Fatalf("disabled planner should not count the default model, got %d", got)
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
		"update: authorization cancelled":     "authorization_cancelled",
		"update: authorization failed":        "authorization_failed",
		"update: package manager busy":        "package_manager_busy",
		"update: package install failed":      "package_install_failed",
		"update: package verify failed":       "package_verify_failed",
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

func TestRecordDroppedWebRuntimeEventsDrainsAtomicCount(t *testing.T) {
	oldVersion := version
	version = "v1.23.0"
	t.Cleanup(func() { version = oldVersion })

	dir := t.TempDir()
	app := NewApp()
	app.metrics.Store(newMetricsAggregator(dir))
	var dropped atomic.Uint64
	dropped.Store(3)

	recordDroppedWebRuntimeEvents(app, "webview2", &dropped)
	recordDroppedWebRuntimeEvents(app, "webview2", &dropped)

	if got := dropped.Load(); got != 0 {
		t.Fatalf("dropped count = %d, want drained", got)
	}
	if got := readCounters(filepath.Join(dir, metricsPendingFile))["desktop_web_runtime_dropped"]["webview2"]; got != 3 {
		t.Fatalf("desktop_web_runtime_dropped/webview2 = %d, want 3", got)
	}
}

func TestErrorClassSeparatesBadRequestCauses(t *testing.T) {
	// Verbatim provider bodies from reported issues, each needing a different fix.
	cases := map[string]string{
		`status 400: messages[203]: unknown variant ` + "`image_url`" + `, expected ` + "`text`": "http_400_content",
		`status 400: Invalid schema for function 'ls': null is not of type "array"`:              "http_400_schema",
		"status 400: The `content[].thinking` in the thinking mode must be passed back":          "http_400_reasoning_replay",
		"status 400: thinking: invalid type: map, expected a boolean":                            "http_400_thinking_shape",
		"status 400: This model's maximum context length is 65536 tokens":                        "http_400_context_length",
		"status 400: missing field name at line 1":                                               "http_400_tool_calls",
		"status 400: something nobody has classified yet":                                        "http_400",
	}
	for msg, want := range cases {
		if got := errorClass(msg); got != want {
			t.Errorf("errorClass(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestErrorClassNeverEchoesProviderText(t *testing.T) {
	// The bucket is uploaded; the message is not. A label must be a fixed token,
	// so no substring of a body carrying user code or prompt text can reach it.
	secrets := []string{
		"status 400: Invalid schema for function 'ls': /home/alice/secret-project is not of type \"array\"",
		"status 400: unknown variant `image_url` in ~/work/client-contract.pdf",
		"status 400: thinking: invalid type: map, expected a boolean, key=sk-abc123",
	}
	for _, msg := range secrets {
		got := errorClass(msg)
		for _, leak := range []string{"alice", "secret", "client-contract", "sk-abc123", "/home", "~/work"} {
			if strings.Contains(got, leak) {
				t.Fatalf("errorClass(%q) = %q, leaked %q", msg, got, leak)
			}
		}
		if !strings.HasPrefix(got, "http_400") {
			t.Errorf("errorClass(%q) = %q, want an http_400 label", msg, got)
		}
	}
}
