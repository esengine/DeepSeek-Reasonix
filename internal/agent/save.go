package agent

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
	"reasonix/internal/store"
)

const (
	cleanupPendingExt             = ".cleanup-pending.json"
	maxRecoveryParentStemBytes    = 80
	sessionLockSidecarSuffix      = ".jsonl.lock"
	sessionLeaseLockSidecarSuffix = ".jsonl.lease.lock"
	sessionLeaseInfoSidecarSuffix = ".jsonl.lease.json"
	guardianSidecarSuffix         = ".guardian.jsonl"
	// nameMaxBytes is the single-component filename limit shared by the
	// filesystems Reasonix targets (APFS, ext4, NTFS all cap at 255).
	nameMaxBytes = 255
	// maxSessionBasenameBytes bounds transcript basenames that reconciliation
	// leaves in place. Sidecars append up to ~16 bytes to the transcript name
	// or its stem (".lease.lock", ".cleanup-pending.json", ".guardian.jsonl"),
	// so 224 keeps every sidecar comfortably under nameMaxBytes with headroom
	// for future suffixes. Names past this bound come from the pre-bounded
	// recovery cascade and get renamed by reconcileOverlongSessionFilenames.
	maxSessionBasenameBytes = 224
)

var (
	sessionSaveLocks            sync.Map
	ErrSessionSnapshotConflict  = errors.New("session snapshot conflicts with newer transcript")
	ErrSessionRecoveryNotNeeded = errors.New("session recovery not needed")
	// ErrSessionRecoveryDepthExceeded refuses a recovery fork whose parent is
	// already SessionRecoveryMaxDepth recovery forks deep. A chain that deep
	// means saves keep conflicting on branches this runtime itself created;
	// forking further multiplies session files without converging (#5993).
	ErrSessionRecoveryDepthExceeded = errors.New("session recovery chain depth exceeded")
	errSessionFileLockHeld          = errors.New("session file lock held")
	sessionWriterID                 = newSessionWriterID()
)

// SessionRecoveryMaxDepth bounds nested recovery forks: a normal session may
// fork a recovery branch (depth 1), which may itself fork twice more under
// genuine repeated incidents; past that the caller should stop forking and
// write onto the branch it already owns.
const SessionRecoveryMaxDepth = 3

type sessionPersistState struct {
	path     string
	digest   [sha256.Size]byte
	version  uint64
	revision int64
	// revisionKnown marks revision as a real ledger value. It is false when
	// the baseline was established while the meta sidecar was unreadable
	// (torn or corrupt): the session must still open, but revision 0 must not
	// pose as a baseline or every honest on-disk revision would read as a
	// stale-runtime conflict. CAS checks fall back to digest+version until a
	// successful save re-learns the revision.
	revisionKnown bool
	ok            bool
}

type sessionSaveMode int

const (
	sessionSaveForce sessionSaveMode = iota
	sessionSaveSnapshot
	sessionSaveRewrite
)

type snapshotWriteDecision struct {
	revision   int64
	upToDate   bool
	appendFrom int
	appendOnly bool
	// repairLog is set when the on-disk event log was damaged (torn tail with
	// a lost suffix, or nothing decodable): the safe write shape is a full
	// rewrite that also compacts the log back to a healthy single event.
	repairLog bool
	// ledgerStale is set when the on-disk transcript already matches the
	// snapshot but the meta ledger still describes older content — the
	// aftermath of a save whose bytes landed and whose revision record then
	// failed. The up-to-date path must heal the ledger instead of skipping it.
	ledgerStale bool
}

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

