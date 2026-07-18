package runtimeservice

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"reasonix/internal/billing"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/sessiontelemetry"
)

var ErrInvalidStatusProjection = errors.New("runtime service: invalid status projection")

// ContextSource is the already-frozen Controller and telemetry view used by
// both Local and Remote snapshot/status projection. It owns no Controller and
// performs no reads after ProjectContext returns.
type ContextSource struct {
	UsedTokens   int
	WindowTokens int
	LastUsage    *provider.Usage
	Telemetry    sessiontelemetry.Snapshot
}

// RuntimeBinding binds an opaque paging cursor to one target incarnation.
// Incarnation is adapter-private (a Remote runtime epoch or a Local generation)
// and is never returned through RuntimeAPI.
type RuntimeBinding struct {
	Session     runtimeapi.SessionRef
	Incarnation string
}

// ProjectBalance intentionally collapses provider/network failures to the same
// unavailable view as an unconfigured balance endpoint. Raw provider errors,
// URLs, and credentials never enter the shared DTO.
func ProjectBalance(balance *billing.Balance, queryErr error) runtimeapi.BalanceView {
	if queryErr != nil || balance == nil || !balance.Available {
		return runtimeapi.BalanceView{}
	}
	return runtimeapi.BalanceView{Available: true, Display: balance.Display()}
}

func ProjectContext(source ContextSource) (runtimeapi.ContextView, error) {
	if source.UsedTokens < 0 || source.WindowTokens < 0 {
		return runtimeapi.ContextView{}, invalidStatus("current context counters must be non-negative")
	}
	view := runtimeapi.ContextView{
		UsedTokens: source.UsedTokens, WindowTokens: source.WindowTokens,
		Sources: []runtimeapi.UsageSource{}, ReadFiles: []runtimeapi.ReadFileRecord{},
	}
	if usage := source.LastUsage; usage != nil {
		if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 ||
			usage.ReasoningTokens < 0 || usage.CacheHitTokens < 0 || usage.CacheMissTokens < 0 {
			return runtimeapi.ContextView{}, invalidStatus("last usage counters must be non-negative")
		}
		view.PromptTokens = usage.PromptTokens
		view.CompletionTokens = usage.CompletionTokens
		view.ReasoningTokens = usage.ReasoningTokens
		view.CacheHitTokens = usage.CacheHitTokens
		view.CacheMissTokens = usage.CacheMissTokens
	}

	usage := source.Telemetry.Usage
	if err := validateUsageStats("Session telemetry", usage); err != nil {
		return runtimeapi.ContextView{}, err
	}
	view.TotalTokens = usage.TotalTokens
	view.SessionCacheHitTokens = usage.CacheHitTokens
	view.SessionCacheMissTokens = usage.CacheMissTokens
	view.SessionCompletionTokens = usage.CompletionTokens
	view.RequestCount = usage.RequestCount
	view.ElapsedMillis = usage.ElapsedMs
	view.SessionCost = canonicalCost(usage.SessionCost, usage.SessionCostUsd)
	view.SessionCurrency = usage.SessionCurrency

	sourceNames := make([]string, 0, len(usage.Sources))
	for name := range usage.Sources {
		sourceNames = append(sourceNames, name)
	}
	sort.Strings(sourceNames)
	for _, name := range sourceNames {
		if err := validateIdentityText("usage source", name, 128); err != nil {
			return runtimeapi.ContextView{}, err
		}
		stats := usage.Sources[name]
		if err := validateUsageSource(name, stats); err != nil {
			return runtimeapi.ContextView{}, err
		}
		view.Sources = append(view.Sources, runtimeapi.UsageSource{
			Source: name, PromptTokens: stats.PromptTokens, CompletionTokens: stats.CompletionTokens,
			TotalTokens: stats.TotalTokens, ReasoningTokens: stats.ReasoningTokens,
			CacheHitTokens: stats.CacheHitTokens, CacheMissTokens: stats.CacheMissTokens,
			RequestCount: stats.RequestCount,
			SessionCost:  canonicalCost(stats.SessionCost, stats.SessionCostUsd), SessionCurrency: stats.SessionCurrency,
		})
	}

	for index, record := range source.Telemetry.ReadFiles {
		projected, err := projectReadFile(index, record)
		if err != nil {
			return runtimeapi.ContextView{}, err
		}
		view.ReadFiles = append(view.ReadFiles, projected)
	}
	return view, nil
}

