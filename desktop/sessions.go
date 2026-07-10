package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

// sessions.go holds the desktop-only session-management state that the shared
// kernel doesn't model: custom display titles. A session on disk is just a JSONL
// transcript named by timestamp+model, with no title slot — so the history panel
// stores user-chosen names in a sidecar map (basename → title) next to the .jsonl
// files. The preview (first user message) is the default name; a title overrides
// it. Deleting a session also drops its title entry.

const sessionTitlesFile = ".titles.json"
const sessionDisplayFile = ".display.json"
const sessionPlannerDisplayFile = ".planner-display.json"
const sessionTrashDir = ".trash"
const sessionTrashMetaFile = ".trash-meta.json"

func sessionTitlesPath(dir string) string  { return filepath.Join(dir, sessionTitlesFile) }
func sessionDisplayPath(dir string) string { return filepath.Join(dir, sessionDisplayFile) }
func sessionTrashPath(dir string) string   { return filepath.Join(dir, sessionTrashDir) }

func desktopSessionDir(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return config.SessionDir()
		}
		root = cwd
	}
	if dir := config.ProjectSessionDir(root); dir != "" {
		return dir
	}
	return config.SessionDir()
}

// loadSessionTitles reads the basename→title map (missing/corrupt → empty).
func loadSessionTitles(dir string) map[string]string {
	m := map[string]string{}
	b, err := readFileWithTimeout(sessionTitlesPath(dir), topicFileReadTimeout)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func loadSessionTitlesForUpdate(dir string) (map[string]string, error) {
	return loadStringMapForUpdate(sessionTitlesPath(dir))
}

// saveSessionTitles writes the map atomically (temp file + rename).
func saveSessionTitles(dir string, m map[string]string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".titles.*.tmp")
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
	return fileutil.ReplaceFile(tmpPath, sessionTitlesPath(dir))
}

// setSessionTitle sets (or, with an empty title, clears) a session's custom name.
func setSessionTitle(dir, sessionPath, title string) error {
	sessionPath, _, err := validateSessionPath(dir, sessionPath)
	if err != nil {
		return err
	}
	m, err := loadSessionTitlesForUpdate(dir)
	if err != nil {
		return err
	}
	key := filepath.Base(sessionPath)
	if strings.TrimSpace(title) == "" {
		delete(m, key)
	} else {
		m[key] = strings.TrimSpace(title)
	}
	return saveSessionTitles(dir, m)
}

// deleteSessionFile moves a session's .jsonl and file sidecars into the local
// trash. Title/display sidecars stay in place so trash previews and restores can
// preserve the user's labels.
func deleteSessionFile(dir, sessionPath string) error {
	sessionPath, key, err := validateSessionPath(dir, sessionPath)
	if err != nil {
		return err
	}
	return trashSessionArtifacts(dir, sessionPath, key)
}

type trashedSessionMeta struct {
	Key       string `json:"key"`
	DeletedAt int64  `json:"deletedAt"`
}

type sessionTrashArtifact struct {
	src  string
	name string
}

func sessionTelemetryPath(sessionPath string) string {
	if strings.TrimSpace(sessionPath) == "" {
		return ""
	}
	return sessionPath + ".telemetry.json"
}

func sessionTrashArtifacts(sessionPath, key string) []sessionTrashArtifact {
	stem := strings.TrimSuffix(key, ".jsonl")
	return []sessionTrashArtifact{
		{src: sessionPath, name: key},
		{src: store.SessionMeta(sessionPath), name: key + ".meta"},
		{src: store.SessionGoalState(sessionPath), name: stem + ".goal-state.json"},
		{src: store.SessionEventLog(sessionPath), name: stem + ".events.jsonl"},
		{src: store.SessionEventIndex(sessionPath), name: stem + ".event-index.json"},
		{src: store.SessionConflictLog(sessionPath), name: stem + ".conflicts.jsonl"},
		{src: sessionTelemetryPath(sessionPath), name: key + ".telemetry.json"},
		{src: store.SessionCheckpointDir(sessionPath), name: stem + ".ckpt"},
		{src: store.SessionJobsDir(sessionPath), name: stem + ".jobs"},
	}
}