// Save persists the session using an append-only event log beside path. The
// .jsonl file remains as a compatibility checkpoint and discovery anchor; the
// event log is the authoritative transcript once present.
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
	// Capture the snapshot only while holding the save locks. Concurrent
	// in-process savers (turn-end snapshot, periodic autosave, shutdown
	// snapshot) that captured before locking could land out of order: the
	// stalest capture written last would then read the newer transcript it
	// lost the race to as a bogus stale-prefix conflict.
	msgs, version, rewriteVersion := s.snapshotWithVersion()
	// Durable transcripts are always redacted, independent of the live
	// [secrets] redact_tool_output toggle. Digest consistency holds because
	// Redact is deterministic and idempotent (see secrets.Redact): a loaded
	// session's messages are already redacted, so re-redacting them here is a
	// byte-for-byte no-op and the snapshot keeps its prefix relationship to
	// the on-disk transcript — the same digest/prefix machinery that guards
	// against bogus stale-prefix conflicts (#6083) sees identical bytes. The
	// persisted baseline (markPersisted) is likewise recorded over the
	// redacted form, matching what LoadSession will digest back.
	msgs = secrets.RedactMessages(msgs)
	digest, contentBytes, err := digestAndSizeSessionMessages(msgs)
	if err != nil {
		return err
	}
	probe, err := probeSessionEventLog(path)
	if err != nil {
		return err
	}
	if probe.futureSchema {
		return fmt.Errorf("session event log for %s uses schema %d; this build supports up to %d", path, probe.schemaVersion, sessionEventSchemaVersion)
	}
	if probe.native && probe.size > 0 {
		// Drop any torn tail a crashed or disk-full append left behind before
		// it can be buried under new records where replay would stop forever.
		if err := repairSessionEventLogTail(path); err != nil {
			return fmt.Errorf("repair session event log: %w", err)
		}
	}
	repairLog := false
	if mode != sessionSaveForce {
		decision, err := s.checkSnapshotWrite(path, msgs, digest, version, mode == sessionSaveRewrite)
		if err != nil {
			return err
		}
		if decision.upToDate {
			// Disk already holds exactly this transcript. Rewriting it would only
			// bump the revision, invalidating the persistence baseline of every
			// other runtime resumed on this file and turning their next
			// legitimate save into a stale-runtime conflict. Skip the write and
			// adopt the current on-disk revision as this session's baseline.
			if decision.ledgerStale {
				// ...unless the ledger never learned about this transcript: a
				// prior save landed its bytes and then failed to record the
				// revision. Same-content retries are exactly the "later save"
				// that failure deferred to, and skipping here would strand the
				// ledger on the old digest forever. Record now, reproducing
				// the state the interrupted save would have left.
				revision, err := recordSessionContentRevision(path, digest, decision.revision)
				if err != nil {
					return err
				}
				if probe.native {
					if err := writeSessionEventIndex(path, msgs, digest, revision); err != nil {
						// See the append path below: index loss must not fail a
						// save whose transcript and revision already landed.
						slog.Warn("session: keeping save after event index write failure", "path", path, "err", err)
					}
				}
				s.markPersisted(path, digest, version, revision, rewriteVersion)
				return nil
			}
			s.markPersisted(path, digest, version, decision.revision, rewriteVersion)
			return nil
		}
		if decision.appendOnly && probe.native {
			logSize := sessionEventLogSize(path)
			switch {
			case logSize == 0:
				if err := appendSessionReplaceEvent(path, msgs, digest, decision.revision, "snapshot"); err != nil {
					return err
				}
			case sessionEventLogOversized(logSize, contentBytes):
				// Checkpoint: fold history into one replace event and refresh
				// the .jsonl anchor so direct readers and older binaries stay
				// bounded-stale instead of frozen at first save.
				if err := compactSessionEventLog(path, msgs, digest, decision.revision, "compact"); err != nil {
					return err
				}
				if err := writeSessionMessages(path, msgs); err != nil {
					return err
				}
			default:
				if err := appendSessionAppendEvent(path, decision.appendFrom, msgs[decision.appendFrom:], digest, decision.revision); err != nil {
					return err
				}
			}
			revision, err := recordSessionContentRevision(path, digest, decision.revision)
			if err != nil {
				return err
			}
			if err := writeSessionEventIndex(path, msgs, digest, revision); err != nil {
				// The event index is only a listing accelerator; the transcript
				// and its revision are already durable above. Failing the save
				// here would skip markPersisted and leave the in-memory baseline
				// behind the disk state it just wrote, misreading the next save
				// as a stale-runtime conflict.
				slog.Warn("session: keeping save after event index write failure", "path", path, "err", err)
			}
			s.markPersisted(path, digest, version, revision, rewriteVersion)
			return nil
		}
		baseRevision = decision.revision
		repairLog = decision.repairLog
	} else if revision, _, err := sessionContentRevision(path); err != nil {
		return err
	} else {
		baseRevision = revision
	}
	// Full-rewrite path: intentional history rewrites, damage repairs, and
	// force saves. The event log mutates first so a crash between the two
	// writes leaves the newer transcript authoritative; the anchor rewrite
	// keeps the compatibility .jsonl fresh for direct readers.
	reason := "save"
	switch mode {
	case sessionSaveSnapshot:
		reason = "snapshot"
	case sessionSaveRewrite:
		reason = "rewrite"
	}
	if repairLog {
		reason = "repair"
	}
	logSize := sessionEventLogSize(path)
	switch {
	case !probe.native:
		// A foreign file (legacy import leftover) squats the native log path.
		// Never write into or over it — the session stays checkpoint-only.
	case mode == sessionSaveForce:
		// Force saves are one-shot copies (subagents, guardian, migrations,
		// forks): they never bootstrap an event log, and fold an existing one
		// into a single replace event so the log cannot disagree with the
		// anchor.
		if logSize > 0 {
			if err := compactSessionEventLog(path, msgs, digest, baseRevision, reason); err != nil {
				return err
			}
		}
	case repairLog, sessionEventLogOversized(logSize, contentBytes):
		if err := compactSessionEventLog(path, msgs, digest, baseRevision, reason); err != nil {
			return err
		}
	default:
		if err := appendSessionReplaceEvent(path, msgs, digest, baseRevision, reason); err != nil {
			return err
		}
	}
	if err := writeSessionMessages(path, msgs); err != nil {
		return err
	}
	revision, err := recordSessionContentRevision(path, digest, baseRevision)
	if err != nil {
		return err
	}
	if probe.native {
		if err := writeSessionEventIndex(path, msgs, digest, revision); err != nil {
			// See the append path above: index loss must not fail a save whose
			// transcript and revision already landed.
			slog.Warn("session: keeping save after event index write failure", "path", path, "err", err)
		}
	}
	s.markPersisted(path, digest, version, revision, rewriteVersion)
	return nil
}

