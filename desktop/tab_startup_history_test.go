package main

import (
	"os"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestTerminalStartupFailureRemainsInProjectHistoryAfterRestart(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	topicID := "topic_failed_startup_history"
	topicTitle := "Failed startup history"
	if err := addProject(projectRoot, "Failed startup project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, topicTitle); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	sessionDir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session directory: %v", err)
	}
	sessionPath := writeTopicSession(t, sessionDir, "failed-startup.jsonl", topicID, topicTitle, projectRoot)

	app := NewApp()
	failed := &WorkspaceTab{
		ID: "failed", Scope: "project", WorkspaceRoot: projectRoot,
		TopicID: topicID, TopicTitle: topicTitle, SessionPath: sessionPath,
		Ready: true, disabledMCP: map[string]ServerView{},
	}
	app.tabs[failed.ID] = failed
	app.tabOrder = []string{failed.ID}
	app.activeTabID = failed.ID
	app.mu.Lock()
	app.newSessionRuntimeLocked(failed, sessionRuntimeKey(sessionPath))
	leaseHeld, save := app.markTabStartupFailureLocked(failed, agent.ErrSessionWriteAuthorityMissing, suppressStartupRestore)
	app.mu.Unlock()
	if leaseHeld || save == nil {
		t.Fatalf("terminal failure result: leaseHeld=%v save=%#v", leaseHeld, save)
	}
	app.writeTabsSaveRequest(save)

	persisted := loadTabsFile()
	if len(persisted.Tabs) != 0 || persisted.ActiveTab != "" {
		t.Fatalf("persisted tabs = %#v active=%q, want failed tab omitted", persisted.Tabs, persisted.ActiveTab)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("terminal startup failure removed session history: %v", err)
	}

	restarted := NewApp()
	targetFound := false
	for _, target := range restarted.sessionCatalogTargets() {
		if sameDesktopPath(target.Path, sessionDir) && target.Scope == "project" && sameProjectRoot(target.WorkspaceRoot, projectRoot) {
			targetFound = true
			break
		}
	}
	if !targetFound {
		t.Fatalf("restart catalog targets = %#v, want project session directory %q", restarted.sessionCatalogTargets(), sessionDir)
	}
	restarted.startSessionCatalog()
	t.Cleanup(func() { restarted.stopSessionCatalog(time.Second) })
	waitForCatalogTreeCondition(t, restarted, "failed session history after restart", func(nodes []ProjectNode) bool {
		for _, project := range nodes {
			if project.Kind != "project" || !sameProjectRoot(project.Root, projectRoot) {
				continue
			}
			for _, topic := range project.Children {
				if topic.TopicID == topicID && sameDesktopPath(topic.SessionPath, sessionPath) {
					return true
				}
			}
		}
		return false
	})
}
