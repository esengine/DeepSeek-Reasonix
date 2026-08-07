package taskmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileStore is a Store backed by a JSON file tree under a project-local
// directory.  Tasks are stored as <dir>/<task-id>/snapshot.json and
// <dir>/<task-id>/events.jsonl.  It is read-only in TM-02; write support
// is added in TM-04.
type FileStore struct {
	baseDir string // projectDir → task data root (e.g. ".reasonix/tasks")
}

// NewFileStore returns a FileStore rooted at baseDir.  baseDir is typically
// ".reasonix/tasks" relative to the project root.
func NewFileStore(baseDir string) *FileStore {
	return &FileStore{baseDir: baseDir}
}

// safeID validates a user-supplied identifier for use as a filesystem path
// component. It rejects empty strings, ".", "..", and values containing a
// path separator. Used for both taskID and idempotency keys.
func safeID(name string) (string, error) {
	if name == "" {
		return "", errors.New("identifier must not be empty")
	}
	cleaned := filepath.Base(name)
	if cleaned == "." || cleaned == ".." {
		return "", fmt.Errorf("invalid identifier %q", name)
	}
	// Windows accepts both slash styles as path separators. Check both so
	// validation has the same traversal behavior on every platform.
	if strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("identifier %q contains path separator", name)
	}
	return cleaned, nil
}

// taskRoot returns the cleaned directory holding task data for projectDir.
// projectDir is the caller-selected project scope, not a path relative to a
// separate containment root. Parent-relative paths such as ../project and
// directory names containing ".." are therefore valid inputs.
func (s *FileStore) taskRoot(projectDir string) (string, error) {
	if projectDir == "" {
		projectDir = "."
	}
	cleaned := filepath.Clean(projectDir)
	root := filepath.Join(cleaned, s.baseDir)
	if err := rejectStoreParents(cleaned, root); err != nil {
		return "", err
	}
	return root, nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("task store path %q is a symlink", path)
	}
	return nil
}

// rejectSymlinkChain rejects symlinks in the store path itself and all of its
// descendants up to target. This keeps a project-local task id from redirecting
// reads or writes outside the project through an intermediate directory.
func rejectSymlinkChain(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	cur := root
	if err := rejectSymlink(cur); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if err := rejectSymlink(cur); err != nil {
			return err
		}
	}
	return nil
}

func rejectStoreParents(projectDir, root string) error {
	rel, err := filepath.Rel(projectDir, root)
	if err != nil {
		return err
	}
	cur := projectDir
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		if err := rejectSymlink(cur); err != nil {
			return err
		}
	}
	return nil
}