// ProjectJobs validates and deterministically orders the still-running jobs.
// Terminal jobs are rejected rather than silently crossing the V1 query.
func ProjectJobs(items []jobs.View) ([]runtimeapi.Job, error) {
	out := make([]runtimeapi.Job, 0, len(items))
	seen := make(map[runtimeapi.JobID]struct{}, len(items))
	for index, item := range items {
		var kind runtimeapi.JobKind
		switch item.Kind {
		case string(runtimeapi.JobBash):
			kind = runtimeapi.JobBash
		case string(runtimeapi.JobTask):
			kind = runtimeapi.JobTask
		default:
			return nil, invalidStatus("job %d has unsupported kind %q", index, item.Kind)
		}
		if item.Status != string(runtimeapi.JobRunning) {
			return nil, invalidStatus("job %d has unsupported status %q", index, item.Status)
		}
		id := runtimeapi.JobID(strings.TrimSpace(item.ID))
		if id == "" || strings.TrimSpace(item.Label) == "" || item.StartedAt < 0 {
			return nil, invalidStatus("job %d has invalid identity, label, or start time", index)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, invalidStatus("job %d repeats job id %q", index, id)
		}
		seen[id] = struct{}{}
		out = append(out, runtimeapi.Job{
			ID: id, Kind: kind, Label: item.Label, Status: runtimeapi.JobRunning,
			StartedAtMillis: item.StartedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedAtMillis != out[j].StartedAtMillis {
			return out[i].StartedAtMillis < out[j].StartedAtMillis
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// PageJobs applies the frozen 200/1000 item limits and issues an opaque cursor
// bound to target, incarnation, and the exact running-job revision.
func PageJobs(binding RuntimeBinding, items []jobs.View, cursor runtimeapi.Cursor, limit int) (runtimeapi.JobPage, error) {
	if err := requireSession(binding.Session); err != nil || strings.TrimSpace(binding.Incarnation) == "" {
		return runtimeapi.JobPage{}, ErrInvalidSession
	}
	pageLimit, err := normalizedPageLimit(limit)
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	projected, err := ProjectJobs(items)
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	revision := snapshotRevision(projected, sessionBinding(binding.Session), binding.Incarnation)
	offset, err := decodeJobCursor(cursor, binding, revision, len(projected))
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	end := offset + pageLimit
	if end > len(projected) {
		end = len(projected)
	}
	result := runtimeapi.JobPage{Jobs: append([]runtimeapi.Job(nil), projected[offset:end]...)}
	if end < len(projected) {
		result.HasMore = true
		result.Next, err = encodeJobCursor(jobCursorPayload{
			Target: sessionBinding(binding.Session), Incarnation: binding.Incarnation,
			Revision: revision, Offset: end,
		})
		if err != nil {
			return runtimeapi.JobPage{}, err
		}
	}
	return result, nil
}

type jobCursorPayload struct {
	Version     int    `json:"v"`
	Target      string `json:"t"`
	Incarnation string `json:"i"`
	Revision    string `json:"r"`
	Offset      int    `json:"o"`
}

var (
	jobCursorKeyOnce sync.Once
	jobCursorKey     [32]byte
	jobCursorKeyErr  error
)

func statusCursorKey() ([32]byte, error) {
	jobCursorKeyOnce.Do(func() {
		_, jobCursorKeyErr = rand.Read(jobCursorKey[:])
	})
	return jobCursorKey, jobCursorKeyErr
}

func encodeJobCursor(payload jobCursorPayload) (runtimeapi.Cursor, error) {
	key, err := statusCursorKey()
	if err != nil {
		return "", ErrQueryFailed
	}
	payload.Version = cursorVersion
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", ErrQueryFailed
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(raw)
	encoded := append(append([]byte(nil), raw...), mac.Sum(nil)...)
	return runtimeapi.Cursor(base64.RawURLEncoding.EncodeToString(encoded)), nil
}

func decodeJobCursor(cursor runtimeapi.Cursor, binding RuntimeBinding, revision string, length int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	key, err := statusCursorKey()
	if err != nil {
		return 0, ErrQueryFailed
	}
	raw, err := base64.RawURLEncoding.DecodeString(string(cursor))
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != string(cursor) {
		return 0, ErrInvalidCursor
	}
	message, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(message)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, ErrInvalidCursor
	}
	var payload jobCursorPayload
	if err := json.Unmarshal(message, &payload); err != nil || payload.Version != cursorVersion || payload.Offset < 0 {
		return 0, ErrInvalidCursor
	}
	if payload.Target != sessionBinding(binding.Session) || payload.Incarnation != binding.Incarnation {
		return 0, ErrInvalidCursor
	}
	if payload.Revision != revision || payload.Offset > length {
		return 0, ErrStaleCursor
	}
	return payload.Offset, nil
}

func validateUsageStats(label string, usage sessiontelemetry.UsageStats) error {
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 ||
		usage.ReasoningTokens < 0 || usage.CacheHitTokens < 0 || usage.CacheMissTokens < 0 ||
		usage.RequestCount < 0 || usage.ElapsedMs < 0 {
		return invalidStatus("%s counters must be non-negative", label)
	}
	if err := validateCostsAndCurrency(label, usage.SessionCost, usage.SessionCostUsd, usage.SessionCurrency); err != nil {
		return err
	}
	if usage.ActiveTurnStartedAt != 0 || len(usage.SourceSessionCache) != 0 {
		return invalidStatus("%s contains mutable runtime-only accounting", label)
	}
	return nil
}

func validateUsageSource(source string, stats sessiontelemetry.UsageSourceStats) error {
	if stats.PromptTokens < 0 || stats.CompletionTokens < 0 || stats.TotalTokens < 0 ||
		stats.ReasoningTokens < 0 || stats.CacheHitTokens < 0 || stats.CacheMissTokens < 0 || stats.RequestCount < 0 {
		return invalidStatus("usage source %q counters must be non-negative", source)
	}
	return validateCostsAndCurrency("usage source "+source, stats.SessionCost, stats.SessionCostUsd, stats.SessionCurrency)
}

func validateCostsAndCurrency(label string, cost, compatibilityCost float64, currency string) error {
	if !finiteNonNegative(cost) || !finiteNonNegative(compatibilityCost) {
		return invalidStatus("%s cost must be finite and non-negative", label)
	}
	canonical := canonicalCost(cost, compatibilityCost)
	if currency == "" {
		if canonical != 0 {
			return invalidStatus("%s has cost without a currency", label)
		}
		return nil
	}
	if err := validateIdentityText("currency", currency, 16); err != nil {
		return invalidStatus("%s has invalid currency %q", label, currency)
	}
	return nil
}

func canonicalCost(cost, compatibilityCost float64) float64 {
	if cost == 0 && compatibilityCost > 0 {
		return compatibilityCost
	}
	return cost
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validateIdentityText(label, value string, maxRunes int) error {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return invalidStatus("%s must be trimmed, non-empty valid UTF-8 within %d characters", label, maxRunes)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return invalidStatus("%s contains a control character", label)
		}
	}
	return nil
}

func projectReadFile(index int, record sessiontelemetry.ReadFileRecord) (runtimeapi.ReadFileRecord, error) {
	if err := validateReadPath(record.Path); err != nil {
		return runtimeapi.ReadFileRecord{}, invalidStatus("read file %d path %q is unsafe: %v", index, record.Path, err)
	}
	if record.Turn < 0 || record.Time < 0 || record.Offset < 0 || record.Limit < 0 {
		return runtimeapi.ReadFileRecord{}, invalidStatus("read file %d turn, time, offset, and limit must be non-negative", index)
	}
	return runtimeapi.ReadFileRecord{
		Path: record.Path, Turn: record.Turn, TimeMs: record.Time,
		Offset: positiveInt64(record.Offset), Limit: positiveInt64(record.Limit), Truncated: record.Truncated,
	}, nil
}

func validateReadPath(value string) error {
	if err := validateIdentityText("read path", value, 4096); err != nil {
		return err
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return errors.New("must be a primary-relative POSIX path")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("must not escape the primary workspace")
	}
	if cleaned != value {
		return errors.New("must already be canonical")
	}
	return nil
}

func positiveInt64(value int) *int64 {
	if value <= 0 {
		return nil
	}
	converted := int64(value)
	return &converted
}

func invalidStatus(format string, args ...any) error {
	// Keep the detail deterministic for snapshot validation and tests while the
	// daemon maps it to QUERY_FAILED instead of crossing it over the wire.
	return fmt.Errorf("%w: %s", ErrInvalidStatusProjection, fmt.Sprintf(format, args...))
}
