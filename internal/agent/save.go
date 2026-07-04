package agent

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

const cleanupPendingExt = ".cleanup-pending.json"

var (
	sessionSaveLocks            sync.Map
	ErrSessionSnapshotConflict  = errors.New("session snapshot conflicts with newer transcript")
	ErrSessionRecoveryNotNeeded = errors.New("session recovery not needed")
	sessionWriterID             = newSessionWriterID()
)

type sessionPersistState struct {
	path     string
	digest   [sha256.Size]byte
	version  uint64
	revision int64
	ok       bool
}

type sessionSaveMode int

const (
	sessionSaveForce sessionSaveMode = iota
	sessionSaveSnapshot
	sessionSaveRewrite
)

type SessionSnapshotConflictKind string

const (
	SessionSnapshotConflictStalePrefix SessionSnapshotConflictKind = "stale_prefix"
	SessionSnapshotConflictDiverged    SessionSnapshotConflictKind = "diverged"
)

type SessionSnapshotConflictError struct {
	Path             string
	Kind             SessionSnapshotConflictKind
	ExistingMessages int
	SnapshotMessages int
	BaseRevision     int64
	DiskRevision     int64
}

func (e *SessionSnapshotConflictError) Error() string {
	if e == nil {
		return ErrSessionSnapshotConflict.Error()
	}
	switch e.Kind {
	case SessionSnapshotConflictStalePrefix:
		return fmt.Sprintf("%s: %s has %d messages at revision %d; stale snapshot has %d messages from revision %d",
			ErrSessionSnapshotConflict, e.Path, e.ExistingMessages, e.DiskRevision, e.SnapshotMessages, e.BaseRevision)
	default:
		return fmt.Sprintf("%s: %s diverged on disk (%d messages, revision %d) from snapshot (%d messages, revision %d)",
			ErrSessionSnapshotConflict, e.Path, e.ExistingMessages, e.DiskRevision, e.SnapshotMessages, e.BaseRevision)
	}
}

func (e *SessionSnapshotConflictError) Unwrap() error {
	return ErrSessionSnapshotConflict
}

func SnapshotConflictKind(err error) (SessionSnapshotConflictKind, bool) {
	var conflict *SessionSnapshotConflictError
	if errors.As(err, &conflict) && conflict != nil {
		return conflict.Kind, true
	}
	return "", false
}

const RecoveryBranchDefaultName = "Recovered unsaved changes from stale runtime"

type RecoveryBranchOptions struct {
	OriginalPath string
	Name         string
	Reason       string
	BranchMeta   BranchMeta
}

type RecoveryBranchInfo struct {
	Path     string
	Digest   string
	Existing bool
	Meta     BranchMeta
	Preview  string
	Turns    int
}

// Save writes the session's messages to path in JSONL — one provider.Message
// per line — so a user can resume the conversation later. The file is
// rewritten in full on every save: chat sessions are small (kilobytes), and
// append-only would have to be reconciled with the compaction pass that
// mutates the middle of session.Messages.
func (s *Session) Save(path string) error {
	return s.save(path, sessionSaveForce)
}

// SaveSnapshot writes a normal autosave/snapshot only when doing so cannot hide
// a newer transcript already on disk. Explicit history rewrites such as rewind,
// compaction, and cancel recovery should call SaveRewrite instead.
func (s *Session) SaveSnapshot(path string) error {
	return s.save(path, sessionSaveSnapshot)
}

// SaveRewrite writes an intentional non-append history rewrite only while this
// Session still owns the current on-disk transcript baseline. It prevents a
// stale controller from force-rewinding a newer transcript written elsewhere.
func (s *Session) SaveRewrite(path string) error {
	return s.save(path, sessionSaveRewrite)
}