func prepareTaskDir(root, id string) (string, error) {
	taskDir := filepath.Join(root, id)
	if err := rejectSymlinkChain(root, taskDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(taskDir, 0o700); err != nil {
		return "", err
	}
	return taskDir, nil
}

// ListTasks implements Store.
func (s *FileStore) ListTasks(ctx context.Context, projectDir string) ([]TaskSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlink(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []TaskSnapshot{}, nil
		}
		return nil, fmt.Errorf("read task dir %s: %w", root, err)
	}
	result := make([]TaskSnapshot, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		taskDir := filepath.Join(root, e.Name())
		if err := rejectSymlinkChain(root, taskDir); err != nil {
			continue
		}
		snap, err := s.readSnapshot(taskDir)
		if err != nil {
			continue // skip corrupt entries
		}
		reconcileRuntime(&snap, timeNow())
		result = append(result, snap)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

// GetTask implements Store.
func (s *FileStore) GetTask(ctx context.Context, projectDir string, taskID string) (*TaskSnapshot, error) {
	snap, err := s.getTaskRaw(ctx, projectDir, taskID)
	if snap != nil {
		reconcileRuntime(snap, timeNow())
	}
	return snap, err
}

// getTaskRaw returns the persisted snapshot without applying observer-side
// runtime lease reconciliation. Runtime owners use this path when renewing a
// lease after process suspension or system sleep.
func (s *FileStore) getTaskRaw(ctx context.Context, projectDir string, taskID string) (*TaskSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := safeID(taskID)
	if err != nil {
		return nil, err
	}
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkChain(root, filepath.Join(root, id)); err != nil {
		return nil, err
	}
	snap, err := s.readSnapshot(filepath.Join(root, id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

// RenewRuntimeLease implements WriteStore. The raw read plus SaveTask CAS
// ensures a delayed owner cannot overwrite a concurrent control/completion
// update or renew a newer recorder generation.
func (s *FileStore) RenewRuntimeLease(ctx context.Context, projectDir, taskID, ownerID string, leaseUntil time.Time) (bool, error) {
	if ownerID == "" || leaseUntil.IsZero() {
		return false, nil
	}
	const maxAttempts = 4
	for range maxAttempts {
		snap, err := s.getTaskRaw(ctx, projectDir, taskID)
		if err != nil || snap == nil {
			return false, err
		}
		if snap.RuntimeOwnerID != ownerID || snap.State.Terminal() || snap.RuntimeState.Effective() != RuntimeStateAlive {
			return false, nil
		}
		snap.Version++
		snap.RuntimeLeaseUntil = leaseUntil
		if err := s.SaveTask(ctx, projectDir, *snap); err == nil {
			return true, nil
		} else if !errors.Is(err, ErrStoreVersionConflict) {
			return false, err
		}
	}
	return false, ErrStoreVersionConflict
}

// ListEvents implements Store.
func (s *FileStore) ListEvents(ctx context.Context, projectDir string, taskID string, afterSequence int) ([]TaskEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := safeID(taskID)
	if err != nil {
		return nil, err
	}
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkChain(root, filepath.Join(root, id)); err != nil {
		return nil, err
	}
	events, err := s.readEvents(filepath.Join(root, id))
	if err != nil {
		if os.IsNotExist(err) {
			return []TaskEvent{}, nil
		}
		return nil, err
	}
	result := make([]TaskEvent, 0)
	for _, e := range events {
		if e.Sequence > afterSequence {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Sequence < result[j].Sequence
	})
	return result, nil
}

func (s *FileStore) readSnapshot(taskDir string) (TaskSnapshot, error) {
	if err := rejectSymlink(filepath.Join(taskDir, "snapshot.json")); err != nil {
		return TaskSnapshot{}, err
	}
	data, err := os.ReadFile(filepath.Join(taskDir, "snapshot.json"))
	if err != nil {
		return TaskSnapshot{}, err
	}
	var snap TaskSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return TaskSnapshot{}, fmt.Errorf("parse snapshot: %w", err)
	}
	return snap, nil
}

func (s *FileStore) readEvents(taskDir string) ([]TaskEvent, error) {
	if err := rejectSymlink(filepath.Join(taskDir, "events.jsonl")); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(taskDir, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	// JSONL: one JSON object per line
	var events []TaskEvent
	raw := string(data)
	for raw != "" {
		idx := 0
		// find newline
		for idx < len(raw) && raw[idx] != '\n' {
			idx++
		}
		line := raw[:idx]
		raw = raw[idx:]
		if len(raw) > 0 {
			raw = raw[1:] // skip newline
		}
		if line == "" {
			continue
		}
		var ev TaskEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // skip corrupt lines
		}
		events = append(events, ev)
	}
	return events, nil
}

// SaveTask implements WriteStore. It atomically writes the snapshot,
// failing if a concurrent write has changed the version.
func (s *FileStore) SaveTask(ctx context.Context, projectDir string, snap TaskSnapshot) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := safeID(snap.TaskID)
	if err != nil {
		return err
	}
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return err
	}
	taskDir, err := prepareTaskDir(root, id)
	if err != nil {
		return fmt.Errorf("save task: %w", err)
	}

	// Cross-process CAS: hold the per-task lock while reading the current
	// version and replacing snapshot.json, so two writers (CLI + Desktop,
	// or two control operations) cannot both pass the version check and
	// clobber each other. A dedicated lock file is used — never snapshot.json
	// itself, since rename swaps the inode and would orphan the lock.
	lockPath := filepath.Join(taskDir, "task.lock")
	if err := rejectSymlink(lockPath); err != nil {
		return fmt.Errorf("save task: %w", err)
	}
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("save task: open lock: %w", err)
	}
	defer lf.Close()
	if err := lockTaskFile(lf); err != nil {
		return fmt.Errorf("save task: lock: %w", err)
	}
	_ = lf.Chmod(0o600)
	defer func() {
		if unlockErr := unlockTaskFile(lf); unlockErr != nil && retErr == nil {
			retErr = fmt.Errorf("save task: unlock: %w", unlockErr)
		}
	}()

	target := filepath.Join(taskDir, "snapshot.json")
	// Read current version for CAS check (inside the lock).
	current, err := s.readSnapshot(taskDir)
	switch {
	case err == nil && snap.Version <= current.Version:
		return fmt.Errorf("save task: %w: stored=%d, given=%d", ErrStoreVersionConflict, current.Version, snap.Version)
	case err != nil && !os.IsNotExist(err):
		// A corrupt snapshot must fail loudly, never bypass the CAS check.
		return fmt.Errorf("save task: read current snapshot: %w", err)
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("save task: marshal: %w", err)
	}

	// Atomic write via temp file + rename
	tmp, err := os.CreateTemp(taskDir, ".snapshot-*.tmp")
	if err != nil {
		return fmt.Errorf("save task: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("save task: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("save task: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("save task: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("save task: %w", err)
	}
	_ = os.Chmod(target, 0o600)
	return nil
}

// SaveEvent implements WriteStore.
// AppendAuditEvent implements WriteStore. It atomically assigns the next
// monotonic sequence number and appends the event to the JSONL file.
func (s *FileStore) AppendAuditEvent(ctx context.Context, projectDir string, ev TaskEvent) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := safeID(ev.TaskID)
	if err != nil {
		return err
	}
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return err
	}
	taskDir, err := prepareTaskDir(root, id)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}

	// Cross-process atomicity: take the per-task lock (shared with SaveTask)
	// so sequence assignment and snapshot writes never interleave. The
	// events file itself is never renamed, so a dedicated task.lock is
	// sufficient and keeps exactly one lock per task directory.
	lockPath := filepath.Join(taskDir, "task.lock")
	if err := rejectSymlink(lockPath); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("append audit event: open lock: %w", err)
	}
	defer lf.Close()
	if err := lockTaskFile(lf); err != nil {
		return fmt.Errorf("append audit event: lock: %w", err)
	}
	defer func() {
		if unlockErr := unlockTaskFile(lf); unlockErr != nil && retErr == nil {
			retErr = fmt.Errorf("append audit event: unlock: %w", unlockErr)
		}
	}()

	eventsPath := filepath.Join(taskDir, "events.jsonl")
	if err := rejectSymlink(eventsPath); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	f, err := os.OpenFile(eventsPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_ = f.Chmod(0o600)

	// Read current events to compute next sequence (safe under lock)
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	max := 0
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var existing TaskEvent
		if err := json.Unmarshal([]byte(line), &existing); err != nil {
			continue
		}
		if existing.Sequence > max {
			max = existing.Sequence
		}
	}
	ev.Sequence = max + 1
	if err := ev.Validate(); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	// Append at end of locked file
	if _, err := f.Seek(0, 2); err != nil {
		return err
	}
	if _, err := f.WriteString(string(data) + "\n"); err != nil {
		return err
	}
	return nil
}