// errSessionBusyElsewhere is the sanitized error surfaced when a destructive
// session operation is blocked by a live owner. It intentionally carries no
// writer id, hostname, or path.
var errSessionBusyElsewhere = errors.New("session is in use by another Reasonix window or process")

// acquireSessionRemovalGuard wraps agent.TryAcquireSessionRemovalGuard with
// the sanitized busy error. The guard holds the session's save and lease
// locks across the destructive operation and deletes the lock files
// atomically with the release — a one-shot busy probe followed by RemoveAll
// would let another process acquire the lease in between and then lose its
// freshly locked lease file, breaking cross-process mutual exclusion.
func acquireSessionRemovalGuard(sessionPath string) (*agent.SessionRemovalGuard, error) {
	guard, err := agent.TryAcquireSessionRemovalGuard(sessionPath)
	if err != nil {
		if errors.Is(err, agent.ErrSessionLeaseHeld) {
			return nil, errSessionBusyElsewhere
		}
		return nil, err
	}
	return guard, nil
}

func sessionOwnedArtifactPaths(sessionPath string) []string {
	key := filepath.Base(sessionPath)
	artifacts := sessionTrashArtifacts(sessionPath, key)
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.src) != "" {
			paths = append(paths, artifact.src)
		}
	}
	return paths
}

func trashSessionArtifacts(dir, sessionPath, key string) error {
	return trashSessionArtifactsBeforeMove(dir, sessionPath, key, nil)
}

func reconcileDesktopCleanupPending(dir string) error {
	return agent.ReconcileCleanupPending(dir, func(item agent.CleanupPendingInfo) error {
		if strings.TrimSpace(item.Meta.Operation) == "delete" {
			sessionPath, key, err := validateSessionPath(dir, item.SessionPath)
			if err != nil {
				return err
			}
			return reconcileDesktopTrashSessionArtifacts(dir, sessionPath, key)
		}
		return removeDesktopSessionArtifacts(item.SessionPath)
	})
}

func reconcileDesktopTrashSessionArtifacts(dir, sessionPath, key string) error {
	// Hold the removal guard across the whole move so no runtime can acquire
	// the session (or save into it) while its artifacts are relocated; the
	// lock sidecars are deleted atomically with the guard release.
	guard, err := acquireSessionRemovalGuard(sessionPath)
	if err != nil {
		return err
	}
	defer guard.Release()
	itemDir := filepath.Join(sessionTrashPath(dir), key)
	if info, err := os.Stat(itemDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("session trash target is not a directory: %s", key)
		}
		trashPath := filepath.Join(itemDir, key)
		if trashInfo, err := os.Stat(trashPath); err == nil && !trashInfo.IsDir() {
			matches, err := trashSessionMatchesLive(sessionPath, trashPath)
			if err != nil {
				return err
			}
			if !matches {
				itemDir, err = reserveUniqueSessionTrashItemDir(dir, key)
				if err != nil {
					return err
				}
			}
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(itemDir, 0o755); err != nil {
			return err
		}
	} else {
		return err
	}
	for _, artifact := range sessionTrashArtifacts(sessionPath, key) {
		if err := movePathIfExists(artifact.src, filepath.Join(itemDir, artifact.name)); err != nil {
			return err
		}
	}
	if err := trashSubagentArtifacts(dir, sessionPath, itemDir); err != nil {
		return err
	}
	if err := guard.RemoveSidecarsAndRelease(); err != nil {
		return err
	}
	meta := trashedSessionMeta{Key: key, DeletedAt: time.Now().UnixMilli()}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(itemDir, sessionTrashMetaFile), b, 0o644); err != nil {
		return err
	}
	return agent.ClearCleanupPending(sessionPath)
}