func writeSessionMessages(path string, msgs []provider.Message) error {
	// Write to a sibling tmp file then rename, so a crash mid-write can't
	// leave a partial JSONL that won't reload. The fsync guards the anchor
	// against power loss — it is the fallback when the event log is damaged.
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
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

// checkSnapshotWrite decides whether this session may write msgs over path, and
// whether the safe write shape is a no-op, append-only suffix, or full rewrite.
func (s *Session) checkSnapshotWrite(path string, next []provider.Message, nextDigest [sha256.Size]byte, nextVersion uint64, allowOwnedRewrite bool) (snapshotWriteDecision, error) {
	current, err := LoadSession(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshotWriteDecision{}, nil
		}
		return snapshotWriteDecision{}, err
	}
	currentRevision, currentLedgerDigest, err := sessionContentRevision(path)
	if err != nil {
		return snapshotWriteDecision{}, err
	}
	baseState := s.persistState(path)
	existing := current.Snapshot()
	existingDigest, err := digestSessionMessages(existing)
	if err != nil {
		return snapshotWriteDecision{}, err
	}
	// raw is the transcript as stored, before load-time normalization repaired
	// it; it equals existing when no repair ran. The prefix checks below must
	// be able to fall back to it: a mid-turn snapshot legitimately cuts an
	// assistant tool call from its still-running result, normalization then
	// fabricates a placeholder answer on load, and the live session's real
	// result collides with that placeholder — misreading a pure append as
	// divergence (and forking a bogus recovery branch).
	raw, rawDigest := existing, existingDigest
	rawDiffers := current.normalizedDirty && len(current.rawMessages) > 0
	if rawDiffers {
		raw = current.rawMessages
		if rawDigest, err = digestSessionMessages(raw); err != nil {
			return snapshotWriteDecision{}, err
		}
	}
	contentUnchanged := bytes.Equal(existingDigest[:], nextDigest[:])
	exactAppend := messagesHavePrefix(next, existing)
	appendShaped := contentUnchanged || exactAppend || messagesHavePrefixWithCompatibleSystem(next, existing)
	repairPending := current.normalizedDirty
	if !appendShaped && rawDiffers {
		rawUnchanged := bytes.Equal(rawDigest[:], nextDigest[:])
		rawAppend := messagesHavePrefix(next, raw)
		if rawUnchanged || rawAppend || messagesHavePrefixWithCompatibleSystem(next, raw) {
			existing = raw
			contentUnchanged = rawUnchanged
			exactAppend = rawAppend
			appendShaped = true
			// The snapshot supersedes the repaired view — appending it lands
			// the real tool results where the placeholders were fabricated —
			// so no load-time repair is left to force a rewrite.
			repairPending = false
		}
	}
	if appendShaped {
		// An unknown-revision baseline (meta sidecar unreadable at load) cannot
		// vouch for revision equality; the digest/prefix checks above already
		// vouch for the content, so only a known baseline arms the CAS check.
		// Under an append-shaped write (at most a compatible leading-system
		// swap) a stale revision is ledger drift — a reset sidecar, a
		// same-content heal, or another runtime recording messages this
		// snapshot already contains — unless the transcript was rewound.
		// Locating the persisted baseline among the snapshot's prefixes and
		// requiring the disk transcript to still reach it tells the two apart:
		// drift keeps the baseline reachable, while a rewind cut below it and
		// appending would resurrect the suffix another runtime removed.
		if baseState.ok && baseState.revisionKnown && currentRevision != baseState.revision && !contentUnchanged &&
			!appendCoversPersistedBaseline(next, existing, baseState.digest) {
			return snapshotWriteDecision{}, snapshotConflict(path, existing, next, baseState.revision, currentRevision)
		}
		// A normalized-dirty load means LoadSession repaired the history on the
		// way in: the digests match but the raw bytes on disk do not, so the
		// repair still needs a real write to persist. A damaged event log
		// likewise needs a real write (rewrite + compact) even when the
		// replayable prefix already matches this snapshot.
		decision := snapshotWriteDecision{
			revision:  currentRevision,
			upToDate:  contentUnchanged && !repairPending && !current.eventLogDamaged,
			repairLog: current.eventLogDamaged,
		}
		// A ledger digest that describes different content than the transcript
		// on disk is the aftermath of a save whose bytes landed and whose
		// revision record then failed (crash or fail-closed record between the
		// two writes). Only a non-empty mismatch counts: a missing sidecar or
		// a legacy one without a digest is a legitimate state, and stamping it
		// here would bump revisions other runtimes still hold as baselines.
		if decision.upToDate && currentLedgerDigest != "" && currentLedgerDigest != digestString(nextDigest) {
			decision.ledgerStale = true
		}
		// An append is only chain-safe when existing measures the transcript
		// the event log actually replays. Under a pending load-time repair the
		// normalized view differs from the raw log, so an append event indexed
		// against it breaks the replay chain and orphans the appended suffix;
		// fall through to the full rewrite, which also persists the repair.
		if exactAppend && !contentUnchanged && len(existing) < len(next) && !current.eventLogDamaged && !repairPending {
			decision.appendOnly = true
			decision.appendFrom = len(existing)
		}
		return decision, nil
	}
	if allowOwnedRewrite {
		owned := s.ownsPersistedState(path, existingDigest, currentRevision, currentLedgerDigest, nextVersion)
		if !owned && rawDiffers {
			// The persisted baseline describes the bytes this session wrote, so
			// a repaired view can never match it; ownership is judged against
			// the raw transcript.
			owned = s.ownsPersistedState(path, rawDigest, currentRevision, currentLedgerDigest, nextVersion)
		}
		if owned {
			return snapshotWriteDecision{revision: currentRevision, repairLog: current.eventLogDamaged}, nil
		}
	}
	if messagesHavePrefix(existing, next) || messagesHavePrefixWithCompatibleSystem(existing, next) ||
		(rawDiffers && (messagesHavePrefix(raw, next) || messagesHavePrefixWithCompatibleSystem(raw, next))) {
		return snapshotWriteDecision{}, &SessionSnapshotConflictError{
			Path:             path,
			Kind:             SessionSnapshotConflictStalePrefix,
			ExistingMessages: len(existing),
			SnapshotMessages: len(next),
			BaseRevision:     baseState.revision,
			DiskRevision:     currentRevision,
		}
	}
	return snapshotWriteDecision{}, &SessionSnapshotConflictError{
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
	msgs, version, rewriteVersion := s.snapshotWithVersion()
	// Same redaction contract as save(): recovery branches are durable
	// transcripts too, and the coverage checks below compare against on-disk
	// content that save() already redacted — comparing a raw snapshot against
	// it would misread pure coverage as divergence and fork a bogus branch.
	msgs = secrets.RedactMessages(msgs)
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
		covered := bytes.Equal(existingDigest[:], digest[:]) ||
			messagesHavePrefix(existing, msgs) ||
			messagesHavePrefixWithCompatibleSystem(existing, msgs)
		if !covered && current.normalizedDirty && len(current.rawMessages) > 0 {
			// Judge coverage against the pre-repair transcript too, for the
			// same reason as checkSnapshotWrite: load-time normalization can
			// reshape what is actually stored, and a recovery fork is only
			// warranted when the stored bytes themselves fail to cover this
			// snapshot.
			raw := current.rawMessages
			rawDigest, rawErr := digestSessionMessages(raw)
			if rawErr != nil {
				return RecoveryBranchInfo{}, rawErr
			}
			covered = bytes.Equal(rawDigest[:], digest[:]) ||
				messagesHavePrefix(raw, msgs) ||
				messagesHavePrefixWithCompatibleSystem(raw, msgs)
		}
		if covered {
			return RecoveryBranchInfo{}, ErrSessionRecoveryNotNeeded
		}
	}

	// Refuse to deepen a runaway chain: forking FROM a branch that is already
	// at the depth cap only multiplies recovery files (#5993 reached 8 nested
	// levels). The caller falls back to force-writing the branch it owns.
	parentDepth := 0
	if parentMeta, ok, metaErr := LoadBranchMeta(originalPath); metaErr == nil && ok && parentMeta.Recovered {
		parentDepth = parentMeta.RecoveryDepth
		if parentDepth <= 0 {
			// Legacy recovery meta predating RecoveryDepth.
			parentDepth = 1
		}
	}
	if parentDepth >= SessionRecoveryMaxDepth {
		return RecoveryBranchInfo{}, fmt.Errorf("%w: %s is already %d recovery forks deep",
			ErrSessionRecoveryDepthExceeded, originalPath, parentDepth)
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
			meta, err := s.saveRecoveryBranchMeta(recoveryPath, opts, preview, turns, digestText, parentDepth+1)
			if err != nil {
				return RecoveryBranchInfo{}, err
			}
			s.markPersisted(recoveryPath, digest, version, meta.Revision, rewriteVersion)
			return RecoveryBranchInfo{Path: recoveryPath, Digest: digestText, Existing: true, Meta: meta, Preview: preview, Turns: turns}, nil
		}
	} else if loadErr != nil && !os.IsNotExist(loadErr) {
		return RecoveryBranchInfo{}, loadErr
	}

	if err := os.MkdirAll(filepath.Dir(recoveryPath), 0o755); err != nil {
		return RecoveryBranchInfo{}, fmt.Errorf("create recovery session dir: %w", err)
	}
	// Log first, anchor second: a crash in between leaves the (authoritative)
	// log holding the recovered transcript. A foreign file at the log path is
	// left alone; the recovery stays checkpoint-only then.
	recoveryProbe, err := probeSessionEventLog(recoveryPath)
	if err != nil {
		return RecoveryBranchInfo{}, err
	}
	if recoveryProbe.native {
		if err := appendSessionReplaceEvent(recoveryPath, msgs, digest, 0, "recovery"); err != nil {
			return RecoveryBranchInfo{}, err
		}
	}
	if err := writeSessionMessages(recoveryPath, msgs); err != nil {
		return RecoveryBranchInfo{}, err
	}
	meta, err := s.saveRecoveryBranchMeta(recoveryPath, opts, preview, turns, digestText, parentDepth+1)
	if err != nil {
		return RecoveryBranchInfo{}, err
	}
	if err := writeSessionEventIndex(recoveryPath, msgs, digest, meta.Revision); err != nil {
		// The recovery transcript (log + checkpoint) and its meta are already
		// durable; the index is only a listing accelerator. Failing here would
		// discard a recovery that in fact succeeded and re-run the whole
		// conflict path on the next save.
		slog.Warn("session: keeping recovery branch after event index write failure",
			"path", recoveryPath, "err", err)
	}
	s.markPersisted(recoveryPath, digest, version, meta.Revision, rewriteVersion)
	return RecoveryBranchInfo{Path: recoveryPath, Digest: digestText, Meta: meta, Preview: preview, Turns: turns}, nil
}

