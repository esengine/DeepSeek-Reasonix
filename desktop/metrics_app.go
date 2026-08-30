package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/recovery"
	"reasonix/internal/turnevent"
)

// metrics_app.go is the aggregate desktop-metrics flush: anonymous (signal,
// bucket) counters observed from the event stream and safe desktop preference
// snapshots, POSTed once per launch. Never carries content, keys, prompts, paths,
// or base URLs; custom provider/model identifiers are normalized into bounded
// buckets. Gated on config desktop.metrics (default on), dev-skipped.

var metricsEndpoint = "https://crash.reasonix.io/v1/metrics"

const metricsPendingFile = "metrics-pending.json"
const metricsPostTimeout = 8 * time.Second

var statusCodePattern = regexp.MustCompile(`status (\d{3})`)
var metricsPendingMu sync.Mutex

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
	m.add(signal, bucket, 1)
}

func (m *metricsAggregator) add(signal, bucket string, n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	m.c.add(signal, bucket, n)
	m.mu.Unlock()
}

func boolBucket(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func statusBarItemsCountBucket(n int) string {
	if n < 0 {
		n = 0
	}
	return "n_" + strconv.Itoa(n)
}

func countBucket(n int) string {
	if n < 0 {
		n = 0
	}
	switch {
	case n == 0:
		return "n_0"
	case n == 1:
		return "n_1"
	case n <= 3:
		return "n_2_3"
	case n <= 5:
		return "n_4_5"
	default:
		return "n_6_plus"
	}
}

func knownBucket(value string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if slices.Contains(allowed, value) {
		return value
	}
	return "other"
}

func knownBucketDefault(value, def string, allowed ...string) string {
	if strings.TrimSpace(value) == "" {
		value = def
	}
	return knownBucket(value, allowed...)
}

func metricBucket(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "other"
	}
	if len(out) > 96 {
		return out[:96]
	}
	return out
}

func metricsOfficialProviderHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func officialProviderBucket(e *config.ProviderEntry) string {
	if e == nil {
		return ""
	}
	switch config.CanonicalDesktopOfficialProviderName(e.Name) {
	case "deepseek":
		if metricsOfficialProviderHost(e.BaseURL) == "api.deepseek.com" {
			return "deepseek"
		}
	case "mimo-api":
		if metricsOfficialProviderHost(e.BaseURL) == "api.xiaomimimo.com" {
			return "mimoapi"
		}
	case "mimo-token-plan":
		if metricsOfficialProviderHost(e.BaseURL) == "token-plan-cn.xiaomimimo.com" {
			return "mimoplan"
		}
	}
	return ""
}

func providerMetricsBucket(e *config.ProviderEntry) string {
	if b := officialProviderBucket(e); b != "" {
		return b
	}
	if e == nil {
		return "unknown"
	}
	return metricBucket("custom_" + e.Name)
}

func safeModelBucket(c *config.Config, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = c.DefaultModel
	}
	e, ok := c.ResolveModel(ref)
	if !ok {
		return "unresolved"
	}
	provider := providerMetricsBucket(e)
	return metricBucket(provider + "_" + e.Model)
}

func plannerModelBucket(c *config.Config) string {
	if strings.TrimSpace(c.Agent.PlannerModel) == "" {
		return "off"
	}
	return safeModelBucket(c, c.Agent.PlannerModel)
}

func safeProviderAccessBucket(c *config.Config, name string) string {
	if p, ok := c.Provider(name); ok {
		return providerMetricsBucket(p)
	}
	return metricBucket("custom_" + name)
}

