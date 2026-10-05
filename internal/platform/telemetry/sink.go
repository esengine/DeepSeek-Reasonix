package telemetry

import (
	"context"
	"errors"
	"net"
	"reasonix/internal/contract/hostaudit"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
)

type Options struct {
	Mode    string
	Version string
	// Surface is empty for the CLI, the only surface that reported before the
	// field existed.
	Surface surface.Surface
	// SuppressPing withholds the daily launch ping while still reporting
	// counters. Zero sends it, which is what every surface did before the
	// consents were separable.
	SuppressPing   bool
	HomeDir        string
	Interactive    bool
	Proxy          netclient.ProxySpec
	CLIMode        string
	Profile        string
	PermissionMode string
	SessionMode    string
	Language       string
}

type Reporter struct {
	client  *Client
	version string
	surface surface.Surface
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
	from := opts.Surface.Or(surface.CLI)
	client, err := newClient(opts.HomeDir, opts.Version, from, opts.Proxy)
	if err != nil {
		return nil
	}
	r := &Reporter{
		client:  client,
		version: opts.Version,
		surface: from,
		home:    opts.HomeDir,
		static: []Counter{
			{Signal: "client_surface", Bucket: from.String(), Count: 1},
			{Signal: "client_version", Bucket: safeBucket(opts.Version, "other"), Count: 1},
			{Signal: "cli_mode", Bucket: enumBucket(opts.CLIMode, "run", "tui"), Count: 1},
			{Signal: "cli_profile", Bucket: enumBucket(opts.Profile, "economy", "balanced", "delivery"), Count: 1},
			{Signal: "cli_permission_mode", Bucket: permissionBucket(opts.PermissionMode), Count: 1},
			{Signal: "cli_session_mode", Bucket: enumBucket(opts.SessionMode, "fresh", "resume", "continue", "copy"), Count: 1},
			{Signal: "settings_language", Bucket: languageBucket(opts.Language), Count: 1},
		},
	}
	go client.backgroundFlush(!opts.SuppressPing)
	return r
}

func (r *Reporter) Wrap(inner event.Sink) event.Sink {
	if r == nil {
		return inner
	}
	return &sink{AuditForwarder: event.AuditForwarder{Inner: inner}, inner: inner, reporter: r, counts: countersFrom(r.static)}
}

func (r *Reporter) RecordRecovery(m hostaudit.RecoveryMetrics) {
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
	_ = appendPending(r.home, pendingPayload{Version: r.version, OS: runtime.GOOS, Surface: r.surface.String(), Counters: counters})
}

// turnCounts is the sink state that lives for exactly one turn. Both boundaries
// reset it by whole-struct assignment, so a field added here starts clean
// without a second edit — the declaration is the reset. Embedded, not nested,
// so access stays flat.
type turnCounts struct {
	started                    time.Time
	prompt, completion, cached int
	hasText                    bool
	emptyFinalSeen             bool
}

type sink struct {
	event.AuditForwarder
	inner    event.Sink
	reporter *Reporter
	counts   map[string]int
	turnCounts
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
		s.turnCounts = turnCounts{started: time.Now()}
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
			s.prompt += e.Usage.PromptTokens
			s.completion += e.Usage.CompletionTokens
			s.cached += e.Usage.CacheHitTokens
		}
		// One changed byte re-bills the whole prefix at full rate. The kernel
		// already attributes a prefix change; this carries that verdict and
		// names the case it cannot see.
		if d := e.CacheDiagnostics; d != nil {
			switch {
			case d.PrefixChanged:
				for _, reason := range d.PrefixChangeReasons {
					add(s.counts, "cache_miss_reason", enumBucket(reason, prefixChangeReasons...), 1)
				}
			case e.Usage != nil && e.Usage.CacheHitTokens == 0 && e.Usage.PromptTokens >= minCacheablePrompt:
				add(s.counts, "cache_miss_reason", "unexplained", 1)
			}
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
	case event.WorkspaceLeaseEvent:
		// Flushed on its own rather than into the turn's batch: the lease is
		// released when the last run of a session ends, which is not reliably
		// before the TurnDone that empties the batch.
		s.reporter.append(leaseCounts(e.WorkspaceLease))
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
		add(s.counts, "turn_prompt_tokens", tokenBucket(s.prompt), 1)
		add(s.counts, "turn_output_tokens", tokenBucket(s.completion), 1)
		add(s.counts, "turn_cached_tokens", tokenBucket(s.cached), 1)
		s.reporter.append(s.counts)
		s.counts = map[string]int{}
		s.turnCounts = turnCounts{}
	}
}

// leaseCounts buckets one closed account of the workspace write lease. Three
// closed enums and nothing else: the account is five integers, so there is no
// path, command or identity here to leave out.
func leaseCounts(l *event.WorkspaceLease) map[string]int {
	if l == nil {
		return nil
	}
	counts := map[string]int{}
	if l.Contended > 0 {
		add(counts, "lease_hold", "contended", 1)
		add(counts, "lease_wait", latencyBucket(time.Duration(l.WaitedMs)*time.Millisecond), 1)
	} else {
		add(counts, "lease_hold", "uncontended", 1)
	}
	// The share of a hold spent holding and not writing, which is what an
	// earlier release would give back. A hold with no length has no share.
	if l.HeldMs > 0 {
		add(counts, "lease_idle", shareBucket(float64(l.IdleMs)/float64(l.HeldMs)), 1)
	}
	return counts
}

func shareBucket(share float64) string {
	switch {
	case share < 0.25:
		return "lt_25"
	case share < 0.5:
		return "pc_25_50"
	case share < 0.75:
		return "pc_50_75"
	default:
		return "pc_75_100"
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

// tokenBucket bins one turn's token volume by magnitude. A flat-rate plan is
// priced by the tail, not the mean, so the shape is what has to survive the
// wire — and a bucket is the coarsest thing that still carries it. Bounds are
// spaced around the measured per-turn median near 300k.
func tokenBucket(n int) string {
	switch {
	case n <= 0:
		return "0"
	case n < 1_000:
		return "0_1k"
	case n < 4_000:
		return "1k_4k"
	case n < 16_000:
		return "4k_16k"
	case n < 64_000:
		return "16k_64k"
	case n < 256_000:
		return "64k_256k"
	case n < 1_000_000:
		return "256k_1m"
	case n < 4_000_000:
		return "1m_4m"
	default:
		return "4m_plus"
	}
}

// minCacheablePrompt is under the smallest prefix any vendor here will cache,
// so a miss below it is the rule rather than a finding.
const minCacheablePrompt = 1024

// prefixChangeReasons is what CompareShape derives from the shape hashes and
// Session.Rewrite queues beside it. Rewrite takes an open string, so a reason
// this build has not seen buckets as other rather than letting a caller widen
// the wire's bucket space.
var prefixChangeReasons = []string{
	"system", "tools", "compact", "guardian_merge",
	"rewind", "rewind_restore", "rewind_truncate",
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
