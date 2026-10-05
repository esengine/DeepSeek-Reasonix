package sessionstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// CompactionState is the session context sidecar payload.
type CompactionState struct {
	SchemaVersion int `json:"schema_version"`
	// TranscriptVersion is the in-process CAS counter this state was written
	// under: diagnostics and compaction transaction metadata, never durable
	// projection identity. See ContextProjection.TranscriptVersion.
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
	Recall             RecallLedger               `json:"recall,omitempty"`
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

// ErrContextSchemaUnsupported marks a context sidecar whose schema this build
// cannot interpret. The file belongs to whichever release wrote it.
var ErrContextSchemaUnsupported = errors.New("unsupported context schema")

// LoadCompactionState reads the context sidecar. Missing files return ok=false.
// Corrupt or unsupported state returns an error; the caller rebuilds from the
// canonical transcript and leaves the file alone.
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
	switch st.SchemaVersion {
	case 0:
		st.SchemaVersion = CompactionStateSchemaV1
	case CompactionStateSchemaV1, compactionStateSchemaV2, compactionStateSchemaV3:
	case compactionStateSchema1xV4:
		if err := check1xV4Projection(b); err != nil {
			return CompactionState{}, false, fmt.Errorf("context state %s: %w", path, err)
		}
	default:
		return CompactionState{}, false, fmt.Errorf("context state %s: %w version %d", path, ErrContextSchemaUnsupported, st.SchemaVersion)
	}
	return st, true, nil
}

// check1xV4Projection admits a 1.x schema 4 sidecar whose projection carries no
// pinned-context checkpoint. A checkpoint is authenticated against 1.x's pinned
// revision provenance, which this build does not model, so it cannot be trusted.
func check1xV4Projection(b []byte) error {
	var v4 struct {
		Projection struct {
			PinnedContextHash string `json:"pinned_context_hash"`
		} `json:"projection"`
	}
	if err := json.Unmarshal(b, &v4); err != nil {
		return err
	}
	if v4.Projection.PinnedContextHash != "" {
		return fmt.Errorf("%w: 1.x schema 4 with a pinned-context checkpoint", ErrContextSchemaUnsupported)
	}
	return nil
}

// Foreign reports whether another release line wrote st. Its file is read,
// never rewritten in place by a load.
func (st CompactionState) Foreign() bool {
	return st.SchemaVersion > CompactionStateSchemaCurrent
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
	st.SchemaVersion = CompactionStateSchemaCurrent
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

// CoveredPrefixHash fingerprints the provider-visible prefix of msgs[:n].
// LocalOnly and transport-only fields are stripped via ModelMessages; every
// remaining provider-visible field is included so edits to images, signed
// reasoning, Responses items, or thought signatures invalidate the projection.
func CoveredPrefixHash(msgs []provider.Message, n int) string {
	if n <= 0 || n > len(msgs) {
		return ""
	}
	return ProviderVisibleFingerprint(provider.ModelMessages(msgs[:n]))
}

// Context-projection schema versions. Readers accept any known version;
// writers always emit the current schema.
const (
	CompactionStateSchemaV1      = 1
	compactionStateSchemaV2      = 2
	compactionStateSchemaV3      = 3
	CompactionStateSchemaCurrent = compactionStateSchemaV3
	// compactionStateSchema1xV4 is what 1.x writes from 1.36.0: schema 3 plus
	// projection.pinned_context_hash. A 2.x schema bump must not reuse 4.
	compactionStateSchema1xV4 = 4
)

// ContextProjection is the model-visible view of a session. The canonical
// transcript in Session.Messages is never replaced by this structure.
type ContextProjection struct {
	Messages []provider.Message `json:"messages"`
	// TranscriptVersion is diagnostic and in-process bookkeeping, never a
	// durable identity for this projection's content: it is a Session counter a
	// reload restarts. CoveredPrefixHash is what proves coverage.
	TranscriptVersion uint64 `json:"transcript_version"`
	ProjectionVersion uint64 `json:"projection_version"`
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
	InputTokens         int       `json:"input_tokens,omitempty"` // context worked on, not what the work cost
	ResultTokens        int       `json:"result_tokens,omitempty"`
	SavedTokens         int       `json:"saved_tokens,omitempty"`
	AffectedToolResults int       `json:"affected_tool_results,omitempty"`
	SummaryHash         string    `json:"summary_hash,omitempty"`
	CacheBreak          bool      `json:"cache_break,omitempty"`
	Reason              string    `json:"reason,omitempty"`
	Code                string    `json:"code,omitempty"`
	Boundary            string    `json:"boundary,omitempty"`
	TriggerTokens       int       `json:"trigger_tokens,omitempty"`
	BlockedInputHash    string    `json:"blocked_input_hash,omitempty"`
	CreatedAt           time.Time `json:"created_at,omitempty"`
	// What this maintenance actually spent with the provider. Every summary
	// call it made, including any whose answer it discarded.
	SummaryUsage CompactionUsage `json:"summary_usage,omitzero"`
}

// RecallLedger is what one projection generation has already pulled back out
// of the fold. Carrying the generation it belongs to is what resets the budget:
// a stale generation reads as an unspent one, with no reset call to forget.
type RecallLedger struct {
	Generation  uint64 `json:"generation,omitempty"`
	SpentTokens int    `json:"spent_tokens,omitempty"`
}

// ContextStatePath returns the projection sidecar path for a session transcript.
func ContextStatePath(sessionPath string) string {
	return store.SessionContext(sessionPath)
}

// RemoveCompactionState deletes an invalidated projection sidecar.
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

// ProviderVisibleFingerprint is the stable hash of fields that reach a provider.
func ProviderVisibleFingerprint(msgs []provider.Message) string {
	type wireCall struct {
		ID               string `json:"id,omitempty"`
		Name             string `json:"name,omitempty"`
		Arguments        string `json:"args,omitempty"`
		ThoughtSignature string `json:"ts,omitempty"`
	}
	type wireMsg struct {
		Role               string            `json:"r"`
		Content            string            `json:"c,omitempty"`
		Images             []string          `json:"img,omitempty"`
		ReasoningContent   string            `json:"rc,omitempty"`
		ReasoningID        string            `json:"rid,omitempty"`
		ReasoningStatus    string            `json:"rst,omitempty"`
		ReasoningSignature string            `json:"rsig,omitempty"`
		ToolCallID         string            `json:"tid,omitempty"`
		Name               string            `json:"n,omitempty"`
		ToolCalls          []wireCall        `json:"tc,omitempty"`
		ResponsesItems     []json.RawMessage `json:"ri,omitempty"`
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
		wire = append(wire, wm)
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// CompactionUsage is what one maintenance transaction spent with the provider:
// every summarizer call it made, including the ones whose answer was thrown
// away. It is not the size of the context that was worked on — a receipt's
// InputTokens is that, and the two are routinely orders apart.
type CompactionUsage struct {
	// Calls is billed summarizer calls. A call the provider never charged for
	// is not one, which is what makes this comparable to the usage ledger.
	Calls int `json:"calls,omitempty"`
	// RequestAttempts is the provider requests those calls represent: a call
	// the provider retried internally is one call and several requests.
	RequestAttempts  int `json:"request_attempts,omitempty"`
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	CacheHitTokens   int `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens  int `json:"cache_miss_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}