func validateSessionTrashTarget(dir, sessionPath, key string) error {
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	itemDir := filepath.Join(sessionTrashPath(dir), key)
	if info, err := os.Stat(itemDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("session trash target is not a directory: %s", key)
		}
		trashPath := filepath.Join(itemDir, key)
		if trashInfo, err := os.Stat(trashPath); err == nil && !trashInfo.IsDir() {
			removable, err := liveSessionRemovableWithExistingTrash(sessionPath, trashPath)
			if err != nil {
				return err
			}
			if removable {
				return nil
			}
			if agent.SessionLeaseHeldByOtherRuntime(sessionPath) {
				return errSessionBusyElsewhere
			}
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

type preparedSessionTrashTarget struct {
	shouldMove     bool
	itemDir        string
	allocateUnique bool
}

func prepareSessionTrashTarget(dir, sessionPath, key string) (preparedSessionTrashTarget, error) {
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return preparedSessionTrashTarget{}, nil
	} else if err != nil {
		return preparedSessionTrashTarget{}, err
	}
	itemDir := filepath.Join(sessionTrashPath(dir), key)
	if info, err := os.Stat(itemDir); err == nil {
		if !info.IsDir() {
			return preparedSessionTrashTarget{}, fmt.Errorf("session trash target is not a directory: %s", key)
		}
		trashPath := filepath.Join(itemDir, key)
		if trashInfo, err := os.Stat(trashPath); err == nil && !trashInfo.IsDir() {
			removable, err := liveSessionRemovableWithExistingTrash(sessionPath, trashPath)
			if err != nil {
				return preparedSessionTrashTarget{}, err
			}
			if removable {
				return preparedSessionTrashTarget{}, removeDesktopSessionArtifacts(sessionPath)
			}
			if agent.SessionLeaseHeldByOtherRuntime(sessionPath) {
				return preparedSessionTrashTarget{}, errSessionBusyElsewhere
			}
			return preparedSessionTrashTarget{shouldMove: true, allocateUnique: true}, nil
		} else if err != nil && !os.IsNotExist(err) {
			return preparedSessionTrashTarget{}, err
		}
		if err := os.RemoveAll(itemDir); err != nil {
			return preparedSessionTrashTarget{}, err
		}
	} else if !os.IsNotExist(err) {
		return preparedSessionTrashTarget{}, err
	}
	return preparedSessionTrashTarget{shouldMove: true, itemDir: itemDir}, nil
}

func reserveUniqueSessionTrashItemDir(dir, key string) (string, error) {
	root := sessionTrashPath(dir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	stem := strings.TrimSuffix(key, ".jsonl")
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("%s.jsonl-deleted-%d-%02d", stem, time.Now().UnixNano(), i)
		itemDir := filepath.Join(root, name)
		if err := os.Mkdir(itemDir, 0o755); err == nil {
			return itemDir, nil
		} else if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate unique trash target for session: %s", key)
}

// liveSessionRemovableWithExistingTrash reports whether a live session file may
// be removed even though a trash copy already exists under the same key: the
// live file must be discardable (empty stub) or byte-identical to the trash
// copy, and no other runtime may hold its session lease — another process could
// be mid-write, and removing the file would silently drop its next save.
func liveSessionRemovableWithExistingTrash(sessionPath, trashPath string) (bool, error) {
	discardable, err := liveSessionDiscardable(sessionPath)
	if err != nil {
		return false, err
	}
	duplicate := false
	if !discardable {
		duplicate, err = trashSessionMatchesLive(sessionPath, trashPath)
		if err != nil {
			return false, err
		}
	}
	if !discardable && !duplicate {
		return false, nil
	}
	return !agent.SessionLeaseHeldByOtherRuntime(sessionPath), nil
}

func liveSessionDiscardable(sessionPath string) (bool, error) {
	if agent.IsCleanupPending(sessionPath) {
		return true, nil
	}
	info, err := os.Stat(sessionPath)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	if info.Size() == 0 {
		return true, nil
	}
	session, err := agent.LoadSession(sessionPath)
	if err != nil {
		return false, nil
	}
	return !session.HasContent(), nil
}

func trashSessionMatchesLive(sessionPath, trashPath string) (bool, error) {
	if _, err := os.Stat(sessionPath); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	// Compare decoded transcripts, not .jsonl bytes: the checkpoint only
	// changes at checkpoints, so two byte-identical .jsonl files can hide
	// diverged event logs — and treating them as duplicates would delete the
	// live session's newer history.
	return agent.SessionsShareContent(sessionPath, trashPath)
}

func sessionFileHasConversationContent(sessionPath string) bool {
	if strings.TrimSpace(sessionPath) == "" || agent.IsCleanupPending(sessionPath) {
		return false
	}
	info, err := os.Stat(sessionPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	session, err := agent.LoadSession(sessionPath)
	if err != nil {
		return false
	}
	return session.HasContent()
}

func trashSessionArtifactsBeforeMove(dir, sessionPath, key string, beforeMove func()) error {
	if err := validateSessionTrashTarget(dir, sessionPath, key); err != nil {
		return err
	}
	target, err := prepareSessionTrashTarget(dir, sessionPath, key)
	if err != nil {
		return err
	}
	if !target.shouldMove {
		return nil
	}
	// Acquired after prepareSessionTrashTarget: the duplicate-trash path in
	// there takes its own removal guard, and the guard is not reentrant.
	guard, err := acquireSessionRemovalGuard(sessionPath)
	if err != nil {
		return err
	}
	defer guard.Release()
	if err := invalidateTopicDirMarkers(dir); err != nil {
		return err
	}
	itemDir := target.itemDir
	if target.allocateUnique {
		itemDir, err = reserveUniqueSessionTrashItemDir(dir, key)
		if err != nil {
			return err
		}
	} else if err := os.MkdirAll(itemDir, 0o755); err != nil {
		return err
	}
	if beforeMove != nil {
		beforeMove()
	}
	for _, artifact := range sessionTrashArtifacts(sessionPath, key) {
		if err := movePathIfExists(artifact.src, filepath.Join(itemDir, artifact.name)); err != nil {
			return err
		}
	}
	if err := trashSubagentArtifacts(dir, sessionPath, itemDir); err != nil {
		return err
	}
	if err := guard.RemoveSidecarsAndRelease(); err != nil {
		return err
	}
	meta := trashedSessionMeta{Key: key, DeletedAt: time.Now().UnixMilli()}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(itemDir, sessionTrashMetaFile), b, 0o644); err != nil {
		return err
	}
	if err := agent.ClearCleanupPending(sessionPath); err != nil {
		return err
	}
	return nil
}

func listTrashedSessionFiles(dir string) ([]string, error) {
	root := sessionTrashPath(dir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	paths := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		itemDir := filepath.Join(root, e.Name())
		keys := []string{}
		if b, err := readFileUTF8(filepath.Join(itemDir, sessionTrashMetaFile)); err == nil {
			var meta trashedSessionMeta
			if json.Unmarshal(b, &meta) == nil && store.IsSessionTranscriptName(meta.Key) {
				keys = append(keys, meta.Key)
			}
		}
		if store.IsSessionTranscriptName(e.Name()) {
			keys = append(keys, e.Name())
		}
		for _, key := range keys {
			path := filepath.Join(itemDir, key)
			validPath, _, _, err := validateTrashedSessionPath(dir, path)
			if err != nil {
				continue
			}
			if info, err := os.Stat(validPath); err == nil && !info.IsDir() {
				paths = append(paths, validPath)
				break
			}
		}
	}
	return paths, nil
}

func trashedSessionDeletedAt(path string) int64 {
	b, err := readFileUTF8(filepath.Join(filepath.Dir(path), sessionTrashMetaFile))
	if err != nil {
		return 0
	}
	var meta trashedSessionMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return 0
	}
	return meta.DeletedAt
}

func restoreTrashedSessionFile(dir, path string) error {
	_, key, itemDir, err := validateTrashedSessionPath(dir, path)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, key)
	if _, err := os.Stat(target); err == nil {
		discardable, err := liveSessionDiscardable(target)
		if err != nil {
			return err
		}
		if !discardable {
			return fmt.Errorf("session already exists: %s", key)
		}
		if err := removeDesktopSessionArtifacts(target); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := checkRestoreSubagentConflicts(dir, itemDir); err != nil {
		return err
	}
	for _, artifact := range sessionTrashArtifacts(target, key) {
		if err := movePathIfExists(filepath.Join(itemDir, artifact.name), artifact.src); err != nil {
			return err
		}
	}
	if err := restoreSubagentArtifacts(dir, itemDir); err != nil {
		return err
	}
	return os.RemoveAll(itemDir)
}

func purgeTrashedSessionFile(dir, path string) error {
	_, key, itemDir, err := validateTrashedSessionPath(dir, path)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(itemDir); err != nil {
		return err
	}
	m, err := loadSessionTitlesForUpdate(dir)
	if err != nil {
		return err
	}
	if _, ok := m[key]; ok {
		delete(m, key)
		if err := saveSessionTitles(dir, m); err != nil {
			return err
		}
	}
	if err := removeSessionDisplayKey(dir, key); err != nil {
		return err
	}
	return nil
}

func movePathIfExists(src, dst string) error {
	if _, err := os.Lstat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// Try os.Rename first — it's atomic and fast when it works.
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if sourcePathMissing(src) {
		return nil
	} else if !isRenameCrossDeviceOrBusy(err) {
		return err
	}
	// Fallback: copy then remove. This handles cross-device moves and the
	// Windows case where a directory rename fails because a handle is briefly
	// held open (e.g. antivirus scan, indexing, or a just-closed file).
	return copyAndRemove(src, dst)
}

// isRenameCrossDeviceOrBusy reports whether err is a cross-device rename or
// a "file busy" error that a copy+remove fallback can recover from.
func isRenameCrossDeviceOrBusy(err error) bool {
	if err == nil {
		return false
	}
	// Cross-device link.
	if le, ok := err.(*os.LinkError); ok {
		if le.Err == syscall.EXDEV {
			return true
		}
		// Windows: "The process cannot access the file because it is being used by another process."
		if errno, ok := le.Err.(syscall.Errno); ok {
			return errno == 32 // ERROR_SHARING_VIOLATION
		}
	}
	return false
}

func sourcePathMissing(src string) bool {
	if strings.TrimSpace(src) == "" {
		return true
	}
	_, err := os.Lstat(src)
	return os.IsNotExist(err)
}

// copyPathFn is a seam for tests to simulate a source vanishing mid-copy.
var copyPathFn = copyPath

// copyAndRemove recursively copies src to dst, then removes src. Used as a
// fallback when os.Rename fails (cross-device or Windows file-lock races).
func copyAndRemove(src, dst string) error {
	if err := copyPathFn(src, dst); err != nil {
		if sourcePathMissing(src) {
			// The source vanished mid-copy; drop the partial destination so
			// the trash never keeps a truncated artifact that a later restore
			// would resurrect as a corrupted transcript.
			_ = os.RemoveAll(dst)
			return nil
		}
		return err
	}
	// On Windows, wait briefly for any file handle release.
	time.Sleep(10 * time.Millisecond)
	return os.RemoveAll(src)
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return copySymlink(src, dst)
	case mode.IsDir():
		return copyDir(src, dst, mode.Perm())
	case mode.IsRegular():
		return copyFile(src, dst, mode.Perm())
	default:
		return fmt.Errorf("unsupported file type in rename fallback: %s", src)
	}
}