func (s *Session) save(path string, mode sessionSaveMode) error {
	if path == "" {
		return fmt.Errorf("empty session path")
	}
	msgs, version := s.snapshotWithVersion()
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		return err
	}
	baseRevision := int64(0)
	unlock := lockSessionSavePath(path)
	defer unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	unlockFile, err := lockSessionFile(path)
	if err != nil {
		return fmt.Errorf("lock session file: %w", err)
	}
	defer unlockFile()
	if mode != sessionSaveForce {
		revision, upToDate, err := s.checkSnapshotWrite(path, msgs, digest, version, mode == sessionSaveRewrite)
		if err != nil {
			return err
		}
		if upToDate {
			// Disk already holds exactly this transcript. Rewriting it would only
			// bump the revision, invalidating the persistence baseline of every
			// other runtime resumed on this file and turning their next
			// legitimate save into a stale-runtime conflict. Skip the write and
			// adopt the current on-disk revision as this session's baseline.
			s.markPersisted(path, digest, version, revision)
			return nil
		}
		baseRevision = revision
	} else if revision, _, err := sessionContentRevision(path); err != nil {
		return err
	} else {
		baseRevision = revision
	}
	if err := writeSessionMessages(path, msgs); err != nil {
		return err
	}
	revision, err := recordSessionContentRevision(path, digest, baseRevision)
	if err != nil {
		return err
	}
	s.markPersisted(path, digest, version, revision)
	return nil
}

