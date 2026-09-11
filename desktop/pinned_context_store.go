package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

const (
	pinnedContextSchemaVersion = 1
	maxPinnedContextStateBytes = 64 * 1024
)

type pinnedContextState struct {
	SchemaVersion int      `json:"schemaVersion"`
	SessionID     string   `json:"sessionId"`
	Files         []string `json:"files"`
}

func emptyPinnedContextState(sessionPath string) pinnedContextState {
	return pinnedContextState{
		SchemaVersion: pinnedContextSchemaVersion,
		SessionID:     agent.BranchID(sessionPath),
		Files:         []string{},
	}
}

func normalizePinnedContextFiles(files []string) ([]string, error) {
	out := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, path := range files {
		clean, err := normalizePinnedRelPath(path)
		if err != nil {
			return nil, fmt.Errorf("invalid pinned path %q: %w", path, err)
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	if len(out) > maxPinnedFileCount {
		return nil, fmt.Errorf("at most %d files can be pinned", maxPinnedFileCount)
	}
	sort.Strings(out)
	return out, nil
}

func loadPinnedContextState(sessionPath string) (pinnedContextState, error) {
	sessionPath = strings.TrimSpace(sessionPath)
	state := emptyPinnedContextState(sessionPath)
	if sessionPath == "" {
		return state, nil
	}
	path := store.SessionPinnedContext(sessionPath)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("read pinned context state: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return state, fmt.Errorf("stat pinned context state: %w", err)
	}
	if info.Size() > maxPinnedContextStateBytes {
		return state, fmt.Errorf("pinned context state exceeds %d bytes", maxPinnedContextStateBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxPinnedContextStateBytes+1))
	if err != nil {
		return state, fmt.Errorf("read pinned context state: %w", err)
	}
	if len(raw) > maxPinnedContextStateBytes {
		return state, fmt.Errorf("pinned context state exceeds %d bytes", maxPinnedContextStateBytes)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return emptyPinnedContextState(sessionPath), fmt.Errorf("decode pinned context state: %w", err)
	}
	if state.SchemaVersion != pinnedContextSchemaVersion {
		return emptyPinnedContextState(sessionPath), fmt.Errorf("unsupported pinned context schema version %d", state.SchemaVersion)
	}
	wantID := agent.BranchID(sessionPath)
	if state.SessionID != wantID {
		return emptyPinnedContextState(sessionPath), fmt.Errorf("pinned context belongs to session %q, not %q", state.SessionID, wantID)
	}
	files, err := normalizePinnedContextFiles(state.Files)
	if err != nil {
		return emptyPinnedContextState(sessionPath), err
	}
	state.Files = files
	return state, nil
}

func savePinnedContextState(sessionPath string, files []string) error {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return fmt.Errorf("session is not ready")
	}
	normalized, err := normalizePinnedContextFiles(files)
	if err != nil {
		return err
	}
	state := emptyPinnedContextState(sessionPath)
	state.Files = normalized
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > maxPinnedContextStateBytes {
		return fmt.Errorf("pinned context state exceeds %d bytes", maxPinnedContextStateBytes)
	}
	return fileutil.AtomicWriteFileStrict(store.SessionPinnedContext(sessionPath), raw, 0o600)
}

func loadOrMigratePinnedContextState(sessionPath string, legacy []string) (pinnedContextState, error) {
	if strings.TrimSpace(sessionPath) == "" {
		state := emptyPinnedContextState("")
		files, err := normalizePinnedContextFiles(legacy)
		state.Files = files
		return state, err
	}
	state, err := loadPinnedContextState(sessionPath)
	if err != nil || len(state.Files) > 0 || len(legacy) == 0 {
		return state, err
	}
	if _, statErr := os.Stat(store.SessionPinnedContext(sessionPath)); statErr == nil {
		return state, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return state, statErr
	}
	files, err := normalizePinnedContextFiles(legacy)
	if err != nil {
		return state, err
	}
	if err := savePinnedContextState(sessionPath, files); err != nil {
		return state, err
	}
	state.Files = files
	return state, nil
}

func copyPinnedContextState(sourcePath, targetPath string) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(targetPath) == "" {
		return nil
	}
	if _, err := os.Stat(store.SessionPinnedContext(sourcePath)); errors.Is(err, os.ErrNotExist) {
		return savePinnedContextState(targetPath, []string{})
	} else if err != nil {
		return err
	}
	state, err := loadPinnedContextState(sourcePath)
	if err != nil {
		return err
	}
	return savePinnedContextState(targetPath, state.Files)
}