// ── deprecated: removed NextSequence, SaveEvent — use AppendAuditEvent ──

// CheckIdempotency implements WriteStore.
func (s *FileStore) CheckIdempotency(ctx context.Context, projectDir string, key string) (*IdempotencyRecord, error) {
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return nil, err
	}
	id, err := safeID(key)
	if err != nil {
		return nil, err
	}
	idemDir := filepath.Join(root, ".idempotency")
	if err := rejectSymlink(idemDir); err != nil {
		return nil, err
	}
	if err := rejectSymlink(filepath.Join(idemDir, id+".json")); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(idemDir, id+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rec IdempotencyRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, nil
	}
	return &rec, nil
}

func (s *FileStore) idempotencyPaths(projectDir, key string) (string, string, string, error) {
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return "", "", "", err
	}
	id, err := safeID(key)
	if err != nil {
		return "", "", "", err
	}
	dir := filepath.Join(root, ".idempotency")
	if err := rejectSymlink(dir); err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", "", err
	}
	_ = os.Chmod(dir, 0o700)
	target := filepath.Join(dir, id+".json")
	lock := filepath.Join(dir, id+".lock")
	if err := rejectSymlink(target); err != nil {
		return "", "", "", err
	}
	if err := rejectSymlink(lock); err != nil {
		return "", "", "", err
	}
	return dir, target, lock, nil
}