func writeSessionMessages(path string, msgs []provider.Message) error {
	// Write to a sibling tmp file then rename, so a crash mid-write can't
	// leave a partial JSONL that won't reload.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session.*.tmp")
	if err != nil {
		return fmt.Errorf("create session tmp: %w", err)
	}
	tmpPath := tmp.Name()
	enc := json.NewEncoder(tmp)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("encode message: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// checkSnapshotWrite decides whether this session may write msgs over path.
// The bool result reports the write is a no-op: the on-disk transcript already
// matches next byte-for-byte, so the caller should skip the rewrite instead of
// burning a revision bump that would stale-out other runtimes' baselines.
func (s *Session) checkSnapshotWrite(path string, next []provider.Message, nextDigest [sha256.Size]byte, nextVersion uint64, allowOwnedRewrite bool) (int64, bool, error) {
	current, err := LoadSession(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	currentRevision, _, err := sessionContentRevision(path)
	if err != nil {
		return 0, false, err
	}
	baseState := s.persistState(path)
	existing := current.Snapshot()
	existingDigest, err := digestSessionMessages(existing)
	if err != nil {
		return 0, false, err
	}
	contentUnchanged := bytes.Equal(existingDigest[:], nextDigest[:])
	if contentUnchanged || messagesHavePrefix(next, existing) || messagesHavePrefixWithCompatibleSystem(next, existing) {
		if baseState.ok && currentRevision != baseState.revision && !contentUnchanged {
			return 0, false, snapshotConflict(path, existing, next, baseState.revision, currentRevision)
		}
		// A normalized-dirty load means LoadSession repaired the history on the
		// way in: the digests match but the raw bytes on disk do not, so the
		// repair still needs a real write to persist.
		return currentRevision, contentUnchanged && !current.normalizedDirty, nil
	}
	if allowOwnedRewrite && s.ownsPersistedState(path, existingDigest, currentRevision, nextVersion) {
		return currentRevision, false, nil
	}
	if messagesHavePrefix(existing, next) || messagesHavePrefixWithCompatibleSystem(existing, next) {
		return 0, false, &SessionSnapshotConflictError{
			Path:             path,
			Kind:             SessionSnapshotConflictStalePrefix,
			ExistingMessages: len(existing),
			SnapshotMessages: len(next),
			BaseRevision:     baseState.revision,
			DiskRevision:     currentRevision,
		}
	}
	return 0, false, &SessionSnapshotConflictError{
		Path:             path,
		Kind:             SessionSnapshotConflictDiverged,
		ExistingMessages: len(existing),
		SnapshotMessages: len(next),
		BaseRevision:     baseState.revision,
		DiskRevision:     currentRevision,
	}
}

func snapshotConflict(path string, existing, next []provider.Message, baseRevision, diskRevision int64) error {
	kind := SessionSnapshotConflictDiverged
	if messagesHavePrefix(existing, next) || messagesHavePrefixWithCompatibleSystem(existing, next) {
		kind = SessionSnapshotConflictStalePrefix
	}
	return &SessionSnapshotConflictError{
		Path:             path,
		Kind:             kind,
		ExistingMessages: len(existing),
		SnapshotMessages: len(next),
		BaseRevision:     baseRevision,
		DiskRevision:     diskRevision,
	}
}

func (s *Session) SaveRecoveryBranch(opts RecoveryBranchOptions) (RecoveryBranchInfo, error) {
	originalPath := strings.TrimSpace(opts.OriginalPath)
	if originalPath == "" {
		return RecoveryBranchInfo{}, fmt.Errorf("empty original session path")
	}
	msgs, version := s.snapshotWithVersion()
	preview, turns := SessionPreviewFromMessages(msgs)
	if turns == 0 {
		return RecoveryBranchInfo{}, ErrSessionRecoveryNotNeeded
	}
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		return RecoveryBranchInfo{}, err
	}
	digestText := digestString(digest)

	unlockOriginal := lockSessionSavePath(originalPath)
	unlockOriginalFile, lockErr := lockSessionFile(originalPath)
	if lockErr != nil {
		unlockOriginal()
		return RecoveryBranchInfo{}, fmt.Errorf("lock original session file: %w", lockErr)
	}
	current, err := LoadSession(originalPath)
	unlockOriginalFile()
	unlockOriginal()
	if err != nil && !os.IsNotExist(err) {
		return RecoveryBranchInfo{}, err
	}
	if err == nil && current != nil {
		existing := current.Snapshot()
		existingDigest, digestErr := digestSessionMessages(existing)
		if digestErr != nil {
			return RecoveryBranchInfo{}, digestErr
		}
		if bytes.Equal(existingDigest[:], digest[:]) ||
			messagesHavePrefix(existing, msgs) ||
			messagesHavePrefixWithCompatibleSystem(existing, msgs) {
			return RecoveryBranchInfo{}, ErrSessionRecoveryNotNeeded
		}
	}

	recoveryPath := recoverySessionPath(originalPath, digest)
	unlockRecovery := lockSessionSavePath(recoveryPath)
	defer unlockRecovery()
	unlockRecoveryFile, err := lockSessionFile(recoveryPath)
	if err != nil {
		return RecoveryBranchInfo{}, fmt.Errorf("lock recovery session file: %w", err)
	}
	defer unlockRecoveryFile()
	if loaded, loadErr := LoadSession(recoveryPath); loadErr == nil && loaded != nil {
		existingDigest, digestErr := digestSessionMessages(loaded.Snapshot())
		if digestErr != nil {
			return RecoveryBranchInfo{}, digestErr
		}
		if bytes.Equal(existingDigest[:], digest[:]) {
			meta, err := s.saveRecoveryBranchMeta(recoveryPath, opts, preview, turns, digestText)
			if err != nil {
				return RecoveryBranchInfo{}, err
			}
			s.markPersisted(recoveryPath, digest, version, meta.Revision)
			return RecoveryBranchInfo{Path: recoveryPath, Digest: digestText, Existing: true, Meta: meta, Preview: preview, Turns: turns}, nil
		}
	} else if loadErr != nil && !os.IsNotExist(loadErr) {
		return RecoveryBranchInfo{}, loadErr
	}

	if err := os.MkdirAll(filepath.Dir(recoveryPath), 0o755); err != nil {
		return RecoveryBranchInfo{}, fmt.Errorf("create recovery session dir: %w", err)
	}
	if err := writeSessionMessages(recoveryPath, msgs); err != nil {
		return RecoveryBranchInfo{}, err
	}
	meta, err := s.saveRecoveryBranchMeta(recoveryPath, opts, preview, turns, digestText)
	if err != nil {
		return RecoveryBranchInfo{}, err
	}
	s.markPersisted(recoveryPath, digest, version, meta.Revision)
	return RecoveryBranchInfo{Path: recoveryPath, Digest: digestText, Meta: meta, Preview: preview, Turns: turns}, nil
}

func (s *Session) saveRecoveryBranchMeta(path string, opts RecoveryBranchOptions, preview string, turns int, digest string) (BranchMeta, error) {
	meta := opts.BranchMeta
	meta.ID = BranchID(path)
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = firstNonEmpty(strings.TrimSpace(opts.Name), RecoveryBranchDefaultName)
	}
	if strings.TrimSpace(meta.ParentID) == "" {
		meta.ParentID = BranchID(opts.OriginalPath)
	}
	meta.ForkTurn = -1
	meta.ForkMessageIndex = len(s.Snapshot())
	meta.Preview = preview
	meta.Turns = turns
	meta.SchemaVersion = BranchMetaCountsVersion
	meta.Recovered = true
	meta.RecoveryReason = firstNonEmpty(strings.TrimSpace(opts.Reason), "session snapshot conflict")
	meta.RecoveryDigest = digest
	if meta.Revision == 0 {
		meta.Revision = 1
	}
	if strings.TrimSpace(meta.ContentDigest) == "" {
		meta.ContentDigest = digest
	}
	if strings.TrimSpace(meta.WriterID) == "" {
		meta.WriterID = SessionWriterID()
	}
	if err := SaveBranchMeta(path, meta); err != nil {
		return BranchMeta{}, err
	}
	if stored, ok, err := LoadBranchMeta(path); err != nil {
		return BranchMeta{}, err
	} else if ok {
		return stored, nil
	}
	return meta, nil
}

func recoverySessionPath(originalPath string, digest [sha256.Size]byte) string {
	parent := BranchID(originalPath)
	if parent == "" {
		parent = "session"
	}
	return filepath.Join(filepath.Dir(originalPath), fmt.Sprintf("%s-recovery-%x.jsonl", parent, digest[:8]))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Session) ownsPersistedState(path string, existingDigest [sha256.Size]byte, existingRevision int64, nextVersion uint64) bool {
	state := s.persistState(path)
	return state.ok &&
		state.version <= nextVersion &&
		state.revision == existingRevision &&
		bytes.Equal(existingDigest[:], state.digest[:])
}

func (s *Session) persistState(path string) sessionPersistState {
	key := canonicalSessionSavePath(path)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.persisted.ok && s.persisted.path == key {
		return s.persisted
	}
	return sessionPersistState{}
}

func (s *Session) markPersisted(path string, digest [sha256.Size]byte, version uint64, revision int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persisted = sessionPersistState{
		path:     canonicalSessionSavePath(path),
		digest:   digest,
		version:  version,
		revision: revision,
		ok:       true,
	}
}

func sessionContentRevision(path string) (int64, string, error) {
	meta, ok, err := LoadBranchMeta(path)
	if err != nil {
		return 0, "", nil
	}
	if !ok {
		return 0, "", nil
	}
	return meta.Revision, strings.TrimSpace(meta.ContentDigest), nil
}

func recordSessionContentRevision(path string, digest [sha256.Size]byte, baseRevision int64) (int64, error) {
	meta, ok, err := LoadBranchMeta(path)
	if err != nil {
		return baseRevision, nil
	}
	if !ok {
		meta = BranchMeta{ID: BranchID(path)}
	}
	if meta.Revision < baseRevision {
		meta.Revision = baseRevision
	}
	meta.Revision++
	meta.ContentDigest = digestString(digest)
	meta.WriterID = SessionWriterID()
	if err := SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		return 0, err
	}
	stored, ok, err := LoadBranchMeta(path)
	if err != nil {
		return 0, err
	}
	if ok && stored.Revision > 0 {
		return stored.Revision, nil
	}
	return meta.Revision, nil
}

