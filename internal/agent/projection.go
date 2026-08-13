package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// Context-projection schema versions. Readers accept any known version;
// writers always emit the current schema.
const (
	compactionStateSchemaV1      = 1
	compactionStateSchemaV2      = 2
	compactionStateSchemaV3      = 3
	compactionStateSchemaCurrent = compactionStateSchemaV3
)

// Cache state labels for resume/preflight telemetry. They never enter the
// provider-visible prompt.
const (
	CacheStateWarm    = "warm"
	CacheStateCold    = "cold"
	CacheStateUnknown = "unknown"
)

// Compaction trigger labels.
const (
	CompactionTriggerPressure = "pressure"
	CompactionTriggerManual   = "manual"
	CompactionTriggerOverflow = "overflow"
	CompactionTriggerSnip     = "snip"
	CompactionTriggerTool     = "tool"
)

// Compaction mode labels.
const (
	CompactionModeNative     = "native"
	CompactionModeSummarized = "summarized"
	CompactionModeDegraded   = "degraded"
	CompactionModeSnip       = "snip"
)

// ContextProjection is the model-visible view of a session. The canonical
// transcript in Session.Messages is never replaced by this structure.
type ContextProjection struct {
	Messages          []provider.Message `json:"messages"`
	TranscriptVersion uint64             `json:"transcript_version"`
	ProjectionVersion uint64             `json:"projection_version"`
	// CoveredCount is len(canonical) when the projection was built. Model-visible
	// context is projection.Messages + canonical[CoveredCount:].
	CoveredCount int `json:"covered_count"`
	// CoveredPrefixHash fingerprints provider-visible canonical[:CoveredCount]
	// so append-only growth can be distinguished from prefix edits/rewrites.
	CoveredPrefixHash string `json:"covered_prefix_hash,omitempty"`
	SummaryHash       string `json:"summary_hash,omitempty"`
	SourceTokens      int    `json:"source_tokens,omitempty"`
	ProjectionTokens  int    `json:"projection_tokens,omitempty"`
	// ViewInputHash/ViewOutputHash make free maintenance idempotent across
	// retries and resume. They fingerprint the visible view, not canonical
	// storage, so a projection can evolve without rewriting the transcript.
	ViewInputHash  string    `json:"view_input_hash,omitempty"`
	ViewOutputHash string    `json:"view_output_hash,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ContextMaintenanceReceipt is the durable, provider-neutral outcome of one
// context maintenance transaction. Transcript content is intentionally not
// included; hashes and counts are sufficient for dedupe and diagnostics.
type ContextMaintenanceReceipt struct {
	OperationID         string    `json:"operation_id,omitempty"`
	Status              string    `json:"status,omitempty"` // planned|applied|noop|blocked|failed
	Action              string    `json:"action,omitempty"` // snip|prune|summary|native_tool_clear|noop
	Trigger             string    `json:"trigger,omitempty"`
	SourceProjection    uint64    `json:"source_projection,omitempty"`
	ProjectionVersion   uint64    `json:"projection_version,omitempty"`
	CoveredCount        int       `json:"covered_count,omitempty"`
	CoveredPrefixHash   string    `json:"covered_prefix_hash,omitempty"`
	InputHash           string    `json:"input_hash,omitempty"`
	OutputHash          string    `json:"output_hash,omitempty"`
	InputTokens         int       `json:"input_tokens,omitempty"`
	ResultTokens        int       `json:"result_tokens,omitempty"`
	SavedTokens         int       `json:"saved_tokens,omitempty"`
	AffectedToolResults int       `json:"affected_tool_results,omitempty"`
	SummaryHash         string    `json:"summary_hash,omitempty"`
	Archive             string    `json:"archive,omitempty"`
	CacheBreak          bool      `json:"cache_break,omitempty"`
	Reason              string    `json:"reason,omitempty"`
	BlockedInputHash    string    `json:"blocked_input_hash,omitempty"`
	CreatedAt           time.Time `json:"created_at,omitempty"`
}

// CompactionOutcome reports whether compactToProjection installed a projection.
type CompactionOutcome int

const (
	// CompactionInstalled means a new (or replacement) projection was saved.
	CompactionInstalled CompactionOutcome = iota
	// CompactionNoop means no fold region / economics skip / empty fold after hooks.
	CompactionNoop
)

// CompactionState is the session context sidecar payload.
type CompactionState struct {
	SchemaVersion      int                        `json:"schema_version"`
	TranscriptVersion  uint64                     `json:"transcript_version"`
	Projection         ContextProjection          `json:"projection"`
	PromptCacheKey     string                     `json:"prompt_cache_key,omitempty"`
	LastCacheState     string                     `json:"last_cache_state,omitempty"`
	LastTrigger        string                     `json:"last_trigger,omitempty"`
	LastMode           string                     `json:"last_mode,omitempty"`
	LastSourceTokens   int                        `json:"last_source_tokens,omitempty"`
	LastResultTokens   int                        `json:"last_result_tokens,omitempty"`
	LastCompactionCost float64                    `json:"last_compaction_cost,omitempty"`
	Generation         uint64                     `json:"generation,omitempty"`
	LastReceipt        *ContextMaintenanceReceipt `json:"last_receipt,omitempty"`
	BlockedInputHash   string                     `json:"blocked_input_hash,omitempty"`
	BlockedReason      string                     `json:"blocked_reason,omitempty"`
	// NativeContextEditingAccepted latches the first successful native request.
	// ContextEditingFallbackLocal persists the only allowed request-shape switch:
	// an explicit unsupported response before that latch was set.
	NativeContextEditingAccepted bool      `json:"native_context_editing_accepted,omitempty"`
	ContextEditingFallbackLocal  bool      `json:"context_editing_fallback_local,omitempty"`
	UpdatedAt                    time.Time `json:"updated_at"`
}

// CompactionTelemetry is the structured observability record for one
// compaction attempt. Sensitive transcript content is intentionally omitted.
type CompactionTelemetry struct {
	Trigger           string `json:"trigger"`
	CacheState        string `json:"cache_state"`
	Mode              string `json:"mode"`
	Native            bool   `json:"native"`
	SourceTokens      int    `json:"source_tokens"`
	FoldTokens        int    `json:"fold_tokens"` // summarizer input after any shortening
	Spans             int    `json:"spans"`       // summarizer calls the fold needed; 1 unless it was split
	ProjectionTokens  int    `json:"projection_tokens"`
	UserTurnsKept     int    `json:"user_turns_kept"`
	UserTurnsDropped  int    `json:"user_turns_dropped"` // past the retention budget, now summary-only
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	CacheHitTokens    int    `json:"cache_hit_tokens"`
	CacheMissTokens   int    `json:"cache_miss_tokens"`
	CacheWriteTokens  int    `json:"cache_write_tokens"`
	RequestCount      int    `json:"request_count"`
	ProviderRequestID string `json:"provider_request_id,omitempty"`
	Error             string `json:"error,omitempty"`
}

// ContextStatePath returns the projection sidecar path for a session transcript.
func ContextStatePath(sessionPath string) string {
	return store.SessionContext(sessionPath)
}

// LoadCompactionState reads the context sidecar. Missing files return ok=false.
// Corrupt or unsupported schema returns an error so callers can drop and rebuild.
func LoadCompactionState(sessionPath string) (CompactionState, bool, error) {
	path := ContextStatePath(sessionPath)
	if path == "" {
		return CompactionState{}, false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CompactionState{}, false, nil
		}
		return CompactionState{}, false, err
	}
	var st CompactionState
	if err := json.Unmarshal(b, &st); err != nil {
		return CompactionState{}, false, fmt.Errorf("decode context state %s: %w", path, err)
	}
	if st.SchemaVersion != 0 && st.SchemaVersion != compactionStateSchemaV1 && st.SchemaVersion != compactionStateSchemaV2 && st.SchemaVersion != compactionStateSchemaV3 {
		return CompactionState{}, false, fmt.Errorf("unsupported context schema version %d", st.SchemaVersion)
	}
	if st.SchemaVersion == 0 {
		st.SchemaVersion = compactionStateSchemaV1
	}
	return st, true, nil
}

// SaveCompactionState writes the sidecar via strict atomic publish (temp +
// file fsync + rename + best-effort parent-dir fsync). Checkpoint sidecars are
// commit pointers: EXDEV/copy fallbacks that can tear an existing file are
// rejected so a failed write leaves the previous checkpoint intact. A returned
// error means the on-disk pointer was not published.
func SaveCompactionState(sessionPath string, st CompactionState) error {
	path := ContextStatePath(sessionPath)
	if path == "" {
		return fmt.Errorf("empty session path")
	}
	// V3 keeps logical user-turn boundaries; previous readers fall back to
	// canonical history rather than misreading the V1 coalesced invariant.
	st.SchemaVersion = compactionStateSchemaCurrent
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	// LastReceipt is authoritative. Drop mirrored top-level last_*/blocked_*
	// writer fields so new sidecars do not re-emit the pre-v3 dual schema.
	// Old files with those keys still decode into the struct for readers.
	st.LastTrigger = ""
	st.LastMode = ""
	st.LastSourceTokens = 0
	st.LastResultTokens = 0
	st.LastCompactionCost = 0
	if st.LastReceipt != nil {
		st.BlockedInputHash = ""
		st.BlockedReason = ""
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fileutil.AtomicWriteFileStrict(path, b, 0o644)
}

// RemoveCompactionState deletes a corrupt or invalidated projection sidecar.
func RemoveCompactionState(sessionPath string) error {
	path := ContextStatePath(sessionPath)
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// summaryContentHash fingerprints a compaction summary for projection metadata.
func summaryContentHash(summary string) string {
	if summary == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(summary))
	return hex.EncodeToString(sum[:16])
}

// coveredPrefixHash fingerprints the provider-visible prefix of msgs[:n].
// LocalOnly and transport-only fields are stripped via ModelMessages; every
// remaining provider-visible field is included so edits to images, signed
// reasoning, Responses items, or thought signatures invalidate the projection.
func coveredPrefixHash(msgs []provider.Message, n int) string {
	if n <= 0 || n > len(msgs) {
		return ""
	}
	return providerVisibleFingerprint(provider.ModelMessages(msgs[:n]))
}

// providerVisibleFingerprint is the stable hash of fields that reach a provider.
func providerVisibleFingerprint(msgs []provider.Message) string {
	type wireCall struct {
		ID               string `json:"id,omitempty"`
		Name             string `json:"name,omitempty"`
		Arguments        string `json:"args,omitempty"`
		ThoughtSignature string `json:"ts,omitempty"`
	}
	type wireMsg struct {
		Role               string                      `json:"r"`
		Content            string                      `json:"c,omitempty"`
		Images             []string                    `json:"img,omitempty"`
		ReasoningContent   string                      `json:"rc,omitempty"`
		ReasoningID        string                      `json:"rid,omitempty"`
		ReasoningStatus    string                      `json:"rst,omitempty"`
		ReasoningSignature string                      `json:"rsig,omitempty"`
		ToolCallID         string                      `json:"tid,omitempty"`
		Name               string                      `json:"n,omitempty"`
		ToolCalls          []wireCall                  `json:"tc,omitempty"`
		ResponsesItems     []json.RawMessage           `json:"ri,omitempty"`
		ServerSearch       []provider.ServerSearchCall `json:"ss,omitempty"`
	}
	wire := make([]wireMsg, 0, len(msgs))
	for _, m := range msgs {
		wm := wireMsg{
			Role:               string(m.Role),
			Content:            m.Content,
			Images:             append([]string(nil), m.Images...),
			ReasoningContent:   m.ReasoningContent,
			ReasoningID:        m.ReasoningID,
			ReasoningStatus:    m.ReasoningStatus,
			ReasoningSignature: m.ReasoningSignature,
			ToolCallID:         m.ToolCallID,
			Name:               m.Name,
		}
		for _, tc := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, wireCall{
				ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments, ThoughtSignature: tc.ThoughtSignature,
			})
		}
		if len(m.ResponsesItems) > 0 {
			wm.ResponsesItems = make([]json.RawMessage, len(m.ResponsesItems))
			for i, item := range m.ResponsesItems {
				wm.ResponsesItems[i] = append(json.RawMessage(nil), item...)
			}
		}
		if len(m.ServerSearch) > 0 {
			wm.ServerSearch = append([]provider.ServerSearchCall(nil), m.ServerSearch...)
		}
		wire = append(wire, wm)
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// projectionValid reports whether st can be reused for the current transcript
// and provider/model lineage. Fail closed: missing CoveredPrefixHash or a blank
// sidecar PromptCacheKey when the current lineage key is known forces rebuild.
func projectionValid(st CompactionState, msgs []provider.Message, transcriptVersion uint64, cacheKey string) bool {
	if len(st.Projection.Messages) == 0 {
		return false
	}
	// Current lineage known: stored key must match (legacy native suffix ok).
	if cacheKey != "" {
		if _, ok := lineageKeyCompatible(st.PromptCacheKey, cacheKey); !ok {
			return false
		}
	}
	return projectionContentValid(st, msgs, transcriptVersion)
}

// projectionContentValid reports whether st's projection body still matches the
// canonical transcript, independent of provider/model lineage. LoadProjectionSidecar
// uses it to rebind across upgrade/model/workspace key changes.
func projectionContentValid(st CompactionState, msgs []provider.Message, transcriptVersion uint64) bool {
	if len(st.Projection.Messages) == 0 {
		return false
	}
	n := st.Projection.CoveredCount
	if n <= 0 || n > len(msgs) {
		return false
	}
	// Prefix hash is required; legacy sidecars without it are rebuilt.
	if st.Projection.CoveredPrefixHash == "" {
		return false
	}
	if coveredPrefixHash(msgs, n) != st.Projection.CoveredPrefixHash {
		return false
	}
	if st.TranscriptVersion == transcriptVersion || st.Projection.TranscriptVersion == transcriptVersion {
		return true
	}
	// Append-only growth with a verified covered prefix.
	return n < len(msgs)
}

// modelVisibleFromProjection splices the projection with any messages appended
// after it was built. LocalOnly messages stay excluded via ModelMessages later.
func modelVisibleFromProjection(proj ContextProjection, canonical []provider.Message) []provider.Message {
	if len(proj.Messages) == 0 {
		return nil
	}
	out := append([]provider.Message(nil), proj.Messages...)
	if proj.CoveredCount >= 0 && proj.CoveredCount < len(canonical) {
		out = append(out, canonical[proj.CoveredCount:]...)
	}
	return out
}

// coalesceProjectionUserRuns keeps provider request copies compatible with
// providers that require strict user/assistant alternation. Projection
// sidecars retain logical user-turn boundaries; only the outbound copy is
// merged, leaving canonical history and range anchors untouched.
func coalesceProjectionUserRuns(msgs []provider.Message) []provider.Message {
	if len(msgs) < 2 {
		return msgs
	}
	out := make([]provider.Message, 0, len(msgs))
	for _, msg := range msgs {
		if len(out) == 0 || msg.Role != provider.RoleUser || out[len(out)-1].Role != provider.RoleUser {
			clone := msg
			clone.Images = append([]string(nil), msg.Images...)
			clone.ToolCalls = append([]provider.ToolCall(nil), msg.ToolCalls...)
			clone.ResponsesItems = append([]json.RawMessage(nil), msg.ResponsesItems...)
			clone.ServerSearch = append([]provider.ServerSearchCall(nil), msg.ServerSearch...)
			out = append(out, clone)
			continue
		}

		prev := &out[len(out)-1]
		if isCompactionSummary(msg) && !isCompactionSummary(*prev) {
			prev.Content = strings.TrimRight(msg.Content, "\n") + "\n\n" + prev.Content
		} else {
			prev.Content = strings.TrimRight(prev.Content, "\n") + "\n\n" + msg.Content
		}
		prev.Images = append(prev.Images, msg.Images...)
		prev.ToolCalls = append(prev.ToolCalls, msg.ToolCalls...)
		prev.ResponsesItems = append(prev.ResponsesItems, msg.ResponsesItems...)
		prev.ServerSearch = append(prev.ServerSearch, msg.ServerSearch...)
	}
	return out
}

// formatSummaryMessage builds the stable user-turn wrapper around a digest.
func formatSummaryMessage(summary string) provider.Message {
	return provider.Message{
		Role: provider.RoleUser,
		Content: summaryTagOpen + "\n" +
			"Summary of earlier conversation (older messages were compacted to save context):\n" +
			summary + "\n" +
			summaryTagClose,
	}
}