func copyDir(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(dst, mode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.RemoveAll(dst)
			return nil
		}
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if err := copyPath(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	// Open source file.
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Create destination file.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		in.Close()
		return err
	}
	// Copy content.
	_, err = io.Copy(out, in)
	// Close both files before any removal.
	closeErr := out.Close()
	in.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Symlink(target, dst)
}

func trashSubagentArtifacts(dir, sessionPath, itemDir string) error {
	artifacts, err := agent.ListSubagentsByParent(dir, agent.BranchID(sessionPath))
	if err != nil {
		return err
	}
	trashSubagentDir := filepath.Join(itemDir, "subagents")
	for _, artifact := range artifacts {
		paths := []string{artifact.SessionPath, artifact.MetaPath}
		paths = append(paths, store.SessionSidecarFiles(artifact.SessionPath)...)
		for _, src := range paths {
			if strings.TrimSpace(src) == "" {
				continue
			}
			if err := movePathIfExists(src, filepath.Join(trashSubagentDir, filepath.Base(src))); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkRestoreSubagentConflicts(dir, itemDir string) error {
	trashSubagentDir := filepath.Join(itemDir, "subagents")
	entries, err := os.ReadDir(trashSubagentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		target := filepath.Join(dir, "subagents", entry.Name())
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("subagent artifact already exists: %s", entry.Name())
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func restoreSubagentArtifacts(dir, itemDir string) error {
	trashSubagentDir := filepath.Join(itemDir, "subagents")
	entries, err := os.ReadDir(trashSubagentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := movePathIfExists(filepath.Join(trashSubagentDir, entry.Name()), filepath.Join(dir, "subagents", entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func validateSessionPath(dir, sessionPath string) (string, string, error) {
	if strings.TrimSpace(sessionPath) == "" {
		return "", "", fmt.Errorf("empty session path")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	path := sessionPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(absDir, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	if !store.IsSessionTranscriptName(filepath.Base(absPath)) {
		return "", "", fmt.Errorf("not a session file: %s", sessionPath)
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("session path outside session dir: %s", sessionPath)
	}
	if info, err := os.Lstat(absPath); err == nil {
		if info.IsDir() {
			return "", "", fmt.Errorf("not a session file: %s", sessionPath)
		}
		realDir, dirErr := filepath.EvalSymlinks(absDir)
		if dirErr != nil {
			realDir = absDir
		}
		realPath, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			return "", "", err
		}
		rel, err := filepath.Rel(realDir, realPath)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
			return "", "", fmt.Errorf("session path escapes session dir: %s", sessionPath)
		}
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	return absPath, filepath.Base(absPath), nil
}

func validateTrashedSessionPath(dir, sessionPath string) (string, string, string, error) {
	if strings.TrimSpace(sessionPath) == "" {
		return "", "", "", fmt.Errorf("empty session path")
	}
	root, err := filepath.Abs(sessionTrashPath(dir))
	if err != nil {
		return "", "", "", err
	}
	path := sessionPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", "", err
	}
	if !store.IsSessionTranscriptName(filepath.Base(absPath)) {
		return "", "", "", fmt.Errorf("not a session file: %s", sessionPath)
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", "", "", fmt.Errorf("session path outside trash dir: %s", sessionPath)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("invalid trash session path: %s", sessionPath)
	}
	if parts[0] != parts[1] {
		b, err := readFileUTF8(filepath.Join(root, parts[0], sessionTrashMetaFile))
		if err != nil {
			return "", "", "", fmt.Errorf("invalid trash session path: %s", sessionPath)
		}
		var meta trashedSessionMeta
		if err := json.Unmarshal(b, &meta); err != nil || meta.Key != parts[1] {
			return "", "", "", fmt.Errorf("invalid trash session path: %s", sessionPath)
		}
	}
	if info, err := os.Lstat(absPath); err == nil {
		if info.IsDir() {
			return "", "", "", fmt.Errorf("not a session file: %s", sessionPath)
		}
		realRoot, dirErr := filepath.EvalSymlinks(root)
		if dirErr != nil {
			realRoot = root
		}
		realPath, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			return "", "", "", err
		}
		rel, err := filepath.Rel(realRoot, realPath)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
			return "", "", "", fmt.Errorf("session path escapes trash dir: %s", sessionPath)
		}
	} else if !os.IsNotExist(err) {
		return "", "", "", err
	}
	return absPath, filepath.Base(absPath), filepath.Dir(absPath), nil
}

type sessionDisplayMap map[string]map[string]string

type sessionPlannerDisplayMap map[string][]plannerDisplayTurn

type plannerDisplayTurn struct {
	UserHash string           `json:"userHash"`
	Messages []HistoryMessage `json:"messages"`
}

func messageDisplayKey(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

func loadSessionDisplays(dir string) sessionDisplayMap {
	m := sessionDisplayMap{}
	b, err := readFileUTF8(sessionDisplayPath(dir))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func sessionPlannerDisplayPath(dir string) string {
	return filepath.Join(dir, sessionPlannerDisplayFile)
}

func loadSessionPlannerDisplays(dir string) sessionPlannerDisplayMap {
	m := sessionPlannerDisplayMap{}
	if strings.TrimSpace(dir) == "" {
		return m
	}
	b, err := readFileUTF8(sessionPlannerDisplayPath(dir))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func saveSessionPlannerDisplays(dir string, m sessionPlannerDisplayMap) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".planner-display.*.tmp")
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
	return fileutil.ReplaceFile(tmpPath, sessionPlannerDisplayPath(dir))
}

func recordSessionPlannerDisplay(dir, sessionPath, userContent string, messages []HistoryMessage) error {
	if strings.TrimSpace(sessionPath) == "" || strings.TrimSpace(userContent) == "" || len(messages) == 0 {
		return nil
	}
	m := loadSessionPlannerDisplays(dir)
	key := filepath.Base(sessionPath)
	turn := plannerDisplayTurn{
		UserHash: messageDisplayKey(userContent),
		Messages: cloneHistoryMessages(messages),
	}
	m[key] = append(m[key], turn)
	return saveSessionPlannerDisplays(dir, m)
}

func sessionPlannerDisplayTurns(dir, sessionPath string) []plannerDisplayTurn {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(sessionPath) == "" {
		return nil
	}
	turns := loadSessionPlannerDisplays(dir)[filepath.Base(sessionPath)]
	if len(turns) == 0 {
		return nil
	}
	out := make([]plannerDisplayTurn, 0, len(turns))
	for _, turn := range turns {
		if strings.TrimSpace(turn.UserHash) == "" || len(turn.Messages) == 0 {
			continue
		}
		out = append(out, plannerDisplayTurn{
			UserHash: turn.UserHash,
			Messages: cloneHistoryMessages(turn.Messages),
		})
	}
	return out
}

func saveSessionDisplays(dir string, m sessionDisplayMap) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".display.*.tmp")
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
	return fileutil.ReplaceFile(tmpPath, sessionDisplayPath(dir))
}

func saveOrRemoveSessionDisplays(dir string, m sessionDisplayMap) error {
	if len(m) == 0 {
		err := os.Remove(sessionDisplayPath(dir))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return saveSessionDisplays(dir, m)
}

func removeSessionDisplayKey(dir, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	m := loadSessionDisplays(dir)
	if m[key] == nil {
		return nil
	}
	delete(m, key)
	return saveOrRemoveSessionDisplays(dir, m)
}

func removeSessionDisplay(dir, sessionPath string) error {
	if strings.TrimSpace(sessionPath) == "" {
		return nil
	}
	return removeSessionDisplayKey(dir, filepath.Base(sessionPath))
}

func pruneSessionDisplays(dir string, protected map[string]struct{}) error {
	m := loadSessionDisplays(dir)
	if len(m) == 0 {
		return nil
	}
	changed := false
	for key := range m {
		if sessionDisplayKeyStillOwned(dir, key, protected) {
			continue
		}
		delete(m, key)
		changed = true
	}
	if !changed {
		return nil
	}
	return saveOrRemoveSessionDisplays(dir, m)
}

func sessionDisplayKeyStillOwned(dir, key string, protected map[string]struct{}) bool {
	key = strings.TrimSpace(key)
	if key == "" || filepath.Base(key) != key || !store.IsSessionTranscriptName(key) {
		return false
	}
	if protected != nil {
		if _, ok := protected[key]; ok {
			return true
		}
	}
	sessionPath := filepath.Join(dir, key)
	if info, err := os.Stat(sessionPath); err == nil && !info.IsDir() {
		return true
	}
	trashPath := filepath.Join(sessionTrashPath(dir), key, key)
	if info, err := os.Stat(trashPath); err == nil && !info.IsDir() {
		return true
	}
	if paths, err := listTrashedSessionFiles(dir); err == nil {
		for _, path := range paths {
			if filepath.Base(path) == key {
				return true
			}
		}
	}
	return false
}

func recordSessionDisplay(dir, sessionPath, content, display string) error {
	if strings.TrimSpace(sessionPath) == "" || content == display || strings.TrimSpace(display) == "" {
		return nil
	}
	m := loadSessionDisplays(dir)
	key := filepath.Base(sessionPath)
	if m[key] == nil {
		m[key] = map[string]string{}
	}
	m[key][messageDisplayKey(content)] = display
	return saveSessionDisplays(dir, m)
}

// sessionDisplayResolver loads the sidecar once and returns a per-message
// resolver, so a transcript of N messages doesn't re-read .display.json N times.
func sessionDisplayResolver(dir, sessionPath string) func(content string) string {
	return sessionDisplayResolverFromMap(loadSessionDisplays(dir), sessionPath)
}

func sessionDisplayResolverFromMap(displays sessionDisplayMap, sessionPath string) func(content string) string {
	byHash := displays[filepath.Base(sessionPath)]
	return func(content string) string {
		if byHash != nil {
			if display := byHash[messageDisplayKey(content)]; strings.TrimSpace(display) != "" {
				return display
			}
		}
		return control.StripComposePrefixes(content)
	}
}

func resolveSessionDisplay(dir, sessionPath, content string) string {
	return sessionDisplayResolver(dir, sessionPath)(content)
}