func (s *Session) saveRecoveryBranchMeta(path string, opts RecoveryBranchOptions, preview string, turns int, digest string, depth int) (BranchMeta, error) {
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
	// Always stamped from the parent chain, never trusted from opts: callers
	// copy tab/session meta wholesale and would carry a stale depth.
	meta.RecoveryDepth = depth
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
	parent := recoveryParentStem(BranchID(originalPath))
	return filepath.Join(filepath.Dir(originalPath), fmt.Sprintf("%s-recovery-%x.jsonl", parent, digest[:8]))
}

func recoveryParentStem(parent string) string {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return "session"
	}
	sum := sha256.Sum256([]byte(parent))
	if idx := strings.Index(parent, "-recovery-"); idx >= 0 {
		base := strings.Trim(parent[:idx], "-_. ")
		if base == "" {
			base = "session"
		}
		base = strings.Trim(truncateUTF8Bytes(base, maxRecoveryParentStemBytes), "-_. ")
		if base == "" {
			base = "session"
		}
		return fmt.Sprintf("%s-%x", base, sum[:6])
	}
	if len(parent) <= maxRecoveryParentStemBytes {
		return parent
	}
	prefix := strings.Trim(truncateUTF8Bytes(parent, maxRecoveryParentStemBytes), "-_. ")
	if prefix == "" {
		prefix = "session"
	}
	return fmt.Sprintf("%s-%x", prefix, sum[:6])
}

