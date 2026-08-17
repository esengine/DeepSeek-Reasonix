package telemetry

import (
	"context"
	"errors"
	"net"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/recovery"
)

type Options struct {
	Mode           string
	Version        string
	HomeDir        string
	Interactive    bool
	Proxy          netclient.ProxySpec
	CLIMode        string
	PermissionMode string
	SessionMode    string
	Language       string
}

type Reporter struct {
	client  *Client
	version string
	home    string
	static  []Counter
}

func Start(opts Options) *Reporter {
	if !Enabled(opts.Mode, opts.Version, opts.Interactive) {
		if strings.EqualFold(strings.TrimSpace(opts.Mode), "off") || envOptOut() {
			_ = Cleanup(opts.HomeDir)
		}
		return nil
	}
	client, err := newClient(opts.HomeDir, opts.Version, opts.Proxy)
	if err != nil {
		return nil
	}
	r := &Reporter{
		client:  client,
		version: opts.Version,
		home:    opts.HomeDir,
		static: []Counter{
			{Signal: "client_surface", Bucket: "cli", Count: 1},
			{Signal: "client_version", Bucket: safeBucket(opts.Version, "other"), Count: 1},
			{Signal: "cli_mode", Bucket: enumBucket(opts.CLIMode, "run", "tui"), Count: 1},
			{Signal: "cli_permission_mode", Bucket: permissionBucket(opts.PermissionMode), Count: 1},
			{Signal: "cli_session_mode", Bucket: enumBucket(opts.SessionMode, "fresh", "resume", "continue", "copy"), Count: 1},
			{Signal: "settings_language", Bucket: languageBucket(opts.Language), Count: 1},
		},
	}
	go client.backgroundFlush()
	return r
}

func (r *Reporter) Wrap(inner event.Sink) event.Sink {
	if r == nil {
		return inner
	}
	return &sink{AuditForwarder: event.AuditForwarder{Inner: inner}, inner: inner, reporter: r, counts: countersFrom(r.static)}
}

func (r *Reporter) RecordRecovery(m recovery.Metrics) {
	if r == nil {
		return
	}
	counts := map[string]int{}
	addMetric(counts, "recovery_failure", "count", m.FailureEvents)
	addMetric(counts, "recovery_rule_continue", "count", m.RuleContinues)
	addMetric(counts, "recovery_review_continue", "count", m.ReviewContinues)
	addMetric(counts, "recovery_human_prompt", "count", m.HumanPrompts)
	addMetric(counts, "recovery_human_continue", "count", m.HumanContinues)
	addMetric(counts, "recovery_human_revise", "count", m.HumanRevises)
	addMetric(counts, "recovery_review_error", "count", m.ReviewErrors)
	addMetric(counts, "recovery_repeat_prompt", "count", m.RepeatPrompts)
	if m.ReviewLatencyCount > 0 {
		add(counts, "recovery_review_latency", latencyBucket(time.Duration(m.ReviewLatencyMsSum/m.ReviewLatencyCount)*time.Millisecond), int(m.ReviewLatencyCount))
	}
	r.append(counts)
}

func addMetric(counts map[string]int, signal, bucket string, count int64) {
	if count > 0 {
		add(counts, signal, bucket, int(count))
	}
}

func (r *Reporter) append(counts map[string]int) {
	if r == nil || len(counts) == 0 {
		return
	}
	counters := make([]Counter, 0, len(counts))
	for key, count := range counts {
		signal, bucket, _ := strings.Cut(key, "\x00")
		if count > 1_000_000 {
			count = 1_000_000
		}
		counters = append(counters, Counter{Signal: signal, Bucket: bucket, Count: count})
	}
	_ = appendPending(r.home, pendingPayload{Version: r.version, OS: runtime.GOOS, Counters: counters})
}

type sink struct {
	event.AuditForwarder
	inner          event.Sink
	reporter       *Reporter
	counts         map[string]int
	started        time.Time
	hasText        bool
	emptyFinalSeen bool
}

func (s *sink) Emit(e event.Event) {
	s.observe(e)
	s.inner.Emit(e)
}

func (s *sink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	add(s.counts, "tool_call_reasoning_recovery", string(a.Kind), 1)
	event.RecordProtocolRecovery(s.inner, a)
}

