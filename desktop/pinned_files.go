package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/fileutil"
)

const (
	maxPinnedFileCount   = agent.MaxPinnedContextFiles
	maxPinnedFileSize    = agent.MaxPinnedContextFileBytes
	maxPinnedContextSize = agent.MaxPinnedContextRevisionBytes
)

var (
	errPinnedNotRegular   = errors.New("only regular files can be pinned")
	errPinnedFileTooLarge = errors.New("pinned file exceeds the size limit")
)

// PinnedFileInfo holds metadata about one pinned context file.
type PinnedFileInfo struct {
	Path          string `json:"path"`
	SizeBytes     int64  `json:"sizeBytes"`
	TokenEstimate int    `json:"tokenEstimate"`
	Error         string `json:"error,omitempty"`
}

type pinnedContextBuild struct {
	Snapshot agent.PinnedContextSnapshot
	Infos    []PinnedFileInfo
}

// pinnedFileReadHookForTest coordinates deterministic Pin/New/turn races.
// Production leaves it nil.
var pinnedFileReadHookForTest atomic.Pointer[func()]

func normalizePinnedRelPath(relPath string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(relPath)))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" || clean == "." || filepath.IsAbs(relPath) || strings.HasPrefix(clean, "/") {
		return "", errors.New("invalid empty or absolute path")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path traversal outside workspace is forbidden")
	}
	if !utf8.ValidString(clean) {
		return "", errors.New("pinned path is not valid UTF-8")
	}
	return clean, nil
}

func readPinnedWorkspaceFile(root, relPath string) (string, []byte, int64, error) {
	clean, err := normalizePinnedRelPath(relPath)
	if err != nil {
		return "", nil, 0, err
	}
	if strings.TrimSpace(root) == "" {
		return clean, nil, 0, errors.New("tab has no workspace root")
	}
	file, err := fileutil.OpenFileBeneath(root, filepath.FromSlash(clean))
	if err != nil {
		return clean, nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return clean, nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return clean, nil, info.Size(), errPinnedNotRegular
	}
	if hook := pinnedFileReadHookForTest.Load(); hook != nil {
		(*hook)()
	}
	if info.Size() > maxPinnedFileSize {
		return clean, nil, info.Size(), fmt.Errorf("%w: file size (%d bytes) exceeds the %d-byte limit", errPinnedFileTooLarge, info.Size(), maxPinnedFileSize)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPinnedFileSize+1))
	if err != nil {
		return clean, nil, info.Size(), err
	}
	if len(data) > maxPinnedFileSize {
		return clean, nil, int64(len(data)), fmt.Errorf("%w: file grew beyond the %d-byte limit while reading", errPinnedFileTooLarge, maxPinnedFileSize)
	}
	return clean, data, int64(len(data)), nil
}