func truncateUTF8Bytes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	used := 0
	for i, r := range s {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = 1
		}
		if used+size > max {
			return s[:i]
		}
		used += size
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Session) ownsPersistedState(path string, existingDigest [sha256.Size]byte, existingRevision int64, existingLedgerDigest string, nextVersion uint64) bool {
	state := s.persistState(path)
	if !state.ok || state.version > nextVersion || !bytes.Equal(existingDigest[:], state.digest[:]) {
		return false
	}
	// An unknown-revision baseline still owns the transcript it loaded — the
	// digest+version match proves it. Requiring revision equality here would
	// make every rewrite from such a baseline a permanent conflict, because
	// the revision can only be re-learned by a successful save.
	// A disk ledger with no recorded revision is the mirror case: recorded
	// revisions start at 1, so revision 0 means the sidecar was deleted or
	// rebuilt by a listing-only writer after this session's save. An absent
	// claim cannot revoke the ownership the digest+version match proves.
	if !state.revisionKnown || existingRevision == 0 || state.revision == existingRevision {
		return true
	}
	// A foreign revision stamp whose recorded digest still describes these
	// exact bytes (a same-content heal or no-op record by another runtime)
	// vouches for no content of its own: the transcript is byte-for-byte what
	// this session last persisted, so rewriting it destroys nothing of
	// theirs — at worst the conflict moves to the stamper's next divergent
	// save, where its in-memory history forks a recovery branch as usual.
	// A stamp that disagrees with the on-disk transcript (or a legacy stamp
	// with no digest) keeps revoking ownership: that is the aftermath of a
	// save whose bytes and record split, the bytes cannot be attributed, and
	// only the conservative conflict path preserves both sides.
	return existingLedgerDigest == digestString(existingDigest)
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

func (s *Session) markPersisted(path string, digest [sha256.Size]byte, version uint64, revision int64, rewriteVersion int) {
	s.setPersistedBaseline(path, digest, version, revision, true, rewriteVersion)
}

// markPersistedRevisionUnknown records a baseline whose ledger revision could
// not be learned because the meta sidecar was unreadable. The digest and
// version still anchor ownership checks; revision-based CAS stays disarmed
// until a successful save records the real revision via markPersisted.
func (s *Session) markPersistedRevisionUnknown(path string, digest [sha256.Size]byte, version uint64, rewriteVersion int) {
	s.setPersistedBaseline(path, digest, version, 0, false, rewriteVersion)
}

func (s *Session) setPersistedBaseline(path string, digest [sha256.Size]byte, version uint64, revision int64, revisionKnown bool, rewriteVersion int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persisted = sessionPersistState{
		path:          canonicalSessionSavePath(path),
		digest:        digest,
		version:       version,
		revision:      revision,
		revisionKnown: revisionKnown,
		ok:            true,
	}
	// rewriteVersion was captured together with the persisted snapshot; only
	// move forward so a slower save that captured earlier cannot roll the
	// baseline back below a rewrite a faster save already persisted.
	if rewriteVersion > s.persistedRewriteVersion {
		s.persistedRewriteVersion = rewriteVersion
	}
}

// sessionContentRevision reads the CAS ledger (revision + content digest) from
// the branch-meta sidecar. A missing sidecar is revision 0 — a session that
// has never recorded one. An unreadable sidecar is an error: reporting it as
// revision 0 would desync every runtime baseline from the ledger and turn the
// next honest save into a bogus conflict (and a recovery branch).
func sessionContentRevision(path string) (int64, string, error) {
	meta, ok, err := loadBranchMetaRetry(path)
	if err != nil {
		return 0, "", err
	}
	if !ok {
		return 0, "", nil
	}
	return meta.Revision, strings.TrimSpace(meta.ContentDigest), nil
}

func recordSessionContentRevision(path string, digest [sha256.Size]byte, baseRevision int64) (int64, error) {
	meta, ok, err := loadBranchMetaRetry(path)
	if err != nil {
		// Fail the save instead of rebuilding the ledger from a bad read: the
		// transcript bytes already landed, so a later save can record the
		// revision once the sidecar reads cleanly again — a content-bearing
		// save lands here again, and a same-content retry heals through the
		// up-to-date ledgerStale path in save().
		return 0, err
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
	stored, ok, err := loadBranchMetaRetry(path)
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
	digest, _, err := digestAndSizeSessionMessages(msgs)
	return digest, err
}

// digestAndSizeSessionMessages also reports the encoded transcript size, which
// the save path uses to bound the event log relative to the live content.
func digestAndSizeSessionMessages(msgs []provider.Message) ([sha256.Size]byte, int64, error) {
	h := sha256.New()
	size := int64(0)
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			return [sha256.Size]byte{}, 0, err
		}
		if _, err := h.Write(b); err != nil {
			return [sha256.Size]byte{}, 0, err
		}
		if _, err := h.Write([]byte{'\n'}); err != nil {
			return [sha256.Size]byte{}, 0, err
		}
		size += int64(len(b)) + 1
	}
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out, size, nil
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

// messagesPrefixDigestDepth returns the number of leading messages of msgs
// whose storage digest equals target, or -1 when no prefix matches. The
// digest accumulates exactly like digestAndSizeSessionMessages, so a match at
// depth k means msgs[:k] is byte-for-byte the transcript that produced target.
func messagesPrefixDigestDepth(msgs []provider.Message, target [sha256.Size]byte) int {
	h := sha256.New()
	sum := make([]byte, 0, sha256.Size)
	for i, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			return -1
		}
		h.Write(b)
		h.Write([]byte{'\n'})
		sum = h.Sum(sum[:0])
		if bytes.Equal(sum, target[:]) {
			return i + 1
		}
	}
	return -1
}