func (m *metricsAggregator) observeSettingsSnapshot(c *config.Config) {
	if c == nil {
		return
	}
	lang := c.DesktopLanguage()
	if lang == "" {
		lang = "auto"
	}
	themeStyle := c.DesktopThemeStyle()
	if themeStyle == "" {
		themeStyle = "default"
	}
	m.inc("settings_language", lang)
	m.inc("client_surface", "desktop")
	m.inc("client_version", metricBucket(version))
	m.inc("settings_desktop_layout", c.DesktopLayoutStyle())
	m.inc("settings_theme", c.DesktopTheme())
	m.inc("settings_theme_style", themeStyle)
	m.inc("settings_close_behavior", c.DesktopCloseBehavior())
	m.inc("settings_display_mode", c.DesktopDisplayMode())
	m.inc("settings_status_bar_style", c.DesktopStatusBarStyle())
	m.inc("settings_status_bar_items_count", statusBarItemsCountBucket(len(c.DesktopStatusBarItems())))
	m.inc("settings_check_updates", boolBucket(c.DesktopCheckUpdates()))
	m.inc("settings_default_model", safeModelBucket(c, c.DefaultModel))
	m.inc("settings_planner_model", plannerModelBucket(c))
	m.inc("settings_subagent_model", safeModelBucket(c, c.Agent.SubagentModel))
	m.inc("settings_subagent_effort", knownBucketDefault(c.Agent.SubagentEffort, "auto", "auto", "low", "medium", "high", "xhigh", "max", "off"))
	m.inc("settings_reasoning_language", config.NormalizeReasoningLanguage(c.Agent.ReasoningLanguage))
	m.inc("settings_provider_count", countBucket(len(c.Providers)))
	m.inc("settings_provider_access_count", countBucket(len(c.Desktop.ProviderAccess)))
	for _, name := range c.Desktop.ProviderAccess {
		m.inc("settings_provider_access", safeProviderAccessBucket(c, name))
	}
	m.observeBotSettingsSnapshot(c)
}

func (m *metricsAggregator) observeBotSettingsSnapshot(c *config.Config) {
	bot := c.Bot
	m.inc("settings_bot_enabled", boolBucket(bot.Enabled))
	m.inc("settings_bot_model", safeModelBucket(c, bot.Model))
	m.inc("settings_bot_tool_approval", knownBucketDefault(bot.ToolApprovalMode, "ask", "ask", "auto", "yolo"))
	m.inc("settings_bot_allowlist", boolBucket(bot.Allowlist.Enabled))
	m.inc("settings_bot_allow_all", boolBucket(bot.Allowlist.AllowAll))
	m.inc("settings_bot_qq_enabled", boolBucket(bot.QQ.Enabled))
	m.inc("settings_bot_feishu_enabled", boolBucket(bot.Feishu.Enabled))
	m.inc("settings_bot_weixin_enabled", boolBucket(bot.Weixin.Enabled))
	m.inc("settings_bot_connection_count", countBucket(len(bot.Connections)))
	for _, conn := range bot.Connections {
		provider := knownBucket(conn.Provider, "qq", "feishu", "weixin")
		m.inc("settings_bot_connection_provider", provider)
		m.inc("settings_bot_connection_enabled", boolBucket(conn.Enabled))
		m.inc("settings_bot_connection_status", knownBucket(conn.Status, "disconnected", "pending", "connected", "error"))
		m.inc("settings_bot_connection_model", safeModelBucket(c, conn.Model))
		m.inc("settings_bot_connection_approval", knownBucketDefault(conn.ToolApprovalMode, "default", "default", "ask", "auto", "yolo"))
	}
}

func (a *App) recordSettingsMetricsSnapshot(c *config.Config) {
	if version == "dev" || c == nil {
		return
	}
	m := a.metrics.Load()
	if m == nil {
		return
	}
	m.observeSettingsSnapshot(c)
	m.persist()
}

// recordDiagnosticMetric persists one bounded operational signal even when the
// native event arrives before Wails OnStartup installs the session aggregator.
func (a *App) recordDiagnosticMetric(signal, bucket string) {
	a.recordDiagnosticMetricCount(signal, bucket, 1)
}

