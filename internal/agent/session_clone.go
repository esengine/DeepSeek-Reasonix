package agent

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"reasonix/internal/store"
)

// CloneSessionToPath clones the authoritative transcript of the session at
// srcPath into a brand-new session file at dstPath.
//
// The .jsonl checkpoint is only a compatibility anchor: once the adjacent
// event log exists, it is the authoritative transcript and may hold turns the
// checkpoint has not caught up to. The clone therefore acquires the source
// save-path mutex AND the cross-process session file lock (the same order a
// save uses), replays the event log, reserves the destination files with
// O_EXCL, saves the complete session, and creates fresh branch metadata. Any
// failure removes every partial destination sidecar so a stale or truncated
// copy can never be adopted.
type SessionClone struct {
	Path string

	mu         sync.Mutex
	lease      *SessionLease
	artifacts  []sessionCloneArtifactState
	ownedPaths []string
	finalized  bool
}

var ErrSessionCloneChanged = errors.New("session clone changed after creation")

type sessionCloneArtifactState struct {
	path        string
	digest      [sha256.Size]byte
	mode        os.FileMode
	modTimeNano int64
}

// Commit transfers the destination lease to the caller and revokes this
// handle's cleanup authority. The caller must adopt or release the lease.
func (c *SessionClone) Commit() *SessionLease {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finalized {
		return nil
	}
	c.finalized = true
	lease := c.lease
	c.lease = nil
	c.artifacts = nil
	c.ownedPaths = nil
	return lease
}

// Discard removes the clone only while this handle still owns the destination
// lease and every artifact matches the state captured after Save. A writer
// that changed, replaced, or removed any artifact revokes cleanup authority;
// fail closed and leave the entire copy intact instead of letting an old
// generation delete a newer owner's session.
func (c *SessionClone) Discard() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.finalized {
		c.mu.Unlock()
		return nil
	}
	c.finalized = true
	lease := c.lease
	c.lease = nil
	path := c.Path
	artifacts := append([]sessionCloneArtifactState(nil), c.artifacts...)
	c.artifacts = nil
	c.ownedPaths = nil
	c.mu.Unlock()
	if lease != nil {
		defer lease.Release()
	}

	unlockSave := lockSessionSavePath(path)
	defer unlockSave()
	unlockFile, err := lockSessionFile(path)
	if err != nil {
		return fmt.Errorf("%w: destination is being written", ErrSessionCloneChanged)
	}
	defer unlockFile()
	for _, expected := range artifacts {
		current, err := captureSessionCloneArtifactState(expected.path)
		if err != nil || current != expected {
			return ErrSessionCloneChanged
		}
	}
	for _, artifact := range artifacts {
		if err := os.Remove(artifact.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("discard session clone: %w", err)
		}
	}
	return nil
}

func CloneSessionToPath(srcPath, dstPath string) (*SessionClone, error) {
	if srcPath == "" || dstPath == "" {
		return nil, fmt.Errorf("clone session: source and destination paths are required")
	}
	if canonicalSessionSavePath(srcPath) == canonicalSessionSavePath(dstPath) {
		return nil, fmt.Errorf("clone session: destination must differ from the source")
	}
	// 1. Load the authoritative transcript under the source save-path mutex
	// AND the cross-process file lock — another Reasonix process may be
	// saving the same session right now, and its newest event-log append must
	// not be missed. The lock order matches session.save.
	unlock := lockSessionSavePath(srcPath)
	if cloneLockWaitHook != nil {
		// Signal before blocking on the cross-process lock: tests use this as
		// a deterministic barrier to know the clone is about to wait.
		cloneLockWaitHook()
	}
	unlockFile, err := lockSessionFile(srcPath)
	if err != nil {
		unlock()
		return nil, fmt.Errorf("clone session: lock source file: %w", err)
	}
	session, loadErr := loadSessionUnlocked(srcPath)
	sourceMeta, sourceMetaOK, metaErr := LoadBranchMeta(srcPath)
	unlockFile()
	unlock()
	if loadErr != nil {
		return nil, fmt.Errorf("clone session: load source: %w", loadErr)
	}
	if metaErr != nil {
		return nil, fmt.Errorf("clone session: load source metadata: %w", metaErr)
	}
	// 2. Hold the destination runtime lease through the caller's commit/discard
	// decision. O_EXCL proves initial file creation, while the lease prevents a
	// second normal runtime from adopting the unpublished copy in that window.
	lease, err := TryAcquireSessionLease(dstPath)
	if err != nil {
		return nil, fmt.Errorf("clone session: lease destination: %w", err)
	}
	clone := &SessionClone{Path: dstPath, lease: lease}

	// 3. Reserve every destination path (create-only) before Save can replace
	// any of them. Reserving only the checkpoint/log is insufficient because
	// Save atomically replaces the derived event index and branch metadata.
	// A pre-existing sidecar must make the entire clone fail closed.
	// The event log is reserved empty: force saves are checkpoint-only, so the
	// clone needs the native log anchor in place for its own transcript to
	// evolve authoritatively.
	//
	// Only files THIS transaction actually created are ever removed: a
	// pre-existing sidecar (an authoritative event log whose checkpoint never
	// landed, or user metadata) is refused with its bytes untouched.
	reserve := func(path string) error {
		if err := reserveSessionClonePath(path); err != nil {
			return err
		}
		clone.ownedPaths = append(clone.ownedPaths, path)
		return nil
	}
	for _, path := range []string{
		dstPath,
		store.SessionEventLog(dstPath),
		store.SessionEventIndex(dstPath),
	} {
		if err := reserve(path); err != nil {
			clone.discardPartial()
			return nil, err
		}
	}
	// Session.Save reads the branch-meta CAS ledger before recording a content
	// revision, so the create-only metadata reservation must already contain a
	// valid fresh record rather than an empty placeholder.
	if err := reserveSessionCloneMeta(dstPath, sourceMeta, sourceMetaOK); err != nil {
		clone.discardPartial()
		return nil, err
	}
	clone.ownedPaths = append(clone.ownedPaths, store.SessionMeta(dstPath))
	// 4. Save the complete session; the empty reserved checkpoint receives the
	// full transcript.
	if err := session.Save(dstPath); err != nil {
		clone.discardPartial()
		return nil, fmt.Errorf("clone session: save destination: %w", err)
	}
	states, err := captureSessionCloneArtifactStates(clone.ownedPaths)
	if err != nil {
		clone.discardPartial()
		return nil, fmt.Errorf("clone session: capture destination state: %w", err)
	}
	clone.artifacts = states
	return clone, nil
}