func buildPinnedContext(root string, files []string) pinnedContextBuild {
	result := pinnedContextBuild{
		Snapshot: agent.PinnedContextSnapshot{
			Files:  make([]agent.PinnedContextFile, 0, len(files)),
			Issues: make([]agent.PinnedContextIssue, 0, len(files)),
		},
		Infos: make([]PinnedFileInfo, 0, len(files)),
	}
	if len(files) == 0 || strings.TrimSpace(root) == "" {
		return result
	}
	for _, rel := range files {
		clean, data, size, err := readPinnedWorkspaceFile(root, rel)
		info := PinnedFileInfo{Path: rel, SizeBytes: size, TokenEstimate: estimateTokensFromBytes(size)}
		if clean != "" {
			info.Path = clean
		}
		if err != nil {
			info.Error = err.Error()
			result.Snapshot.Issues = append(result.Snapshot.Issues, agent.PinnedContextIssue{
				Path: info.Path, Reason: pinnedContextIssueReason(err),
			})
			result.Infos = append(result.Infos, info)
			continue
		}
		content := agent.SanitizePinnedContextContent(string(data))
		if len(content) > maxPinnedFileSize {
			info.Error = fmt.Sprintf("pinned file exceeds the %d-byte limit after XML normalization", maxPinnedFileSize)
			result.Snapshot.Issues = append(result.Snapshot.Issues, agent.PinnedContextIssue{
				Path: clean, Reason: agent.PinnedContextIssueFileTooLarge,
			})
			result.Infos = append(result.Infos, info)
			continue
		}
		candidate, err := agent.NormalizePinnedContextFile(agent.PinnedContextFile{Path: clean, Content: content})
		if err != nil {
			info.Error = err.Error()
			result.Snapshot.Issues = append(result.Snapshot.Issues, agent.PinnedContextIssue{
				Path: clean, Reason: agent.PinnedContextIssueReadFailed,
			})
			result.Infos = append(result.Infos, info)
			continue
		}
		result.Snapshot.Files = append(result.Snapshot.Files, candidate)
		if err := agent.ValidatePinnedContextSnapshot(result.Snapshot); err != nil {
			result.Snapshot.Files = result.Snapshot.Files[:len(result.Snapshot.Files)-1]
			result.Snapshot.Issues = append(result.Snapshot.Issues, agent.PinnedContextIssue{
				Path: clean, Reason: agent.PinnedContextIssueTotalLimit,
			})
			info.Error = fmt.Sprintf("pinned context would exceed the %d-byte total limit", maxPinnedContextSize)
			result.Infos = append(result.Infos, info)
			continue
		}
		result.Infos = append(result.Infos, info)
	}
	return result
}

func pinnedContextIssueReason(err error) agent.PinnedContextIssueReason {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return agent.PinnedContextIssueNotFound
	case errors.Is(err, errPinnedNotRegular):
		return agent.PinnedContextIssueNotRegular
	case errors.Is(err, errPinnedFileTooLarge):
		return agent.PinnedContextIssueFileTooLarge
	default:
		return agent.PinnedContextIssueReadFailed
	}
}

func pinnedContextLoader(root string) control.PinnedContextLoader {
	return func(ctx context.Context, sessionPath string) (agent.PinnedContextSnapshot, error) {
		if err := ctx.Err(); err != nil {
			return agent.PinnedContextSnapshot{}, err
		}
		state, err := loadPinnedContextState(sessionPath)
		if err != nil {
			return agent.PinnedContextSnapshot{}, err
		}
		build := buildPinnedContext(root, state.Files)
		if err := ctx.Err(); err != nil {
			return agent.PinnedContextSnapshot{}, err
		}
		return build.Snapshot, nil
	}
}

func pinnedInfoForPath(infos []PinnedFileInfo, path string) (PinnedFileInfo, bool) {
	for _, info := range infos {
		if info.Path == path {
			return info, true
		}
	}
	return PinnedFileInfo{}, false
}

func (t *WorkspaceTab) setPinnedFiles(files []string) {
	t.setPinnedFilesState(files, nil)
}

func (t *WorkspaceTab) setPinnedFilesState(files, pendingLegacy []string) {
	if t == nil {
		return
	}
	t.pinnedFilesMu.Lock()
	t.PinnedFiles = append([]string(nil), files...)
	t.pendingLegacyPinnedFiles = append([]string(nil), pendingLegacy...)
	t.pinnedFilesMu.Unlock()
}

func (t *WorkspaceTab) pinnedFilesState() ([]string, []string) {
	if t == nil {
		return []string{}, []string{}
	}
	t.pinnedFilesMu.RLock()
	defer t.pinnedFilesMu.RUnlock()
	return append([]string{}, t.PinnedFiles...), append([]string{}, t.pendingLegacyPinnedFiles...)
}

func (t *WorkspaceTab) retainLegacyPinnedFiles(files []string) {
	normalized, err := normalizePinnedContextFiles(files)
	if err != nil {
		normalized = []string{}
	}
	t.setPinnedFilesState(normalized, files)
}

