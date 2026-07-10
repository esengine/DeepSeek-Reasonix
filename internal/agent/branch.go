package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	TokenMode        string    `json:"token_mode,omitempty"`
	Mode             string    `json:"mode,omitempty"`
	ToolApprovalMode string    `json:"tool_approval_mode,omitempty"`
	Goal             string    `json:"goal,omitempty"`
	Recovered        bool      `json:"recovered,omitempty"`
	RecoveryReason   string    `json:"recovery_reason,omitempty"`
	RecoveryDigest   string    `json:"recovery_digest,omitempty"`
	// RecoveryDepth counts how many recovery forks separate this branch from a
	// normal session (1 = forked from a normal session). SaveRecoveryBranch
	// refuses to fork past SessionRecoveryMaxDepth so a conflict loop cannot
	// spawn unbounded nested recovery chains (#5993 reached 8 levels). Legacy
	// recovery metas without the field are treated as depth 1.
	RecoveryDepth int    `json:"recovery_depth,omitempty"`
	Revision      int64  `json:"revision,omitempty"`
	ContentDigest string `json:"content_digest,omitempty"`
	WriterID      string `json:"writer_id,omitempty"`
	// SchemaVersion records the BranchMeta version that last wrote the listing
	// fields (Turns/Preview) FROM the session's content. It is stamped only by the
	// writers that actually derive those counts — Controller.snapshot's
	// UpdateSessionMeta and Fork/Branch — never by EnsureBranchMeta / TouchBranchMeta
	// / rename / set-model, which don't know the turn count. So ListSessions can
	// tell a meta whose counts are authoritative (>= BranchMetaCountsVersion: trust
	// Turns even when 0 = genuinely empty) from a legacy/contentless one
	// (< version: decode once, then backfill + stamp).
	SchemaVersion int `json:"schema_version,omitempty"`
	// Turns and Preview are listing-only fields the desktop sidebar and CLI
	// pickers show ("5 turns · 'help me debug…'") without decoding the whole
	// .jsonl. The autosave path (Controller.snapshot) keeps them fresh from the
	// in-memory conversation, so ListSessions stays O(1) per session instead of
	// O(file size). Gated by SchemaVersion (above), not Turns == 0, so a
	// genuinely-empty session is recorded once and never re-decoded.
	Turns        int               `json:"turns,omitempty"`
	Preview      string            `json:"preview,omitempty"`
	InFlightTurn *InFlightTurnMeta `json:"in_flight_turn,omitempty"`
}

// BranchMetaCountsVersion is stamped into BranchMeta.SchemaVersion whenever a
// writer records Turns/Preview from session content (UpdateSessionMeta,
// Fork/Branch). Bump it when the meaning of those listing fields changes so
// existing listings re-derive them instead of trusting a stale cache.
const BranchMetaCountsVersion = 1

// InFlightTurnMeta records the message-log boundary for a foreground turn that
// has started but not yet reached TurnDone. If the process exits mid-turn, a
// later resume can strip the partial assistant/tool tail without guessing.
type InFlightTurnMeta struct {
	StartMessageIndex int       `json:"start_message_index"`
	PreserveUser      bool      `json:"preserve_user"`
	StartedAt         time.Time `json:"started_at"`
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
	return m, true, nil
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
	return saveBranchMeta(sessionPath, m, true)
}

func SaveBranchMetaPreserveUpdated(sessionPath string, m BranchMeta) error {
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
	unlock := lockSessionSavePath(sessionPath)
	defer unlock()
	m, err := EnsureBranchMeta(sessionPath)
	if err != nil {
		return err
	}
	m.UpdatedAt = time.Now().UTC()
	return saveBranchMeta(sessionPath, m, false)
}

func MarkSessionInFlightTurn(sessionPath string, startMessageIndex int, preserveUser bool) error {
	if startMessageIndex < 0 {
		startMessageIndex = 0
	}
	// The sidecar is read-modify-write; the per-path save lock keeps concurrent
	// writers (autosave's UpdateSessionMeta, listing backfill) from dropping
	// each other's fields.
	unlock := lockSessionSavePath(sessionPath)
	defer unlock()
	m, err := EnsureBranchMeta(sessionPath)
	if err != nil {
		return err
	}
	m.InFlightTurn = &InFlightTurnMeta{
		StartMessageIndex: startMessageIndex,
		PreserveUser:      preserveUser,
		StartedAt:         time.Now().UTC(),
	}
	return SaveBranchMetaPreserveUpdated(sessionPath, m)
}

func ClearSessionInFlightTurn(sessionPath string) error {
	unlock := lockSessionSavePath(sessionPath)
	defer unlock()
	m, ok, err := LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return err
	}
	if m.InFlightTurn == nil {
		return nil
	}
	m.InFlightTurn = nil
	return SaveBranchMetaPreserveUpdated(sessionPath, m)
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
	unlock := lockSessionSavePath(sessionPath)
	defer unlock()
	m, err := EnsureBranchMeta(sessionPath)
	if err != nil {
		return err
	}
	m.CustomTitle = strings.TrimSpace(title)
	return SaveBranchMetaPreserveUpdated(sessionPath, m)
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
	unlock := lockSessionSavePath(sessionPath)
	defer unlock()
	meta, err := EnsureBranchMeta(sessionPath)
	if err != nil {
		return err
	}
	meta.Model = strings.TrimSpace(model)
	return SaveBranchMetaPreserveUpdated(sessionPath, meta)
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
	unlock := lockSessionSavePath(sessionPath)
	defer unlock()
	m, err := EnsureBranchMeta(sessionPath)
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