func digestString(digest [sha256.Size]byte) string {
	return fmt.Sprintf("%x", digest[:])
}

func SessionWriterID() string {
	return sessionWriterID
}

func newSessionWriterID() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "unknown-host"
	}
	host = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, host)
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%d-%x", host, os.Getpid(), nonce[:])
}

func digestSessionMessages(msgs []provider.Message) ([sha256.Size]byte, error) {
	h := sha256.New()
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		if _, err := h.Write(b); err != nil {
			return [sha256.Size]byte{}, err
		}
		if _, err := h.Write([]byte{'\n'}); err != nil {
			return [sha256.Size]byte{}, err
		}
	}
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

func messagesHavePrefix(full, prefix []provider.Message) bool {
	if len(prefix) > len(full) {
		return false
	}
	for i := range prefix {
		if !messagesEqualForStorage(full[i], prefix[i]) {
			return false
		}
	}
	return true
}

func messagesHavePrefixWithCompatibleSystem(full, prefix []provider.Message) bool {
	full = messagesWithoutLeadingSystem(full)
	prefix = messagesWithoutLeadingSystem(prefix)
	return messagesHavePrefix(full, prefix)
}

func messagesWithoutLeadingSystem(msgs []provider.Message) []provider.Message {
	if len(msgs) > 0 && msgs[0].Role == provider.RoleSystem {
		return msgs[1:]
	}
	return msgs
}

func messagesEqualForStorage(a, b provider.Message) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

func messagesEqualForStorageList(a, b []provider.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !messagesEqualForStorage(a[i], b[i]) {
			return false
		}
	}
	return true
}

func messagesCompatibleForStorageBaseline(a, b []provider.Message) bool {
	if messagesEqualForStorageList(a, b) {
		return true
	}
	return messagesEqualForStorageList(messagesWithoutLeadingSystem(a), messagesWithoutLeadingSystem(b))
}