func (s *FileStore) ClaimIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) (*IdempotencyRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, target, lockPath, err := s.idempotencyPaths(projectDir, r.Key)
	if err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer lf.Close()
	_ = lf.Chmod(0o600)
	if err := lockTaskFile(lf); err != nil {
		return nil, err
	}
	defer func() { _ = unlockTaskFile(lf) }()
	data, err := os.ReadFile(target)
	if err == nil {
		var existing IdempotencyRecord
		if jsonErr := json.Unmarshal(data, &existing); jsonErr != nil {
			return nil, fmt.Errorf("idempotency claim: parse existing record: %w", jsonErr)
		}
		if existing.Pending && timeNow().Sub(existing.ClaimedAt) > 5*time.Minute {
			_ = os.Remove(target)
		} else {
			return &existing, nil
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if r.ClaimedAt.IsZero() {
		r.ClaimedAt = timeNow()
	}
	r.Pending = true
	data, err = json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return nil, err
	}
	_ = os.Chmod(target, 0o600)
	return nil, nil
}

func (s *FileStore) FinalizeIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, target, lockPath, err := s.idempotencyPaths(projectDir, r.Key)
	if err != nil {
		return err
	}
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := lockTaskFile(lf); err != nil {
		return err
	}
	defer func() { _ = unlockTaskFile(lf) }()
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	var existing IdempotencyRecord
	if err := json.Unmarshal(data, &existing); err != nil {
		return err
	}
	if existing.Op != r.Op || existing.TaskID != r.TaskID || existing.Version != r.Version {
		return fmt.Errorf("idempotency key conflict: different params")
	}
	existing.Pending = false
	data, err = json.Marshal(existing)
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return err
	}
	_ = os.Chmod(target, 0o600)
	return nil
}

func (s *FileStore) ReleaseIdempotency(ctx context.Context, projectDir, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, target, lockPath, err := s.idempotencyPaths(projectDir, key)
	if err != nil {
		return err
	}
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := lockTaskFile(lf); err != nil {
		return err
	}
	defer func() { _ = unlockTaskFile(lf) }()
	data, err := os.ReadFile(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var existing IdempotencyRecord
	if err := json.Unmarshal(data, &existing); err != nil {
		return err
	}
	if existing.Pending {
		return os.Remove(target)
	}
	return nil
}

// RecordIdempotency implements WriteStore.
func (s *FileStore) RecordIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) error {
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return err
	}
	id, err := safeID(r.Key)
	if err != nil {
		return err
	}
	idemDir := filepath.Join(root, ".idempotency")
	if err := rejectSymlink(idemDir); err != nil {
		return err
	}
	if err := os.MkdirAll(idemDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(idemDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	target := filepath.Join(idemDir, id+".json")
	if err := rejectSymlink(target); err != nil {
		return err
	}
	// Atomic claim via O_EXCL: fail if file already exists
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			// File exists — read and compare
			existing, rdErr := os.ReadFile(target)
			if rdErr != nil {
				return fmt.Errorf("idempotency conflict: cannot read existing record: %w", rdErr)
			}
			var prev IdempotencyRecord
			if err := json.Unmarshal(existing, &prev); err != nil {
				return fmt.Errorf("idempotency conflict: cannot parse existing record: %w", err)
			}
			if prev.Op != r.Op || prev.TaskID != r.TaskID || prev.Version != r.Version {
				return fmt.Errorf("idempotency key conflict: different params")
			}
			return nil // idempotent
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(target)
		return err
	}
	return f.Close()
}