// appendCoversPersistedBaseline reports whether an append-shaped write (disk
// transcript a prefix of next, modulo a compatible leading-system swap) still
// covers everything this session ever persisted: the baseline digest must be
// reachable as a prefix of the pending snapshot, and the disk transcript must
// still extend at least to that depth. A shorter disk transcript means some
// other runtime deliberately rewound below the baseline — appending over it
// would resurrect the removed suffix, so the caller must conflict instead.
func appendCoversPersistedBaseline(next, existing []provider.Message, baseDigest [sha256.Size]byte) bool {
	depth := messagesPrefixDigestDepth(next, baseDigest)
	if depth < 0 && len(next) > 0 && len(existing) > 0 &&
		next[0].Role == provider.RoleSystem && existing[0].Role == provider.RoleSystem &&
		!messagesEqualForStorage(next[0], existing[0]) {
		// A resume that swapped the system prompt persisted its baseline with
		// the previous system message — the one still on disk. Re-anchor the
		// search on that message so the swap alone doesn't hide the baseline.
		variant := append([]provider.Message{existing[0]}, next[1:]...)
		depth = messagesPrefixDigestDepth(variant, baseDigest)
	}
	return depth >= 0 && len(existing) >= depth
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

// LockSessionMetaPath serializes a read-modify-write cycle on a session's
// sidecar metadata with every other writer in this process (Save, the
// UpdateSessionMeta family). Callers outside this package that load, mutate,
// and re-save branch meta must hold it for the whole cycle.
func LockSessionMetaPath(path string) func() {
	return lockSessionSavePath(path)
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

// CanonicalSessionPath is the identity key of a session path: cleaned,
// absolute, and case-folded on Windows, matching the key form used by the
// lease registry and the save-path locks. Any runtime bookkeeping that
// compares or maps session paths (desktop tabs, detached runtimes) must use
// this exact form, or the same file splits into distinct keys — e.g.
// `C:\Users\...` vs the lease's lowercased `c:\users\...`. Empty input stays
// empty instead of resolving to the working directory.
func CanonicalSessionPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return canonicalSessionSavePath(path)
}

// LoadSession reads a saved session into a fresh Session value. New sessions
// replay the append-only event log; legacy sessions without an event log fall
// back to the compatibility .jsonl checkpoint. A damaged log is replayed to its
// last clean record (or the checkpoint when nothing decodes) and flagged so the
// next save heals it with a rewrite-and-compact.
// Missing files surface as os.IsNotExist so callers can fall through to a
// new session.
func LoadSession(path string) (*Session, error) {
	msgs, _, damaged, err := loadSessionMessages(path)
	if err != nil {
		return nil, err
	}
	s := &Session{Messages: msgs, eventLogDamaged: damaged}
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
		// Keep the pre-repair transcript: checkSnapshotWrite must be able to
		// recognize a snapshot that extends the bytes actually on disk, which
		// the repaired view no longer represents (an interrupted tool turn
		// gets a placeholder result fabricated here that the live session
		// answered for real).
		s.rawMessages = msgs
	}
	s.Messages = normalized
	if digest, err := digestSessionMessages(s.Messages); err == nil {
		if meta, ok, metaErr := loadBranchMetaRetry(path); metaErr != nil {
			// The sidecar exists but is unreadable even after retries (torn or
			// corrupt). The session must still open, but revision 0 must not
			// pose as a real baseline: the next save would misread the honest
			// on-disk revision as another runtime's write and fork a recovery
			// branch. Anchor the baseline on digest+version only until a
			// successful save re-learns the revision.
			s.markPersistedRevisionUnknown(path, digest, s.version, s.rewriteVersion)
		} else {
			revision := int64(0)
			if ok {
				revision = meta.Revision
			}
			s.markPersisted(path, digest, s.version, revision, s.rewriteVersion)
		}
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
// markers and stale lock/lease sidecars. It keeps going after individual
// cleanup errors and returns them joined.
func ReconcileCleanupPending(dir string, cleanup func(CleanupPendingInfo) error) error {
	var errs []error
	if err := ReconcileSessionSidecars(dir); err != nil {
		errs = append(errs, err)
	}
	pending, err := ListCleanupPending(dir)
	if err != nil {
		errs = append(errs, err)
		return errors.Join(errs...)
	}
	if cleanup == nil {
		return errors.Join(errs...)
	}
	for _, item := range pending {
		if err := cleanup(item); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", item.SessionPath, err))
		}
	}
	return errors.Join(errs...)
}

// ReconcileSessionSidecars renames transcripts whose filenames outgrew their
// sidecars and removes stale lock and lease files left beside sessions by
// older runtimes. It never removes .jsonl transcripts; recovered conversations
// may contain useful user history even when their names are ugly.
func ReconcileSessionSidecars(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	var errs []error
	if err := reconcileOverlongSessionFilenames(dir); err != nil {
		errs = append(errs, err)
	}
	// Re-list after the rename pass: it retires old names and their sidecars.
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.Join(errs...)
		}
		errs = append(errs, err)
		return errors.Join(errs...)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		sidecarPath := filepath.Join(dir, name)
		switch {
		case strings.HasSuffix(name, sessionLeaseInfoSidecarSuffix):
			base := filepath.Join(dir, strings.TrimSuffix(name, ".lease.json"))
			if err := removeStaleSessionLeaseInfoSidecar(base, sidecarPath); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", sidecarPath, err))
			}
		case strings.HasSuffix(name, sessionLeaseLockSidecarSuffix):
			base := filepath.Join(dir, strings.TrimSuffix(name, ".lease.lock"))
			if err := removeStaleSessionLeaseLockSidecar(base, sidecarPath); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", sidecarPath, err))
			}
		case strings.HasSuffix(name, sessionLockSidecarSuffix):
			base := filepath.Join(dir, strings.TrimSuffix(name, ".lock"))
			if err := removeStaleSessionLockSidecar(base, sidecarPath); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", sidecarPath, err))
			}
		}
	}
	return errors.Join(errs...)
}