func (t *WorkspaceTab) pendingLegacyPinnedFilesForPersistence() []string {
	_, pending := t.pinnedFilesState()
	return pending
}

func (t *WorkspaceTab) clearPendingLegacyPinnedFiles() {
	if t == nil {
		return
	}
	t.pinnedFilesMu.Lock()
	t.pendingLegacyPinnedFiles = nil
	t.pinnedFilesMu.Unlock()
}

// PinFile updates the tab-local cache. Durable desktop mutations go through
// PinFileForTab so the session sidecar and controller change atomically.
func (t *WorkspaceTab) PinFile(relPath string) (PinnedFileInfo, error) {
	if t == nil {
		return PinnedFileInfo{}, errors.New("tab is nil")
	}
	clean, err := normalizePinnedRelPath(relPath)
	if err != nil {
		return PinnedFileInfo{}, err
	}
	files := t.GetPinnedFiles()
	if slices.Contains(files, clean) {
		build := buildPinnedContext(t.WorkspaceRoot, files)
		info, _ := pinnedInfoForPath(build.Infos, clean)
		return info, nil
	}
	if len(files) >= maxPinnedFileCount {
		return PinnedFileInfo{}, fmt.Errorf("at most %d files can be pinned", maxPinnedFileCount)
	}
	candidate := append(files, clean)
	candidate, err = normalizePinnedContextFiles(candidate)
	if err != nil {
		return PinnedFileInfo{}, err
	}
	build := buildPinnedContext(t.WorkspaceRoot, candidate)
	info, ok := pinnedInfoForPath(build.Infos, clean)
	if !ok {
		return PinnedFileInfo{}, errors.New("pinned file could not be inspected")
	}
	if info.Error != "" {
		return PinnedFileInfo{}, errors.New(info.Error)
	}
	t.setPinnedFiles(candidate)
	return info, nil
}

func (t *WorkspaceTab) UnpinFile(relPath string) error {
	if t == nil {
		return errors.New("tab is nil")
	}
	clean, err := normalizePinnedRelPath(relPath)
	if err != nil {
		return err
	}
	files := t.GetPinnedFiles()
	next := make([]string, 0, len(files))
	for _, path := range files {
		if path != clean {
			next = append(next, path)
		}
	}
	t.setPinnedFiles(next)
	return nil
}

func (t *WorkspaceTab) GetPinnedFiles() []string {
	if t == nil {
		return []string{}
	}
	t.pinnedFilesMu.RLock()
	defer t.pinnedFilesMu.RUnlock()
	return append([]string{}, t.PinnedFiles...)
}

func (t *WorkspaceTab) GetPinnedFilesInfo() []PinnedFileInfo {
	if t == nil {
		return []PinnedFileInfo{}
	}
	return buildPinnedContext(t.WorkspaceRoot, t.GetPinnedFiles()).Infos
}

func estimateTokensFromBytes(bytes int64) int {
	if bytes <= 0 {
		return 0
	}
	tok := int(bytes / 4)
	if tok == 0 {
		return 1
	}
	return tok
}

