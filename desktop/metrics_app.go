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
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
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
	for _, ok := range allowed {
		if value == ok {
			return value
		}
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
	m.inc("settings_auto_plan", desktopAutoPlanMode(c.Agent.AutoPlan))
	m.inc("settings_memory_compiler", boolBucket(c.MemoryCompilerEnabled()))
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
	case event.MemoryCompilerStatsEvent:
		m.observeMemoryCompilerStats(e.MemoryCompiler)
	case event.Notice:
		if e.Text == "No visible answer was produced; asking the assistant to respond again." || strings.HasPrefix(e.Detail, "empty final answer blocked") {
			m.inc("empty_final", "total")
		}
	}
}

func (m *metricsAggregator) observeMemoryCompilerStats(s *event.MemoryCompilerStats) {
	if s == nil {
		return
	}
	m.inc("memory_compiler_turn", "total")
	m.inc("memory_compiler_injected", boolBucket(s.Injected))
	m.inc("memory_compiler_useful_ir", boolBucket(s.UsefulIR))
	m.inc("memory_compiler_compiled_tokens", tokenSizeBucket(s.CompiledTokens))
	m.inc("memory_compiler_ir_overhead_tokens", tokenSizeBucket(s.IROverheadTokens))
	m.inc("memory_compiler_memory_refs", countBucket(s.MemoryReferences))
	m.inc("memory_compiler_constraints", countBucket(s.Constraints))
	m.inc("memory_compiler_risk_notes", countBucket(s.RiskNotes))
	m.inc("memory_compiler_execution_steps", countBucket(s.ExecutionSteps))
	m.inc("memory_compiler_nodes", memorySizeBucket(s.TotalNodes))
	m.inc("memory_compiler_high_signal_nodes", memorySizeBucket(s.HighSignalNodes))
	m.inc("memory_compiler_tool_result_nodes", memorySizeBucket(s.ToolResultNodes))
	m.inc("memory_compiler_decisions", memorySizeBucket(s.DecisionNodes))
	m.inc("memory_compiler_strategies", memorySizeBucket(s.StrategyCount))
	m.inc("memory_compiler_learnings", memorySizeBucket(s.LearningCount))
}

func tokenSizeBucket(n int) string {
	if n <= 0 {
		return "t_0"
	}
	switch {
	case n <= 250:
		return "t_1_250"
	case n <= 750:
		return "t_251_750"
	case n <= 1500:
		return "t_751_1500"
	case n <= 3000:
		return "t_1501_3000"
	default:
		return "t_3001_plus"
	}
}

func memorySizeBucket(n int) string {
	if n <= 0 {
		return "n_0"
	}
	switch {
	case n <= 5:
		return "n_1_5"
	case n <= 20:
		return "n_6_20"
	case n <= 50:
		return "n_21_50"
	case n <= 100:
		return "n_51_100"
	case n <= 300:
		return "n_101_300"
	default:
		return "n_301_plus"
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
	InstallID string          `json:"installId,omitempty"`
	Version   string          `json:"version"`
	OS        string          `json:"os"`
	Counters  []metricCounter `json:"counters"`
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
	if os.Rename(path, temp) != nil {
		return // nothing pending
	}
	flat := flatten(readCounters(temp))
	payload := metricsPayload{Version: version, OS: runtime.GOOS, Counters: flat}
	if id, err := installID(); err == nil {
		payload.InstallID = id
	}
	if len(flat) == 0 || a.postMetrics(payload) {
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