func removeStaleSessionLockSidecar(basePath, sidecarPath string) error {
	basePath = canonicalSessionSavePath(basePath)
	if sessionLeaseHeldLocally(basePath) || SessionLeaseHeldByOtherRuntime(basePath) {
		return nil
	}
	lock, err := tryTakeSessionLockFile(sidecarPath)
	if err != nil {
		if errors.Is(err, errSessionFileLockHeld) {
			return nil
		}
		return err
	}
	// The removal is atomic with the release (unlink-under-flock on Unix,
	// delete-disposition on the held handle on Windows), so a concurrent
	// saver can never acquire a lock file that is being deleted under it.
	return lock.RemoveAndUnlock()
}

// removeStaleSessionLeaseLockSidecar retires a leftover .lease.lock. The file
// is the lease lock itself, so taking it non-blocking proves no runtime holds
// the lease, and RemoveAndUnlock deletes it atomically with the release.
func removeStaleSessionLeaseLockSidecar(basePath, sidecarPath string) error {
	basePath = canonicalSessionSavePath(basePath)
	if sessionLeaseHeldLocally(basePath) {
		return nil
	}
	lock, err := tryTakeSessionLockFile(sidecarPath)
	if err != nil {
		if errors.Is(err, errSessionFileLockHeld) {
			return nil
		}
		return err
	}
	return lock.RemoveAndUnlock()
}

