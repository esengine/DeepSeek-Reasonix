package main

import (
	"crypto/sha256"
	"encoding/json"
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
		{src: sessionTelemetryPath(sessionPath), name: key + ".telemetry.json"},
		{src: store.SessionCheckpointDir(sessionPath), name: stem + ".ckpt"},
		{src: store.SessionJobsDir(sessionPath), name: stem + ".jobs"},
	}
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
	itemDir := filepath.Join(sessionTrashPath(dir), key)
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
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
	if _, err := os.Stat(itemDir); err == nil {
		return fmt.Errorf("session already exists in trash: %s", key)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func trashSessionArtifactsBeforeMove(dir, sessionPath, key string, beforeMove func()) error {
	if err := validateSessionTrashTarget(dir, sessionPath, key); err != nil {
		return err
	}
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	itemDir := filepath.Join(sessionTrashPath(dir), key)
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
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
		key := e.Name()
		if filepath.Ext(key) != ".jsonl" || filepath.Base(key) != key {
			continue
		}
		path := filepath.Join(root, key, key)
		validPath, _, _, err := validateTrashedSessionPath(dir, path)
		if err != nil {
			continue
		}
		if info, err := os.Stat(validPath); err == nil && !info.IsDir() {
			paths = append(paths, validPath)
		}
	}
	return paths, nil
}

func trashedSessionDeletedAt(path string) int64 {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(path), sessionTrashMetaFile))
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
		return fmt.Errorf("session already exists: %s", key)
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
	if dm := loadSessionDisplays(dir); dm[key] != nil {
		delete(dm, key)
		if err := saveSessionDisplays(dir, dm); err != nil {
			return err
		}
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

// copyAndRemove recursively copies src to dst, then removes src. Used as a
// fallback when os.Rename fails (cross-device or Windows file-lock races).
func copyAndRemove(src, dst string) error {
	if err := copyPath(src, dst); err != nil {
		return err
	}
	// On Windows, wait briefly for any file handle release.
	time.Sleep(10 * time.Millisecond)
	return os.RemoveAll(src)
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
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
		if err := movePathIfExists(artifact.SessionPath, filepath.Join(trashSubagentDir, filepath.Base(artifact.SessionPath))); err != nil {
			return err
		}
		if err := movePathIfExists(artifact.MetaPath, filepath.Join(trashSubagentDir, filepath.Base(artifact.MetaPath))); err != nil {
			return err
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
	if filepath.Ext(absPath) != ".jsonl" {
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
	if filepath.Ext(absPath) != ".jsonl" {
		return "", "", "", fmt.Errorf("not a session file: %s", sessionPath)
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", "", "", fmt.Errorf("session path outside trash dir: %s", sessionPath)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || parts[0] != parts[1] {
		return "", "", "", fmt.Errorf("invalid trash session path: %s", sessionPath)
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

func messageDisplayKey(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

func loadSessionDisplays(dir string) sessionDisplayMap {
	m := sessionDisplayMap{}
	b, err := os.ReadFile(sessionDisplayPath(dir))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
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