func (a *App) recordDiagnosticMetricCount(signal, bucket string, count int) {
	if count <= 0 {
		return
	}
	if version == "dev" {
		return
	}
	m := a.metrics.Load()
	if m == nil {
		cfg, err := config.Load()
		if err != nil || !cfg.DesktopMetrics() {
			return
		}
		m = newMetricsAggregator(config.MemoryUserDir())
	}
	m.add(signal, metricBucket(bucket), count)
	m.persist()
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
		if e.Err != nil && e.Outcome != event.TurnOutcomeRecoveryPaused {
			m.inc("provider_error", errorClass(e.Err.Error()))
		}
	case event.ToolResult:
		if e.Tool.Err != "" {
			m.inc("tool_error", toolErrorClass(e.Tool.Err))
		}
	case event.CompactionDone:
		m.inc("compaction", "total")
	case event.Notice:
		if e.Text == "No visible answer was produced; asking the assistant to respond again." || strings.HasPrefix(e.Detail, "empty final answer blocked") {
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

// badRequestReason separates the 400s that need different fixes. Every arm
// returns a fixed label matched against a fixed substring, so nothing the
// provider echoed back can reach the bucket — the same constraint errorClass
// works under. Unrecognized shapes stay plain http_400 rather than guessing.
func badRequestReason(low string) string {
	switch {
	case strings.Contains(low, "image_url"), strings.Contains(low, "unknown variant"):
		return "content"
	case strings.Contains(low, "is not of type"), strings.Contains(low, "invalid schema for function"):
		return "schema"
	case strings.Contains(low, "thinking") && strings.Contains(low, "passed back"):
		return "reasoning_replay"
	case strings.Contains(low, "thinking") && (strings.Contains(low, "expected a boolean") || strings.Contains(low, "invalid type")):
		return "thinking_shape"
	case strings.Contains(low, "context length"), strings.Contains(low, "maximum context"), strings.Contains(low, "too long"):
		return "context_length"
	case strings.Contains(low, "tool_calls"), strings.Contains(low, "missing field name"):
		return "tool_calls"
	}
	return ""
}

// errorClass extracts only the failure category — never the message itself, which
// can echo request content back from a provider.
func errorClass(msg string) string {
	if mm := statusCodePattern.FindStringSubmatch(msg); mm != nil {
		switch code := mm[1]; {
		case code == "400":
			if reason := badRequestReason(strings.ToLower(msg)); reason != "" {
				return "http_400_" + reason
			}
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
	case strings.Contains(low, "authorization cancelled"):
		return "authorization_cancelled"
	case strings.Contains(low, "authorization failed"):
		return "authorization_failed"
	case strings.Contains(low, "package manager busy"):
		return "package_manager_busy"
	case strings.Contains(low, "package install failed"):
		return "package_install_failed"
	case strings.Contains(low, "package verify failed"), strings.Contains(low, "signature verification failed"):
		return "package_verify_failed"
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
	case strings.Contains(low, "recovery"):
		return "recovery"
	case strings.Contains(low, "hook"):
		return "hook"
	case strings.Contains(low, "timeout"), strings.Contains(low, "deadline"):
		return "timeout"
	default:
		return "exec"
	}
}

// observeRecoveryMetrics merges content-free recovery counters from a controller
// (failure events, rule/review continues, human prompts/actions, reviewer errors).
func (m *metricsAggregator) observeRecoveryMetrics(stats recovery.Metrics) {
	if m == nil {
		return
	}
	add := func(signal string, n int64) {
		for range n {
			m.inc(signal, "total")
		}
	}
	add("recovery_failure", stats.FailureEvents)
	add("recovery_rule_continue", stats.RuleContinues)
	add("recovery_review_continue", stats.ReviewContinues)
	add("recovery_human_prompt", stats.HumanPrompts)
	add("recovery_human_continue", stats.HumanContinues)
	add("recovery_human_revise", stats.HumanRevises)
	add("recovery_review_error", stats.ReviewErrors)
	add("recovery_repeat_prompt", stats.RepeatPrompts)
	if stats.ReviewLatencyCount > 0 {
		avg := stats.ReviewLatencyMsSum / stats.ReviewLatencyCount
		switch {
		case avg < 500:
			m.inc("recovery_review_latency", "lt_500ms")
		case avg < 2000:
			m.inc("recovery_review_latency", "lt_2s")
		case avg < 10000:
			m.inc("recovery_review_latency", "lt_10s")
		default:
			m.inc("recovery_review_latency", "gte_10s")
		}
	}
}

func observeControllerRecoveryMetrics(m *metricsAggregator, ctrl any) {
	if m == nil || ctrl == nil {
		return
	}
	if drainer, ok := ctrl.(interface {
		DrainRecoveryMetrics() recovery.Metrics
	}); ok {
		m.observeRecoveryMetrics(drainer.DrainRecoveryMetrics())
	}
}

func (m *metricsAggregator) observeTurnEventMetrics(stats turnevent.MetricsSnapshot) {
	if m == nil {
		return
	}
	m.add("turn_ledger_stream_raw", "total", int(stats.RawEvents))
	m.add("turn_ledger_stream_records", "total", int(stats.StreamRecords))
	m.add("turn_ledger_write_bytes", "total", int(stats.BytesWritten))
	m.add("turn_ledger_replay_events", "total", int(stats.ReplayEvents))
	m.add("turn_ledger_replay_bytes", "total", int(stats.ReplayBytes))
	m.add("turn_ledger_replay_reset", "total", int(stats.ReplayResets))
	m.add("turn_ledger_compaction", "success", int(stats.Compactions))
	m.add("turn_ledger_compaction", "failed", int(stats.CompactionFailures))
	m.add("turn_ledger_compaction_bytes", "before", int(stats.BytesBeforeCompact))
	m.add("turn_ledger_compaction_bytes", "after", int(stats.BytesAfterCompact))
	m.add("turn_ledger_failure", "write", int(stats.WriteFailures))
	m.add("turn_ledger_recovery", "torn_tail", int(stats.TornTails))
	m.add("turn_ledger_projection_retry", "total", int(stats.ProjectionRetries))
	latencyBuckets := []string{"lt_1ms", "1_5ms", "5_20ms", "20_100ms", "gte_100ms"}
	for i, bucket := range latencyBuckets {
		m.add("turn_ledger_append_latency", bucket, int(stats.AppendLatencyBuckets[i]))
		m.add("turn_ledger_replay_latency", bucket, int(stats.ReplayLatencyBuckets[i]))
		m.add("turn_ledger_compact_latency", bucket, int(stats.CompactLatencyBuckets[i]))
	}
	switch {
	case stats.FileSizeBytes < 256<<10:
		m.inc("turn_ledger_file_size", "lt_256k")
	case stats.FileSizeBytes < 1<<20:
		m.inc("turn_ledger_file_size", "256k_1m")
	case stats.FileSizeBytes < 8<<20:
		m.inc("turn_ledger_file_size", "1m_8m")
	case stats.FileSizeBytes < 32<<20:
		m.inc("turn_ledger_file_size", "8m_32m")
	default:
		m.inc("turn_ledger_file_size", "gte_32m")
	}
	if stats.UnconfirmedTurns > 0 {
		m.add("turn_ledger_projection_pending", "total", stats.UnconfirmedTurns)
	}
}

func observeControllerTurnEventMetrics(m *metricsAggregator, ctrl any) {
	if m == nil || ctrl == nil {
		return
	}
	if drainer, ok := ctrl.(interface {
		DrainTurnEventMetrics() turnevent.MetricsSnapshot
	}); ok {
		m.observeTurnEventMetrics(drainer.DrainTurnEventMetrics())
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

	metricsPendingMu.Lock()
	pending := readCounters(m.path)
	pending.merge(delta)
	writeCounters(m.path, pending)
	metricsPendingMu.Unlock()
}

func readCounters(path string) counters {
	b, err := readFileUTF8(path)
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
	InstallID      string          `json:"installId,omitempty"`
	Version        string          `json:"version"`
	OS             string          `json:"os"`
	Arch           string          `json:"arch,omitempty"`
	Channel        string          `json:"channel,omitempty"`
	OSBuild        int             `json:"osBuild,omitempty"`
	OSRevision     int             `json:"osRevision,omitempty"`
	DistroID       string          `json:"distroId,omitempty"`
	DistroVersion  string          `json:"distroVersion,omitempty"`
	KernelVersion  string          `json:"kernelVersion,omitempty"`
	SessionType    string          `json:"sessionType,omitempty"`
	RuntimeEngine  string          `json:"runtimeEngine,omitempty"`
	RuntimeVersion string          `json:"runtimeVersion,omitempty"`
	GPUMode        string          `json:"gpuMode,omitempty"`
	Counters       []metricCounter `json:"counters"`
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
	path := filepath.Join(config.MemoryUserDir(), metricsPendingFile)
	temp := path + ".sending"
	metricsPendingMu.Lock()
	if os.Rename(path, temp) != nil {
		metricsPendingMu.Unlock()
		return // nothing pending
	}
	metricsPendingMu.Unlock()
	flat := flatten(readCounters(temp))
	device := collectDeviceInfo()
	runtimeContext := webRuntimeContextForTelemetry(500 * time.Millisecond)
	payload := metricsPayload{
		Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, Channel: channel,
		OSBuild: device.OSBuild, OSRevision: device.OSRevision,
		DistroID: device.DistroID, DistroVersion: device.DistroVersion,
		KernelVersion: device.KernelVersion, SessionType: device.SessionType,
		RuntimeEngine: runtimeContext.Engine, RuntimeVersion: runtimeContext.RuntimeVersion,
		GPUMode: runtimeContext.GPUMode, Counters: flat,
	}
	if id, err := installID(); err == nil {
		payload.InstallID = id
	}
	if len(flat) == 0 || a.postMetrics(payload) {
		_ = os.Remove(temp)
		return
	}
	metricsPendingMu.Lock()
	pending := readCounters(path)
	pending.merge(readCounters(temp))
	writeCounters(path, pending)
	metricsPendingMu.Unlock()
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
	c.Timeout = metricsPostTimeout
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