func (a *App) mutatePinnedFiles(tabID, relPath string, pin bool) (PinnedFileInfo, string, error) {
	unlockRuntime := a.lockRuntimeMutation("pinned context")
	defer unlockRuntime()
	tab := a.tabByID(tabID)
	if tab == nil {
		return PinnedFileInfo{}, "", errors.New("tab not found")
	}
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()

	a.mu.RLock()
	if a.tabs[tab.ID] != tab || tab.removed {
		a.mu.RUnlock()
		return PinnedFileInfo{}, "", errors.New("tab changed while updating pinned context")
	}
	root := tab.WorkspaceRoot
	ctrl := tab.Ctrl
	a.mu.RUnlock()
	if ctrl == nil {
		return PinnedFileInfo{}, "", a.workspaceNotReadyErr(tab)
	}
	if ctrl.RuntimeStatus().Running {
		return PinnedFileInfo{}, "", control.ErrTurnRunning
	}
	sessionPath := ctrl.SessionPath()
	state, err := loadPinnedContextState(sessionPath)
	if err != nil {
		return PinnedFileInfo{}, "", err
	}
	clean, err := normalizePinnedRelPath(relPath)
	if err != nil {
		return PinnedFileInfo{}, "", err
	}
	oldFiles := append([]string(nil), state.Files...)
	candidate := append([]string(nil), oldFiles...)
	alreadyPinned := false
	if pin {
		alreadyPinned = slices.Contains(candidate, clean)
		if !alreadyPinned && len(candidate) >= maxPinnedFileCount {
			return PinnedFileInfo{}, "", fmt.Errorf("at most %d files can be pinned", maxPinnedFileCount)
		}
		if !alreadyPinned {
			candidate = append(candidate, clean)
		}
	} else {
		next := make([]string, 0, len(candidate))
		for _, path := range candidate {
			if path != clean {
				next = append(next, path)
			}
		}
		candidate = next
	}
	candidate, err = normalizePinnedContextFiles(candidate)
	if err != nil {
		return PinnedFileInfo{}, "", err
	}
	build := buildPinnedContext(root, candidate)
	info := PinnedFileInfo{Path: clean}
	if pin {
		var ok bool
		info, ok = pinnedInfoForPath(build.Infos, clean)
		if !ok {
			return PinnedFileInfo{}, "", errors.New("pinned file could not be inspected")
		}
		if info.Error != "" && !alreadyPinned {
			return PinnedFileInfo{}, "", errors.New(info.Error)
		}
	}
	if candidateChanged := strings.Join(oldFiles, "\x00") != strings.Join(candidate, "\x00"); candidateChanged {
		if err := savePinnedContextState(sessionPath, candidate); err != nil {
			return PinnedFileInfo{}, "", err
		}
	}
	tab.setPinnedFiles(candidate)
	return info, tab.ID, nil
}

func (a *App) PinFileForTab(tabID, relPath string) (PinnedFileInfo, error) {
	info, changedTabID, err := a.mutatePinnedFiles(tabID, relPath, true)
	if err != nil {
		return PinnedFileInfo{}, err
	}
	if changedTabID != "" {
		a.emitRuntimeEvent(tabMetaRefreshEventChannel, TabMetaRefreshEvent{TabID: changedTabID, Meta: a.MetaForTab(changedTabID)})
	}
	return info, nil
}

func (a *App) UnpinFileForTab(tabID, relPath string) error {
	_, changedTabID, err := a.mutatePinnedFiles(tabID, relPath, false)
	if err != nil {
		return err
	}
	if changedTabID != "" {
		a.emitRuntimeEvent(tabMetaRefreshEventChannel, TabMetaRefreshEvent{TabID: changedTabID, Meta: a.MetaForTab(changedTabID)})
	}
	return nil
}

func (a *App) GetPinnedFilesForTab(tabID string) ([]PinnedFileInfo, error) {
	tab := a.tabByID(tabID)
	if tab == nil {
		return []PinnedFileInfo{}, errors.New("tab not found")
	}
	a.mu.RLock()
	if a.tabs[tab.ID] != tab || tab.removed {
		a.mu.RUnlock()
		return []PinnedFileInfo{}, errors.New("tab not found")
	}
	root := tab.WorkspaceRoot
	ctrl := tab.Ctrl
	a.mu.RUnlock()
	if ctrl == nil {
		return []PinnedFileInfo{}, a.workspaceNotReadyErr(tab)
	}
	state, err := loadPinnedContextState(ctrl.SessionPath())
	if err != nil {
		return []PinnedFileInfo{}, err
	}
	infos := buildPinnedContext(root, state.Files).Infos
	if infos == nil {
		infos = []PinnedFileInfo{}
	}
	return infos, nil
}