func lockSessionSavePath(path string) func() {
	key := canonicalSessionSavePath(path)
	v, _ := sessionSaveLocks.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func canonicalSessionSavePath(path string) string {
	key := filepath.Clean(strings.TrimSpace(path))
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

// LoadSession reads a JSONL file written by Save into a fresh Session value.
// Missing files surface as os.IsNotExist so callers can fall through to a
// new session.
func LoadSession(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Session{}
	// Decode a stream of JSON values rather than scanning lines: a single
	// message (e.g. a multi-MiB bash output) can exceed any line-buffer cap, and
	// Save's json.Encoder has no such limit — a Scanner here made sessions that
	// saved fine fail to reload.
	dec := json.NewDecoder(f)
	for {
		var m provider.Message
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		s.Messages = append(s.Messages, m)
	}
	// Repair persisted-history-safe issues before anything reads the session.
	// Old sessions (pre adde2d3e) and interrupted turns can carry empty tool-call
	// names, dangling tool_calls, or half-streamed argument JSON that DeepSeek
	// rejects with a 400 on replay. Wire-only cleanup, such as dropping orphan
	// tool messages, stays in the provider send path so Save/LoadSession keeps
	// its round-trip contract. The fast path returns the input slice unchanged
	// for a well-formed history, so we detect an actual repair by comparing
	// slice headers: when NormalizeSession allocated a new backing array, the
	// session is marked dirty so the next Save persists the fix.
	normalized := NormalizeSession(s.Messages)
	if len(normalized) != len(s.Messages) || (len(s.Messages) > 0 && &normalized[0] != &s.Messages[0]) {
		s.normalizedDirty = true
	}
	s.Messages = normalized
	if digest, err := digestSessionMessages(s.Messages); err == nil {
		revision := int64(0)
		if meta, ok, metaErr := LoadBranchMeta(path); metaErr == nil && ok {
			revision = meta.Revision
		}
		s.markPersisted(path, digest, s.version, revision)
	}
	return s, nil
}

// SessionInfo summarises a saved session for the --resume picker: where it is on
// disk, when it was created/last active, the first user message as a preview, and
// a rough turn count.
type SessionInfo struct {
	Path           string
	CreatedAt      time.Time
	LastActivityAt time.Time
	ModTime        time.Time // compatibility alias for LastActivityAt
	Preview        string
	Turns          int
	Scope          string
	WorkspaceRoot  string
	TopicID        string
	TopicTitle     string
	CustomTitle    string
	Recovered      bool
	RecoveryReason string
	RecoveryDigest string
	ParentID       string
}

// SessionOrderInfo is the lightweight sidecar/mtime ordering record shared by
// session pickers and prompt-history navigation. It intentionally avoids reading
// JSONL content; callers that need previews can layer that on afterwards.
type SessionOrderInfo struct {
	Path           string
	CreatedAt      time.Time
	LastActivityAt time.Time
	ModTime        time.Time // compatibility alias for LastActivityAt
	Scope          string
	WorkspaceRoot  string
	TopicID        string
	TopicTitle     string
	CustomTitle    string
	Recovered      bool
	RecoveryReason string
	RecoveryDigest string
	ParentID       string
	// Turns and Preview are the cached listing fields from the sidecar; SchemaVersion
	// >= agent.BranchMetaCountsVersion means they were recorded from content and can
	// be trusted (even Turns == 0). ListSessions uses them to skip the whole-file decode.
	Turns         int
	Preview       string
	SchemaVersion int
}

// CleanupPendingMeta records that a session was logically removed but still has
// artifacts waiting for a background job to unwind before physical cleanup.
type CleanupPendingMeta struct {
	Operation string `json:"operation"`
	CreatedAt int64  `json:"createdAt"`
}

// CleanupPendingInfo describes one durable delayed-cleanup marker and the
// session transcript it belongs to.
type CleanupPendingInfo struct {
	SessionPath string
	MarkerPath  string
	Meta        CleanupPendingMeta
}

// CleanupPendingPath returns the durable marker path for a session transcript.
func CleanupPendingPath(sessionPath string) string {
	return store.SessionCleanupPending(sessionPath)
}

// MarkCleanupPending hides a logically removed session from resume/list surfaces
// until delayed physical cleanup has finished.
func MarkCleanupPending(sessionPath, operation string) error {
	path := CleanupPendingPath(sessionPath)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	meta := CleanupPendingMeta{Operation: strings.TrimSpace(operation), CreatedAt: time.Now().UnixMilli()}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ClearCleanupPending removes a delayed-cleanup marker after physical cleanup.
func ClearCleanupPending(sessionPath string) error {
	path := CleanupPendingPath(sessionPath)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsCleanupPending reports whether a session is hidden pending delayed cleanup.
func IsCleanupPending(sessionPath string) bool {
	path := CleanupPendingPath(sessionPath)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// IsVisibleSession reports whether a persisted session should appear on normal
// user/agent-facing list, restore, and retrieval surfaces.
func IsVisibleSession(sessionPath string) bool {
	return strings.TrimSpace(sessionPath) != "" && !IsCleanupPending(sessionPath)
}

// ListCleanupPending returns delayed-cleanup markers left in dir. A missing
// directory is not an error.
func ListCleanupPending(dir string) ([]CleanupPendingInfo, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []CleanupPendingInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), cleanupPendingExt) {
			continue
		}
		markerPath := filepath.Join(dir, e.Name())
		var meta CleanupPendingMeta
		b, err := os.ReadFile(markerPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if strings.TrimSpace(string(b)) != "" {
			if err := json.Unmarshal(b, &meta); err != nil {
				return nil, fmt.Errorf("read cleanup-pending marker %s: %w", markerPath, err)
			}
		}
		name := strings.TrimSuffix(e.Name(), cleanupPendingExt) + ".jsonl"
		out = append(out, CleanupPendingInfo{
			SessionPath: filepath.Join(dir, name),
			MarkerPath:  markerPath,
			Meta:        meta,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SessionPath < out[j].SessionPath
	})
	return out, nil
}

// ReconcileCleanupPending retries physical cleanup for leftover delayed-cleanup
// markers. It keeps going after individual cleanup errors and returns them joined.
func ReconcileCleanupPending(dir string, cleanup func(CleanupPendingInfo) error) error {
	pending, err := ListCleanupPending(dir)
	if err != nil {
		return err
	}
	if cleanup == nil {
		return nil
	}
	var errs []error
	for _, item := range pending {
		if err := cleanup(item); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", item.SessionPath, err))
		}
	}
	return errors.Join(errs...)
}

// ListSessionOrder returns every *.jsonl session under dir in the same
// most-recently-active order used by ListSessions, using only file metadata and
// branch sidecars. A missing directory is not an error.
func ListSessionOrder(dir string) ([]SessionOrderInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SessionOrderInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if !IsVisibleSession(full) {
			continue
		}
		createdAt := info.ModTime()
		lastActivityAt := info.ModTime()
		scope := "global"
		workspaceRoot := ""
		topicID := ""
		topicTitle := ""
		customTitle := ""
		recovered := false
		recoveryReason := ""
		recoveryDigest := ""
		parentID := ""
		turns := 0
		preview := ""
		schemaVersion := 0
		if meta, ok, err := LoadBranchMeta(full); err == nil && ok {
			if !meta.CreatedAt.IsZero() {
				createdAt = meta.CreatedAt
			}
			if !meta.UpdatedAt.IsZero() {
				lastActivityAt = meta.UpdatedAt
			}
			scope = meta.DefaultScope()
			workspaceRoot = meta.WorkspaceRoot
			topicID = meta.TopicID
			topicTitle = meta.TopicTitle
			customTitle = meta.CustomTitle
			recovered = meta.Recovered
			recoveryReason = meta.RecoveryReason
			recoveryDigest = meta.RecoveryDigest
			parentID = meta.ParentID
			turns = meta.Turns
			preview = meta.Preview
			schemaVersion = meta.SchemaVersion
		}
		out = append(out, SessionOrderInfo{
			Path:           full,
			CreatedAt:      createdAt,
			LastActivityAt: lastActivityAt,
			ModTime:        lastActivityAt,
			Scope:          scope,
			WorkspaceRoot:  workspaceRoot,
			TopicID:        topicID,
			TopicTitle:     topicTitle,
			CustomTitle:    customTitle,
			Recovered:      recovered,
			RecoveryReason: recoveryReason,
			RecoveryDigest: recoveryDigest,
			ParentID:       parentID,
			Turns:          turns,
			Preview:        preview,
			SchemaVersion:  schemaVersion,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
			return out[i].Path < out[j].Path
		}
		return out[i].LastActivityAt.After(out[j].LastActivityAt)
	})
	return out, nil
}

// ListSessions returns every non-empty *.jsonl session under dir,
// most-recently-active first, each with a preview line so the picker can show
// something the user recognises. A missing directory is not an error — it just
// means there's nothing to resume yet.
func ListSessions(dir string) ([]SessionInfo, error) {
	ordered, err := ListSessionOrder(dir)
	if err != nil {
		return nil, err
	}
	var out []SessionInfo
	for _, session := range ordered {
		preview, turns := session.Preview, session.Turns
		if session.SchemaVersion < BranchMetaCountsVersion {
			// The sidecar's counts weren't recorded from content (a legacy session
			// from before they were persisted). Decode the .jsonl once, then backfill
			// + stamp the sidecar so every later listing is O(1) — and so a genuinely
			// empty session is recorded once instead of being re-decoded forever.
			preview, turns = previewSession(session.Path)
			// Best-effort: a failure here just means we decode again next time.
			_ = UpdateSessionMeta(session.Path, "", preview, turns, false)
		}
		if turns == 0 {
			// Never had user interaction — an empty conversation that should not
			// appear in the history panel or the resume picker.
			continue
		}
		out = append(out, SessionInfo{
			Path:           session.Path,
			CreatedAt:      session.CreatedAt,
			LastActivityAt: session.LastActivityAt,
			ModTime:        session.ModTime,
			Preview:        preview,
			Turns:          turns,
			Scope:          session.Scope,
			WorkspaceRoot:  session.WorkspaceRoot,
			TopicID:        session.TopicID,
			TopicTitle:     session.TopicTitle,
			CustomTitle:    session.CustomTitle,
			Recovered:      session.Recovered,
			RecoveryReason: session.RecoveryReason,
			RecoveryDigest: session.RecoveryDigest,
			ParentID:       session.ParentID,
		})
	}
	return out, nil
}

// SessionPreview returns the same preview and user-turn count used by
// ListSessions for one session file.
func SessionPreview(path string) (string, int) {
	return previewSession(path)
}

// SessionPreviewFromMessages computes the same preview line and user-turn count
// as previewSession, but from an in-memory message slice. Session.Save writes
// exactly these messages to the .jsonl, so this is byte-for-byte equivalent to
// decoding the file — letting the autosave path persist the counts into the
// sidecar without a disk read.
func SessionPreviewFromMessages(msgs []provider.Message) (string, int) {
	first := ""
	turns := 0
	for _, m := range msgs {
		if m.Role == provider.RoleUser {
			turns++
			if first == "" {
				first = truncatePreview(UserPreviewText(m.Content))
			}
		}
	}
	return first, turns
}

// previewSession returns the first user message (truncated) and the number of
// user-role messages so the picker can show "5 turns · 'help me debug the…'".
// Errors are swallowed — a malformed file just shows up with an empty preview.
func previewSession(path string) (string, int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	first := ""
	turns := 0
	for {
		var m provider.Message
		if err := dec.Decode(&m); err != nil {
			break // EOF or a malformed tail — return the preview gathered so far
		}
		if m.Role == provider.RoleUser {
			turns++
			if first == "" {
				first = truncatePreview(UserPreviewText(m.Content))
			}
		}
	}
	return first, turns
}

// truncatePreview clamps a preview line to 80 runes with an ellipsis, matching
// what the pickers render.
func truncatePreview(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[:77]) + "…"
	}
	return s
}

// ContinueSessionPath returns where a conversation carried into a rebuilt
// controller (model switch, config change) should keep auto-saving: its existing
// file when it has one, so the continued session stays a single file instead of
// the old one being orphaned as an identical duplicate (#2807). A session with no
// file yet gets a fresh path; "" when persistence is disabled.
func ContinueSessionPath(prevPath, dir, model string) string {
	if prevPath != "" {
		return prevPath
	}
	if dir == "" {
		return ""
	}
	return NewSessionPath(dir, model)
}

// NewSessionPath returns the path to use for a fresh session, namespaced by
// the model so the filename hints at what the conversation was with. dir is
// typically config.SessionDir().
func NewSessionPath(dir, model string) string {
	safe := strings.NewReplacer("/", "-", "\\", "-").Replace(model)
	if safe == "" {
		safe = "session"
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", time.Now().UTC().Format("20060102-150405.000000000"), safe))
}