func loadPinnedContextStateOrEmpty(sessionPath, logMessage string) pinnedContextState {
	state, err := loadPinnedContextState(sessionPath)
	if err == nil {
		return state
	}
	slog.Warn(logMessage, "session", agent.BranchID(sessionPath), "err", err)
	return emptyPinnedContextState(sessionPath)
}

func prepareStartupPinnedContext(tab *WorkspaceTab, startupPath, persistedPath string) {
	if startupPath != "" {
		migratePendingLegacyPinnedFiles(tab, startupPath)
		if len(tab.pendingLegacyPinnedFilesForPersistence()) > 0 {
			return
		}
		state := loadPinnedContextStateOrEmpty(startupPath, "desktop: load startup pinned context")
		tab.setPinnedFiles(state.Files)
	} else if strings.TrimSpace(persistedPath) != "" {
		// A rejected persisted path must not seed its replacement. A pathless
		// legacy entry keeps its cache until the one-time migration runs.
		tab.setPinnedFiles(nil)
	}
}

func restoreTabPinnedContext(tab *WorkspaceTab, legacy []string) {
	state, err := loadOrMigratePinnedContextState(tab.SessionPath, legacy)
	if err != nil {
		tab.retainLegacyPinnedFiles(legacy)
		slog.Warn("desktop: restore pinned context", "err", err)
		return
	}
	if strings.TrimSpace(tab.SessionPath) == "" && len(legacy) > 0 {
		tab.setPinnedFilesState(state.Files, legacy)
		return
	}
	tab.setPinnedFiles(state.Files)
}

func migratePendingLegacyPinnedFiles(tab *WorkspaceTab, sessionPath string) {
	legacy := tab.pendingLegacyPinnedFilesForPersistence()
	if len(legacy) == 0 || strings.TrimSpace(sessionPath) == "" {
		return
	}
	sidecar := store.SessionPinnedContext(sessionPath)
	if _, err := os.Stat(sidecar); err == nil {
		if _, loadErr := loadPinnedContextState(sessionPath); loadErr == nil {
			tab.clearPendingLegacyPinnedFiles()
		}
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		return
	}
	if err := savePinnedContextState(sessionPath, legacy); err != nil {
		slog.Warn("desktop: migrate pending legacy pinned context", "err", err)
		return
	}
	tab.clearPendingLegacyPinnedFiles()
}

func pinnedContextStateForSessionBinding(tab *WorkspaceTab, sessionPath string) (pinnedContextState, bool) {
	pendingLegacy := tab.pendingLegacyPinnedFilesForPersistence()
	_, sidecarErr := os.Stat(store.SessionPinnedContext(sessionPath))
	state, err := loadPinnedContextState(sessionPath)
	if err != nil {
		slog.Warn("desktop: load session pinned context", "session", agent.BranchID(sessionPath), "err", err)
	}
	preserveLegacy := len(pendingLegacy) > 0 && (errors.Is(sidecarErr, os.ErrNotExist) || err != nil)
	return state, preserveLegacy
}

func applyPinnedContextSessionBinding(tab *WorkspaceTab, state pinnedContextState, preserveLegacy bool) {
	if !preserveLegacy {
		tab.setPinnedFiles(state.Files)
	}
}