func (s *sink) observe(e event.Event) {
	switch e.Kind {
	case event.TurnStarted:
		s.started = time.Now()
		s.hasText = false
		s.emptyFinalSeen = false
		add(s.counts, "turns", "count", 1)
	case event.Text:
		if e.Text != "" {
			s.hasText = true
		}
	case event.Message:
		if e.Text != "" {
			s.hasText = true
		}
	case event.Usage:
		if e.Usage != nil {
			add(s.counts, "finish_reason", finishReasonBucket(e.Usage.FinishReason), 1)
			add(s.counts, "cache_hit", cacheBucket(e.Usage.CacheHitTokens, e.Usage.CacheMissTokens), 1)
		}
	case event.ToolResult:
		if e.Tool.Err != "" {
			add(s.counts, "tool_error", toolErrorBucket(e.Tool.Err), 1)
		}
	case event.Notice:
		if e.Code == event.NoticeCodeEmptyFinal {
			add(s.counts, "empty_final", "yes", 1)
			s.emptyFinalSeen = true
		}
	case event.CompactionStarted:
		add(s.counts, "compaction", enumBucket(e.Compaction.Trigger, "auto", "manual"), 1)
	case event.TurnDone:
		if !s.hasText && e.Err == nil && !s.emptyFinalSeen {
			add(s.counts, "empty_final", "yes", 1)
		}
		if bucket := providerErrorBucket(e.Err); bucket != "" {
			add(s.counts, "provider_error", bucket, 1)
		}
		add(s.counts, "cli_exit", exitBucket(e), 1)
		if !s.started.IsZero() {
			add(s.counts, "cli_turn_latency", latencyBucket(time.Since(s.started)), 1)
		}
		s.reporter.append(s.counts)
		s.counts = map[string]int{}
		s.started = time.Time{}
		s.hasText = false
		s.emptyFinalSeen = false
	}
}

func countersFrom(in []Counter) map[string]int {
	out := map[string]int{}
	for _, c := range in {
		add(out, c.Signal, c.Bucket, c.Count)
	}
	return out
}

func add(counts map[string]int, signal, bucket string, count int) {
	if count <= 0 || signal == "" || bucket == "" {
		return
	}
	counts[signal+"\x00"+bucket] += count
}

var unsafeBucketChars = regexp.MustCompile(`[^a-z0-9_]+`)

func safeBucket(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = unsafeBucketChars.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return fallback
	}
	if len(value) > 96 {
		value = value[:96]
	}
	return value
}

func enumBucket(value string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if slices.Contains(allowed, value) {
		return value
	}
	return "other"
}

func permissionBucket(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "manual", "ask":
		return "ask"
	case "auto", "acceptedits":
		return "auto"
	case "dontask":
		return "dont_ask"
	case "plan":
		return "plan"
	case "bypasspermissions", "yolo":
		return "yolo"
	default:
		return "other"
	}
}

func languageBucket(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "zh") {
		return "zh"
	}
	if strings.HasPrefix(value, "en") {
		return "en"
	}
	if value == "" || value == "auto" {
		return "auto"
	}
	return "other"
}

func finishReasonBucket(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "stop", "tool_calls", "length", "content_filter", "repetition_truncation":
		return safeBucket(value, "unknown")
	case "":
		return "unknown"
	default:
		return "other"
	}
}

func cacheBucket(hit, miss int) string {
	total := hit + miss
	if total <= 0 {
		return "unknown"
	}
	pct := hit * 100 / total
	switch {
	case pct == 0:
		return "0"
	case pct < 25:
		return "1_24"
	case pct < 50:
		return "25_49"
	case pct < 75:
		return "50_74"
	case pct < 90:
		return "75_89"
	default:
		return "90_100"
	}
}

func toolErrorBucket(value string) string {
	v := strings.ToLower(value)
	switch {
	case strings.Contains(v, "permission"), strings.Contains(v, "blocked"), strings.Contains(v, "denied"):
		return "permission"
	case strings.Contains(v, "timeout"), strings.Contains(v, "deadline"):
		return "timeout"
	case strings.Contains(v, "cancel"):
		return "cancelled"
	case strings.Contains(v, "not found"), strings.Contains(v, "no such"):
		return "not_found"
	default:
		return "other"
	}
}

func providerErrorBucket(err error) string {
	if err == nil {
		return ""
	}
	var auth *provider.AuthError
	if errors.As(err, &auth) {
		return "auth"
	}
	var api *provider.APIError
	if errors.As(err, &api) {
		switch {
		case api.Status == 429:
			return "rate_limit"
		case api.Status >= 500:
			return "server"
		case api.Status >= 400:
			return "request"
		default:
			return "http"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "network"
	}
	if provider.IsStreamInterrupted(err) {
		return "interrupted"
	}
	return ""
}

func latencyBucket(d time.Duration) string {
	switch {
	case d < time.Second:
		return "lt_1s"
	case d < 5*time.Second:
		return "s_1_5"
	case d < 15*time.Second:
		return "s_5_15"
	case d < time.Minute:
		return "s_15_60"
	case d < 5*time.Minute:
		return "m_1_5"
	case d < 15*time.Minute:
		return "m_5_15"
	default:
		return "m_15_plus"
	}
}

func exitBucket(e event.Event) string {
	if e.Cancelled || errors.Is(e.Err, context.Canceled) {
		return "cancelled"
	}
	if e.Outcome == event.TurnOutcomeRecoveryPaused {
		return "recovery_paused"
	}
	if e.Err != nil {
		return "error"
	}
	return "success"
}
