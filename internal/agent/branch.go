package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/store"
)

// BranchMeta is the small sidecar record that turns flat session files into a
// navigable conversation tree. The conversation itself remains in the .jsonl
// file; metadata lives beside it at <session>.meta.
type BranchMeta struct {
	ID               string    `json:"id"`
	Name             string    `json:"name,omitempty"`
	ParentID         string    `json:"parent_id,omitempty"`
	ForkTurn         int       `json:"fork_turn,omitempty"`
	ForkMessageIndex int       `json:"fork_message_index,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Scope            string    `json:"scope,omitempty"`
	WorkspaceRoot    string    `json:"workspace_root,omitempty"`
	TopicID          string    `json:"topic_id,omitempty"`
	TopicTitle       string    `json:"topic_title,omitempty"`
	CustomTitle      string    `json:"custom_title,omitempty"`
	Model            string    `json:"model,omitempty"`
	// TokenMode is the legacy dual-write value (economy|full|delivery). Prefer
	// AgentPreset (light|balanced|delivery) when both are present.
	TokenMode string `json:"token_mode,omitempty"`
	// AgentPreset is the session role setting (角色设定): light|balanced|delivery.
	AgentPreset      string `json:"agent_preset,omitempty"`
	Mode             string `json:"mode,omitempty"`
	ToolApprovalMode string `json:"tool_approval_mode,omitempty"`
	Goal             string `json:"goal,omitempty"`
	Recovered        bool   `json:"recovered,omitempty"`
	RecoveryReason   string `json:"recovery_reason,omitempty"`
	RecoveryDigest   string `json:"recovery_digest,omitempty"`
	// RecoveryDepth counts how many recovery forks separate this branch from a
	// normal session (1 = forked from a normal session). SaveRecoveryBranch
	// refuses to fork past SessionRecoveryMaxDepth so a conflict loop cannot
	// spawn unbounded nested recovery chains (#5993 reached 8 levels). Legacy
	// recovery metas without the field are treated as depth 1.
	RecoveryDepth int `json:"recovery_depth,omitempty"`
	// RecoveryPreferred is a user's explicit choice among genuinely diverged
	// recovery leaves. It changes the default open target, but never authorizes
	// deletion and is cleared automatically if that leaf is no longer valid.
	RecoveryPreferred       bool   `json:"recovery_preferred,omitempty"`
	RecoveryPreferredDigest string `json:"recovery_preferred_digest,omitempty"`
	Revision                int64  `json:"revision,omitempty"`
	ContentDigest           string `json:"content_digest,omitempty"`
	WriterID                string `json:"writer_id,omitempty"`
	// SchemaVersion records the BranchMeta version that last wrote the listing
	// fields (Turns/Preview) FROM the session's content. It is stamped only by the
	// writers that actually derive those counts — Controller.snapshot's
	// UpdateSessionMeta and Fork/Branch — never by EnsureBranchMeta / TouchBranchMeta
	// / rename / set-model, which don't know the turn count. ListSessions trusts
	// positive v1 counts, but revalidates a v1 zero once because old preview errors
	// could be cached as empty. Current-version zero counts are authoritative.
	SchemaVersion int `json:"schema_version,omitempty"`
	// Turns and Preview are listing-only fields the desktop sidebar and CLI
	// pickers show ("5 turns · 'help me debug…'") without decoding the whole
	// .jsonl. The autosave path (Controller.snapshot) keeps them fresh from the
	// in-memory conversation, so ListSessions stays O(1) per session instead of
	// O(file size). SchemaVersion distinguishes a current, validated zero from an
	// old zero that may have swallowed a decode error.
	Turns        int               `json:"turns,omitempty"`
	Preview      string            `json:"preview,omitempty"`
	InFlightTurn *InFlightTurnMeta `json:"in_flight_turn,omitempty"`
}

const (
	// branchMetaCountsInitialVersion introduced content-derived Turns/Preview.
	// Positive counts from this version remain authoritative.
	branchMetaCountsInitialVersion = 1
	// BranchMetaCountsVersion certifies that zero turns came from a successful,
	// error-aware decode. Version 1 could cache a preview failure as zero turns.
	BranchMetaCountsVersion = 2
)

// InFlightTurnMeta records the message-log boundary for a foreground turn that
// has started but not yet reached TurnDone. If the process exits mid-turn, a
// later resume can strip the partial assistant/tool tail without guessing.
type InFlightTurnMeta struct {
	// ID makes marker cleanup compare-and-clear. Older sidecars omit it and are
	// handled by the legacy index/time recovery path.
	ID                string    `json:"id,omitempty"`
	StartMessageIndex int       `json:"start_message_index"`
	PreserveUser      bool      `json:"preserve_user"`
	StartedAt         time.Time `json:"started_at"`
	// StartRevision and StartDigest bind the legacy array boundary to the
	// persisted transcript that existed when the turn began.
	StartRevision int64  `json:"start_revision,omitempty"`
	StartDigest   string `json:"start_digest,omitempty"`
	// CommitDigest is written before the final turn snapshot. If recovery sees
	// this exact transcript on disk, the snapshot committed and only marker
	// cleanup was interrupted; no message recovery is necessary.
	CommitDigest string `json:"commit_digest,omitempty"`
}

func (m BranchMeta) DefaultScope() string {
	switch m.Scope {
	case "project":
		return "project"
	default:
		return "global"
	}
}

// BranchInfo combines sidecar metadata with the session file details needed for
// pickers and tree rendering.
type BranchInfo struct {
	BranchMeta
	Path    string
	ModTime time.Time
	Preview string
	Turns   int
}

func BranchID(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func BranchMetaPath(sessionPath string) string {
	return store.SessionMeta(sessionPath)
}

func LoadBranchMeta(sessionPath string) (BranchMeta, bool, error) {
	metaPath := BranchMetaPath(sessionPath)
	if metaPath == "" {
		return BranchMeta{}, false, nil
	}
	b, err := fileencoding.ReadFileUTF8(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return BranchMeta{}, false, nil
		}
		return BranchMeta{}, false, err
	}
	var m BranchMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return BranchMeta{}, false, fmt.Errorf("decode branch meta %s: %w", metaPath, err)
	}
	if m.ID == "" {
		m.ID = BranchID(sessionPath)
	}
	m.sanitizeDisplayFields()
	return m, true, nil
}

// sanitizeDisplayFields cleans persisted display strings that older builds
// polluted with internal wrappers (memory-compiler execution contracts,
// transient blocks) — #5666. Every reader goes through LoadBranchMeta, so this
// is the single boundary; UserPreviewText is a no-op on clean text, and a
// field that was pure wrapper falls back to empty so callers use their normal
// fallbacks (preview, default title).
func (m *BranchMeta) sanitizeDisplayFields() {
	m.TopicTitle = sanitizeStoredDisplayText(m.TopicTitle)
	m.CustomTitle = sanitizeStoredDisplayText(m.CustomTitle)
	m.Preview = sanitizeStoredDisplayText(m.Preview)
}

func sanitizeStoredDisplayText(s string) string {
	if strings.TrimSpace(s) == "" {
		return strings.TrimSpace(s)
	}
	return UserPreviewText(s)
}

// branchMetaReadBackoffs paces the re-reads of a branch-meta sidecar that
// failed to load. On Windows fileutil.ReplaceFile can fall back to a
// non-atomic in-place copy, so a concurrent reader may catch the sidecar
// half-written (an open/read error or truncated JSON). Those tears heal in
// milliseconds; a few short retries separate them from real corruption.
var branchMetaReadBackoffs = []time.Duration{20 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond}

// loadBranchMetaRetry reads the branch-meta sidecar like LoadBranchMeta but
// retries transient failures (I/O errors and undecodable JSON) before giving
// up. A missing sidecar is a legitimate state — a session that has never
// recorded meta — and returns ok=false immediately without retrying.
func loadBranchMetaRetry(sessionPath string) (BranchMeta, bool, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		meta, ok, err := LoadBranchMeta(sessionPath)
		if err == nil {
			return meta, ok, nil
		}
		lastErr = err
		if attempt >= len(branchMetaReadBackoffs) {
			return BranchMeta{}, false, lastErr
		}
		time.Sleep(branchMetaReadBackoffs[attempt])
	}
}

func SaveBranchMeta(sessionPath string, m BranchMeta) error {
	return UpdateBranchMeta(sessionPath, true, func(current *BranchMeta) error {
		preserveBranchMetaPersistence(&m, *current)
		*current = m
		return nil
	})
}

// saveBranchMetaKeepInFlightTurn keeps any existing in-flight turn on rewrite.
func saveBranchMetaKeepInFlightTurn(sessionPath string, m BranchMeta) error {
	return UpdateBranchMeta(sessionPath, true, func(current *BranchMeta) error {
		if m.InFlightTurn == nil {
			m.InFlightTurn = current.InFlightTurn
		}
		preserveBranchMetaPersistence(&m, *current)
		*current = m
		return nil
	})
}

func SaveBranchMetaPreserveUpdated(sessionPath string, m BranchMeta) error {
	return UpdateBranchMeta(sessionPath, false, func(current *BranchMeta) error {
		preserveBranchMetaPersistence(&m, *current)
		*current = m
		return nil
	})
}

// SaveBranchMetaPreserveUpdatedLocked is for callers that already hold
// LockSessionMetaPath for a larger read-modify-write transaction.
func SaveBranchMetaPreserveUpdatedLocked(sessionPath string, m BranchMeta) error {
	return saveBranchMeta(sessionPath, m, false)
}

func saveBranchMeta(sessionPath string, m BranchMeta, touchUpdated bool) error {
	metaPath := BranchMetaPath(sessionPath)
	if metaPath == "" {
		return fmt.Errorf("empty session path")
	}
	now := time.Now().UTC()
	if m.ID == "" {
		m.ID = BranchID(sessionPath)
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if touchUpdated || m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	if existing, ok, err := LoadBranchMeta(sessionPath); err == nil && ok {
		preserveBranchMetaPersistence(&m, existing)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(metaPath), ".branch.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, metaPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func preserveBranchMetaPersistence(next *BranchMeta, existing BranchMeta) {
	if next == nil {
		return
	}
	if existing.Revision > next.Revision {
		next.Revision = existing.Revision
		next.ContentDigest = existing.ContentDigest
		next.WriterID = existing.WriterID
		return
	}
	if existing.Revision == next.Revision {
		if strings.TrimSpace(next.ContentDigest) == "" {
			next.ContentDigest = existing.ContentDigest
		}
		if strings.TrimSpace(next.WriterID) == "" {
			next.WriterID = existing.WriterID
		}
	}
}

func EnsureBranchMeta(sessionPath string) (BranchMeta, error) {
	var out BranchMeta
	err := UpdateBranchMeta(sessionPath, false, func(m *BranchMeta) error {
		out = *m
		return nil
	})
	return out, err
}

// EnsureBranchMetaLocked is for callers that already hold LockSessionMetaPath.
func EnsureBranchMetaLocked(sessionPath string) (BranchMeta, error) {
	return ensureBranchMetaUnlocked(sessionPath)
}

func ensureBranchMetaUnlocked(sessionPath string) (BranchMeta, error) {
	if sessionPath == "" {
		return BranchMeta{}, fmt.Errorf("empty session path")
	}
	if m, ok, err := LoadBranchMeta(sessionPath); err != nil || ok {
		return m, err
	}
	when := time.Now().UTC()
	if info, err := os.Stat(sessionPath); err == nil {
		when = info.ModTime().UTC()
	}
	m := BranchMeta{
		ID:        BranchID(sessionPath),
		CreatedAt: when,
		UpdatedAt: when,
	}
	return m, saveBranchMeta(sessionPath, m, false)
}

func TouchBranchMeta(sessionPath string) error {
	return UpdateBranchMeta(sessionPath, false, func(m *BranchMeta) error {
		m.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func MarkSessionInFlightTurn(sessionPath string, startMessageIndex int, preserveUser bool) error {
	_, err := BeginSessionInFlightTurn(sessionPath, startMessageIndex, preserveUser)
	return err
}

var inFlightTurnSequence atomic.Uint64

// BeginSessionInFlightTurn writes a new marker and returns the exact marker so
// the owner can later clear only this turn. The baseline fields are learned from
// the branch sidecar before replacing its marker.
func BeginSessionInFlightTurn(sessionPath string, startMessageIndex int, preserveUser bool) (InFlightTurnMeta, error) {
	if sessionPath == "" {
		return InFlightTurnMeta{}, fmt.Errorf("empty session path")
	}
	// Read the baseline and install the marker under the same in-process save
	// lock. Otherwise an autosave can advance the revision between the read and
	// SetSessionInFlightTurn, leaving the marker bound to a stale baseline.
	unlock, err := LockSessionMetaPath(sessionPath)
	if err != nil {
		return InFlightTurnMeta{}, err
	}
	defer unlock()
	meta, err := ensureBranchMetaUnlocked(sessionPath)
	if err != nil {
		return InFlightTurnMeta{}, err
	}
	marker := InFlightTurnMeta{
		ID:                fmt.Sprintf("%s-%d-%d", SessionWriterID(), time.Now().UnixNano(), inFlightTurnSequence.Add(1)),
		StartMessageIndex: startMessageIndex,
		PreserveUser:      preserveUser,
		StartedAt:         time.Now().UTC(),
	}
	marker.StartRevision = meta.Revision
	marker.StartDigest = strings.TrimSpace(meta.ContentDigest)
	marker.StartMessageIndex = max(marker.StartMessageIndex, 0)
	meta.InFlightTurn = &marker
	if err := saveBranchMeta(sessionPath, meta, false); err != nil {
		return InFlightTurnMeta{}, err
	}
	return marker, nil
}

// SetSessionInFlightTurn writes an existing in-flight marker verbatim. It is
// used when a running turn moves to a recovery branch: preserving StartedAt is
// what lets crash recovery relocate the turn after an in-turn compaction has
// rewritten its original message index.
func SetSessionInFlightTurn(sessionPath string, marker InFlightTurnMeta) error {
	startMessageIndex := max(marker.StartMessageIndex, 0)
	// The sidecar is read-modify-write; the per-path save lock keeps concurrent
	// writers (autosave's UpdateSessionMeta, listing backfill) from dropping
	// each other's fields.
	unlock, err := LockSessionMetaPath(sessionPath)
	if err != nil {
		return err
	}
	defer unlock()
	m, err := ensureBranchMetaUnlocked(sessionPath)
	if err != nil {
		return err
	}
	marker.StartMessageIndex = startMessageIndex
	if marker.StartedAt.IsZero() {
		marker.StartedAt = time.Now().UTC()
	}
	m.InFlightTurn = &marker
	return saveBranchMeta(sessionPath, m, false)
}

func ClearSessionInFlightTurn(sessionPath string) error {
	_, err := ClearSessionInFlightTurnIfMatch(sessionPath, InFlightTurnMeta{})
	return err
}

// ClearSessionInFlightTurnIfMatch clears a marker only when it still matches
// expected. A non-empty ID is authoritative; legacy markers without IDs fall
// back to the complete persisted marker shape for compatibility.
func ClearSessionInFlightTurnIfMatch(sessionPath string, expected InFlightTurnMeta) (bool, error) {
	unlock, err := LockSessionMetaPath(sessionPath)
	if err != nil {
		return false, err
	}
	defer unlock()
	m, ok, err := LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return false, err
	}
	if m.InFlightTurn == nil {
		return false, nil
	}
	if expected.ID != "" {
		if m.InFlightTurn.ID != expected.ID {
			return false, nil
		}
	} else if expected.StartMessageIndex != 0 || expected.PreserveUser || !expected.StartedAt.IsZero() || expected.StartRevision != 0 || expected.StartDigest != "" {
		if !sameInFlightTurn(*m.InFlightTurn, expected) {
			return false, nil
		}
	}
	m.InFlightTurn = nil
	return true, saveBranchMeta(sessionPath, m, false)
}

// PrepareSessionInFlightTurnCommit binds the owned marker to the exact final
// transcript before that transcript is saved. Recovery can then distinguish a
// crash after the save from a crash during the turn without guessing from roles
// or array indexes.
func PrepareSessionInFlightTurnCommit(sessionPath string, expected InFlightTurnMeta, digest string) (InFlightTurnMeta, bool, error) {
	digest = strings.TrimSpace(digest)
	if sessionPath == "" || expected.ID == "" || digest == "" {
		return InFlightTurnMeta{}, false, nil
	}
	unlock, err := LockSessionMetaPath(sessionPath)
	if err != nil {
		return InFlightTurnMeta{}, false, err
	}
	defer unlock()
	m, ok, err := LoadBranchMeta(sessionPath)
	if err != nil || !ok || m.InFlightTurn == nil {
		return InFlightTurnMeta{}, false, err
	}
	if m.InFlightTurn.ID != expected.ID {
		return InFlightTurnMeta{}, false, nil
	}
	updated := *m.InFlightTurn
	updated.CommitDigest = digest
	m.InFlightTurn = &updated
	if err := saveBranchMeta(sessionPath, m, false); err != nil {
		return InFlightTurnMeta{}, false, err
	}
	return updated, true, nil
}

func sameInFlightTurn(a, b InFlightTurnMeta) bool {
	return a.ID == b.ID &&
		a.StartMessageIndex == b.StartMessageIndex &&
		a.PreserveUser == b.PreserveUser &&
		a.StartedAt.Equal(b.StartedAt) &&
		a.StartRevision == b.StartRevision &&
		a.StartDigest == b.StartDigest &&
		a.CommitDigest == b.CommitDigest
}

func ListBranches(dir string) ([]BranchInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []BranchInfo
	for _, e := range entries {
		if e.IsDir() || !store.IsSessionTranscriptName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if !IsVisibleSession(path) {
			continue
		}
		preview, turns := previewSession(path)
		if turns == 0 {
			continue
		}
		meta, ok, err := LoadBranchMeta(path)
		if err != nil {
			continue
		}
		if !ok {
			meta = BranchMeta{
				ID:        BranchID(path),
				CreatedAt: info.ModTime().UTC(),
				UpdatedAt: info.ModTime().UTC(),
			}
		}
		if meta.ID == "" {
			meta.ID = BranchID(path)
		}
		out = append(out, BranchInfo{
			BranchMeta: meta,
			Path:       path,
			ModTime:    info.ModTime(),
			Preview:    preview,
			Turns:      turns,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// RenameSession updates the user-chosen display title in the session's
// .jsonl.meta sidecar file. If no meta file exists yet, one is created. The
// topic title remains a separate grouping label, so explicit session names do
// not fight topic auto-titling.
func RenameSession(sessionPath string, title string) error {
	if sessionPath == "" {
		return fmt.Errorf("empty session path")
	}
	// Read-modify-write on the sidecar: hold the per-path meta lock so a
	// concurrent save (recordSessionContentRevision) can't have its Revision
	// bump clobbered by a stale read-back here.
	unlock, err := LockSessionMetaPath(sessionPath)
	if err != nil {
		return err
	}
	defer unlock()
	m, err := ensureBranchMetaUnlocked(sessionPath)
	if err != nil {
		return err
	}
	m.CustomTitle = strings.TrimSpace(title)
	return saveBranchMeta(sessionPath, m, false)
}

// LoadSessionModel reads the canonical provider/model ref saved beside a
// session transcript.
func LoadSessionModel(sessionPath string) (string, bool) {
	meta, ok, err := LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return "", false
	}
	model := strings.TrimSpace(meta.Model)
	if model == "" {
		return "", false
	}
	return model, true
}

// SetBranchModelPreserveUpdated stores the canonical provider/model ref without
// changing the session activity timestamp.
func SetBranchModelPreserveUpdated(sessionPath, model string) error {
	if sessionPath == "" {
		return fmt.Errorf("empty session path")
	}
	unlock, err := LockSessionMetaPath(sessionPath)
	if err != nil {
		return err
	}
	defer unlock()
	meta, err := ensureBranchMetaUnlocked(sessionPath)
	if err != nil {
		return err
	}
	meta.Model = strings.TrimSpace(model)
	return saveBranchMeta(sessionPath, meta, false)
}

// UpdateSessionMeta refreshes the listing-only sidecar fields (model, preview,
// user-turn count) the sidebar and pickers read without decoding the .jsonl.
// markActivity bumps UpdatedAt (the autosave path passes true on a real turn);
// false preserves it (used to backfill legacy sessions during a read). An empty
// model leaves the stored model untouched.
func UpdateSessionMeta(sessionPath, model, preview string, turns int, markActivity bool) error {
	if sessionPath == "" {
		return fmt.Errorf("empty session path")
	}
	unlock, err := LockSessionMetaPath(sessionPath)
	if err != nil {
		return err
	}
	defer unlock()
	m, err := ensureBranchMetaUnlocked(sessionPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(model) != "" {
		m.Model = strings.TrimSpace(model)
	}
	m.Preview = preview
	m.Turns = turns
	// These counts were derived from the current content, so mark them
	// authoritative — listing can then trust Turns (even 0) without re-decoding.
	m.SchemaVersion = BranchMetaCountsVersion
	return saveBranchMeta(sessionPath, m, markActivity)
}