// removeStaleSessionLeaseInfoSidecar retires a leftover .lease.json while
// holding the lease lock, so no runtime can adopt the info file mid-removal.
// The info file itself is never held open by anyone, so a plain remove under
// the lock is safe on every platform.
func removeStaleSessionLeaseInfoSidecar(basePath, sidecarPath string) error {
	basePath = canonicalSessionSavePath(basePath)
	if sessionLeaseHeldLocally(basePath) {
		return nil
	}
	lockPath := basePath + ".lease.lock"
	if _, err := os.Stat(lockPath); err == nil {
		unlock, err := tryLockSessionLeaseFile(basePath)
		if err != nil {
			if errors.Is(err, ErrSessionLeaseHeld) {
				return nil
			}
			return err
		}
		removeErr := os.Remove(sidecarPath)
		if unlock != nil {
			unlock()
		}
		if removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	// No lease lock file: holders keep it present (and locked) for their whole
	// lifetime, so the leftover info sidecar has no owner to race with.
	if err := os.Remove(sidecarPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func sessionLeaseHeldLocally(path string) bool {
	_, ok := sessionLeaseOwners.Load(canonicalSessionSavePath(path))
	return ok
}

// sessionLockSidecarFits reports whether basePath's .lock sidecar name stays
// within the filesystem's per-component limit; past it, no process can hold
// (or ever have held) the file lock, because the lock file cannot be created.
func sessionLockSidecarFits(basePath string) bool {
	return len(filepath.Base(basePath))+len(".lock") <= nameMaxBytes
}

// sessionLeaseSidecarFits is the lease-file analogue of sessionLockSidecarFits.
func sessionLeaseSidecarFits(basePath string) bool {
	return len(filepath.Base(basePath))+len(".lease.lock") <= nameMaxBytes
}

// reconcileOverlongSessionFilenames renames transcripts whose basenames grew
// past maxSessionBasenameBytes — the leftover shape of the pre-bounded
// recovery cascade (#5923), where lock and lease sidecars could no longer be
// created and the session became unsaveable. The conversation bytes are kept
// verbatim under a bounded name derived the same way new recovery branches
// are named; branch meta moves along with its ID rewritten, and sessions
// pointing at the old ID are re-parented so lineage survives the rename.
func reconcileOverlongSessionFilenames(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var errs []error
	renamed := map[string]string{} // old branch ID -> new branch ID
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !store.IsSessionTranscriptName(name) {
			continue
		}
		if len(name) <= maxSessionBasenameBytes {
			continue
		}
		oldPath := filepath.Join(dir, name)
		if IsCleanupPending(oldPath) {
			// Being deleted; renaming would orphan the cleanup marker.
			continue
		}
		newID, err := renameOverlongSession(oldPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", oldPath, err))
		}
		// A non-empty newID means the transcript rename landed even if some
		// sidecar migration failed; record it so children still re-parent —
		// this run is the only one that knows the old-to-new mapping.
		if newID != "" {
			renamed[BranchID(oldPath)] = newID
		}
	}
	if len(renamed) > 0 {
		if err := reparentSessionBranches(dir, renamed); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// renameOverlongSession moves one overlong transcript to its bounded name and
// migrates the sidecars that carry user state. It returns the new branch ID,
// or "" when the session was skipped because a runtime may still own it.
func renameOverlongSession(oldPath string) (string, error) {
	oldID := BranchID(oldPath)
	newID := recoveryParentStem(oldID)
	if newID == oldID {
		return "", nil
	}
	newPath := filepath.Join(filepath.Dir(oldPath), newID+".jsonl")
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("rename target %s already exists", filepath.Base(newPath))
	} else if !os.IsNotExist(err) {
		return "", err
	}
	unlockOld := lockSessionSavePath(oldPath)
	defer unlockOld()
	unlockNew := lockSessionSavePath(newPath)
	defer unlockNew()
	if sessionLeaseHeldLocally(oldPath) {
		return "", nil
	}
	// Names past the sidecar limit cannot have lease or lock holders in any
	// process — the holder files themselves are uncreatable — so probing them
	// would only manufacture ENAMETOOLONG errors and wrongly skip the exact
	// sessions this pass exists to repair.
	if sessionLeaseSidecarFits(oldPath) && SessionLeaseHeldByOtherRuntime(oldPath) {
		return "", nil
	}
	var lockFile *sessionLockFile
	if sessionLockSidecarFits(oldPath) {
		lock, err := tryTakeSessionLockFile(oldPath + ".lock")
		if err != nil {
			if errors.Is(err, errSessionFileLockHeld) {
				return "", nil
			}
			return "", err
		}
		lockFile = lock
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		// Nothing moved: the old transcript is intact and the next
		// reconciliation can retry, so its lock file stays in place too.
		if lockFile != nil {
			lockFile.Unlock()
		}
		return "", err
	}
	// The transcript is committed under its new name from here on. Sidecar
	// migration and lock cleanup failures are reported, but the new ID is
	// still returned so the caller re-parents children: the old name is gone,
	// and a later run would have no way to reconstruct this mapping.
	var errs []error
	if err := migrateSessionSidecars(oldPath, newPath, newID); err != nil {
		errs = append(errs, err)
	}
	// Retire the old disposable lease sidecars: any holder was ruled out
	// above, and nothing keeps these files open, so a plain remove is safe.
	if sessionLeaseSidecarFits(oldPath) {
		for _, stale := range []string{oldPath + ".lease.lock", oldPath + ".lease.json"} {
			if err := os.Remove(stale); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	// The old .lock goes atomically with the release of the lock we hold on it.
	if lockFile != nil {
		if err := lockFile.RemoveAndUnlock(); err != nil {
			errs = append(errs, err)
		}
	}
	return newID, errors.Join(errs...)
}

// migrateSessionSidecars moves the user-state sidecars of a renamed session:
// branch meta (with its ID rewritten to match the new filename), goal state,
// and the checkpoint/job directories. Lock and lease files are disposable and
// are removed by the caller instead.
func migrateSessionSidecars(oldPath, newPath, newID string) error {
	var errs []error
	if len(filepath.Base(oldPath))+len(".meta") <= nameMaxBytes {
		if meta, ok, err := LoadBranchMeta(oldPath); err != nil {
			errs = append(errs, err)
		} else if ok {
			meta.ID = newID
			if err := SaveBranchMetaPreserveUpdated(newPath, meta); err != nil {
				errs = append(errs, err)
			} else if err := os.Remove(BranchMetaPath(oldPath)); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	for _, pair := range [][2]string{
		{store.SessionGoalState(oldPath), store.SessionGoalState(newPath)},
		{store.SessionEventLog(oldPath), store.SessionEventLog(newPath)},
		{store.SessionEventIndex(oldPath), store.SessionEventIndex(newPath)},
		{store.SessionConflictLog(oldPath), store.SessionConflictLog(newPath)},
		{store.SessionCheckpointDir(oldPath), store.SessionCheckpointDir(newPath)},
		{store.SessionJobsDir(oldPath), store.SessionJobsDir(newPath)},
	} {
		// A source name past the filesystem limit cannot exist; renaming it
		// would just manufacture ENAMETOOLONG instead of a clean not-exist.
		if len(filepath.Base(pair[0])) > nameMaxBytes {
			continue
		}
		if err := os.Rename(pair[0], pair[1]); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// reparentSessionBranches rewrites ParentID references from renamed branch IDs
// to their bounded replacements so the branch tree stays connected.
func reparentSessionBranches(dir string, renamed map[string]string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !store.IsSessionTranscriptName(name) {
			continue
		}
		if len(name)+len(".meta") > nameMaxBytes {
			continue
		}
		path := filepath.Join(dir, name)
		unlock := lockSessionSavePath(path)
		meta, ok, err := LoadBranchMeta(path)
		if err == nil && ok {
			if newParent, hit := renamed[meta.ParentID]; hit && newParent != meta.ParentID {
				meta.ParentID = newParent
				err = SaveBranchMetaPreserveUpdated(path, meta)
			}
		}
		unlock()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
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
		if e.IsDir() || !store.IsSessionTranscriptName(e.Name()) {
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
		contentMod := SessionContentModTime(full)
		if contentMod.IsZero() {
			contentMod = info.ModTime()
		}
		createdAt := info.ModTime()
		lastActivityAt := contentMod
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
	msgs, _, _, err := loadSessionMessages(path)
	if err != nil {
		return "", 0
	}
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
