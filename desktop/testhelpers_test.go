package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

// Test-only helpers for the desktop suite. They exercise production state
// through the same package internals, so they live here instead of in the
// shipped binary.

// setSessionTitle sets (or, with an empty title, clears) a session's custom name.
func setSessionTitle(dir, sessionPath, title string) error {
	sessionPath, _, err := validateSessionPath(dir, sessionPath)
	if err != nil {
		return err
	}
	key := filepath.Base(sessionPath)
	return updateSessionTitles(dir, func(m map[string]string) bool {
		title = strings.TrimSpace(title)
		if title == "" {
			if _, ok := m[key]; !ok {
				return false
			}
			delete(m, key)
			return true
		}
		if m[key] == title {
			return false
		}
		m[key] = title
		return true
	})
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

// loadTasks reads tasks from disk.
func (e *HeartbeatEngine) loadTasks() []HeartbeatTask {
	snapshot, err := e.readConfigSnapshot()
	if err != nil {
		return nil
	}
	return snapshot.cfg.Tasks
}

// saveTasks writes tasks to disk atomically.
func (e *HeartbeatEngine) saveTasks(tasks []HeartbeatTask) error {
	return e.writeTasks(tasks, heartbeatConfigSnapshot{}, false)
}

func historyMessages(msgs []provider.Message, resolveUserContent func(string) string) []HistoryMessage {
	return historyMessagesWithPlannerDisplays(msgs, resolveUserContent, nil, nil)
}

func skillRootsView() []SkillRootView {
	cwd, _ := os.Getwd()
	cfg, _ := config.Load()
	userCfg := config.LoadForEdit(config.UserConfigPath())
	return skillRootsViewFrom(cwd, cfg, userCfg)
}

func mcpFailed(ctrl control.SessionAPI, name string) bool {
	if ctrl == nil || ctrl.Host() == nil {
		return false
	}
	for _, f := range ctrl.Host().Failures() {
		if f.Name == name {
			return true
		}
	}
	return false
}

func saveExclusiveExportFiles(targets []string, payloads [][]byte) error {
	return saveExclusiveExportPayloads(targets, len(payloads), func(index int) ([]byte, error) {
		return payloads[index], nil
	})
}

func pathWithinWorktree(path, worktreeRoot string) bool {
	pathKey := canonicalRuntimeRoot(path)
	rootKey := canonicalRuntimeRoot(worktreeRoot)
	if pathKey == "" || rootKey == "" {
		return false
	}
	if pathKey == rootKey {
		return true
	}
	rel, err := filepath.Rel(rootKey, pathKey)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func buildScrollDiagnosticsZip(payload string) ([]byte, error) {
	data, _, err := buildScrollDiagnosticsArchive(payload)
	return data, err
}