func (c *SessionClone) discardPartial() {
	if c == nil {
		return
	}
	removeSessionCloneFiles(c.ownedPaths...)
	c.ownedPaths = nil
	if c.lease != nil {
		c.lease.Release()
		c.lease = nil
	}
	c.finalized = true
}

func captureSessionCloneArtifactStates(paths []string) ([]sessionCloneArtifactState, error) {
	states := make([]sessionCloneArtifactState, 0, len(paths))
	for _, path := range paths {
		state, err := captureSessionCloneArtifactState(path)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

func captureSessionCloneArtifactState(path string) (sessionCloneArtifactState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return sessionCloneArtifactState{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return sessionCloneArtifactState{}, err
	}
	return sessionCloneArtifactState{
		path:        path,
		digest:      sha256.Sum256(b),
		mode:        info.Mode(),
		modTimeNano: info.ModTime().UnixNano(),
	}, nil
}

func removeSessionCloneFiles(paths ...string) {
	for _, path := range paths {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}

func reserveSessionCloneMeta(sessionPath string, source BranchMeta, sourceOK bool) error {
	path := store.SessionMeta(sessionPath)
	when := time.Now().UTC()
	meta := BranchMeta{
		ID:        BranchID(sessionPath),
		CreatedAt: when,
		UpdatedAt: when,
	}
	if sourceOK {
		// Keep the desktop binding and user-selected runtime profile so opening a
		// copy stays in the same workspace/topic. Lineage, recovery, in-flight,
		// and persistence-ledger fields intentionally start fresh: the copy is an
		// independent session whose first Save owns its own revision history.
		meta.Name = source.Name
		meta.Scope = source.Scope
		meta.WorkspaceRoot = source.WorkspaceRoot
		meta.TopicID = source.TopicID
		meta.TopicTitle = source.TopicTitle
		meta.CustomTitle = source.CustomTitle
		meta.Model = source.Model
		meta.TokenMode = source.TokenMode
		meta.Mode = source.Mode
		meta.ToolApprovalMode = source.ToolApprovalMode
		meta.Goal = source.Goal
		meta.SchemaVersion = source.SchemaVersion
		meta.Turns = source.Turns
		meta.Preview = source.Preview
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("clone session: encode branch metadata: %w", err)
	}
	b = append(b, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("clone session: reserve destination: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("clone session: write branch metadata reservation: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("clone session: close branch metadata reservation: %w", err)
	}
	return nil
}

// reserveSessionClonePath creates dstPath with O_EXCL semantics so the clone
// never overwrites an existing session file.
func reserveSessionClonePath(dstPath string) error {
	f, err := os.OpenFile(dstPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("clone session: reserve destination: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(dstPath)
		return fmt.Errorf("clone session: close reserved destination: %w", err)
	}
	return nil
}

// cloneLockWaitHook, when set, is invoked immediately before the clone waits
// for the source's cross-process file lock. Tests use it as a deterministic
// barrier, removing timing-based false positives.
var cloneLockWaitHook func()
