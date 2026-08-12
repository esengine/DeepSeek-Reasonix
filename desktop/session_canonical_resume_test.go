package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

func TestResumeSessionPageForTabBindsCanonicalRecoveryPath(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy-path.jsonl")
	legacy := agent.NewSession("system")
	legacy.Add(provider.Message{Role: provider.RoleUser, Content: "legacy-1"})
	legacy.Add(provider.Message{Role: provider.RoleAssistant, Content: "legacy-answer"})
	if err := legacy.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	canonical := agent.NewSession("system")
	canonical.Add(provider.Message{Role: provider.RoleUser, Content: "legacy-1"})
	canonical.Add(provider.Message{Role: provider.RoleAssistant, Content: "legacy-answer"})
	canonical.Add(provider.Message{Role: provider.RoleUser, Content: "canonical-2"})
	canonical.Add(provider.Message{Role: provider.RoleAssistant, Content: "canonical-answer"})
	recovery, err := canonical.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: legacyPath})
	if err != nil {
		t.Fatal(err)
	}
	canonicalPath := recovery.Path

	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}

	exec := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: legacyPath, Label: "legacy"})
	t.Cleanup(ctrl.Close)
	app := NewApp()
	app.sessionCatalog.Store(catalog)
	tab := &WorkspaceTab{ID: "resume-tab", Scope: "global", WorkspaceRoot: globalTabWorkspaceRoot(), SessionPath: legacyPath, Ctrl: ctrl, Ready: true, sink: &tabEventSink{tabID: "resume-tab", app: app}, disabledMCP: map[string]ServerView{}}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	if got := app.resolveCanonicalSessionPath(legacyPath); !sameDesktopPath(got, canonicalPath) {
		legacy, legacyOK, legacyErr := catalog.GetSession(context.Background(), legacyPath)
		canonical, canonicalOK, canonicalErr := catalog.GetSession(context.Background(), canonicalPath)
		page, pageErr := catalog.ListSessions(context.Background(), sessioncatalog.SessionPageRequest{Scope: "global", Limit: sessioncatalog.MaxLimit})
		t.Fatalf("resolver = %q; legacy=(%+v,%v,%v); canonical=(%+v,%v,%v); page=(%+v,%v)", got, legacy, legacyOK, legacyErr, canonical, canonicalOK, canonicalErr, page.Items, pageErr)
	}

	page, err := app.ResumeSessionPageForTab(tab.ID, legacyPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got := app.tabs[tab.ID].Ctrl.SessionPath(); !sameDesktopPath(got, canonicalPath) {
		t.Fatalf("bound path = %q, want canonical %q", got, canonicalPath)
	}
	if !page.Redirected || !sameDesktopPath(page.ResolvedPath, canonicalPath) {
		t.Fatalf("resolution = redirected:%v path:%q, want canonical %q", page.Redirected, page.ResolvedPath, canonicalPath)
	}
	if len(page.Messages) != 5 || page.Messages[3].Content != "canonical-2" {
		t.Fatalf("page = %+v, want canonical history", page.Messages)
	}
	if got := app.tabs[tab.ID].Ctrl.History(); len(got) != 5 || got[3].Content != "canonical-2" {
		t.Fatalf("bound controller history = %+v, want canonical history", got)
	}
}

func TestResumeSessionPageForTabCanonicalResolutionBoundaries(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Run("already canonical is idempotent", func(t *testing.T) {
		app, tab, _, canonicalPath := canonicalResumeFixture(t)
		page, err := app.ResumeSessionPageForTab(tab.ID, canonicalPath, 20)
		if err != nil {
			t.Fatal(err)
		}
		if page.Redirected || !sameDesktopPath(page.ResolvedPath, canonicalPath) {
			t.Fatalf("page resolution = %+v", page)
		}
		if got := tab.Ctrl.SessionPath(); !sameDesktopPath(got, canonicalPath) {
			t.Fatalf("bound path = %q, want %q", got, canonicalPath)
		}
	})

	t.Run("missing path preserves error and binding", func(t *testing.T) {
		app, tab, legacyPath, _ := canonicalResumeFixture(t)
		missing := filepath.Join(filepath.Dir(legacyPath), "missing.jsonl")
		_, err := app.ResumeSessionPageForTab(tab.ID, missing, 20)
		if err == nil {
			t.Fatal("expected missing path error")
		}
		if got := tab.Ctrl.SessionPath(); !sameDesktopPath(got, legacyPath) {
			t.Fatalf("binding changed to %q", got)
		}
		if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
			t.Fatalf("missing path was created: %v", statErr)
		}
	})

	t.Run("ordinary session stays on requested path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "ordinary.jsonl")
		s := agent.NewSession("system")
		s.Add(provider.Message{Role: provider.RoleUser, Content: "ordinary"})
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
		app, tab := resumeTestApp(t, dir, path)
		catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = catalog.Close(context.Background()) })
		if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
			t.Fatal(err)
		}
		app.sessionCatalog.Store(catalog)
		page, err := app.ResumeSessionPageForTab(tab.ID, path, 20)
		if err != nil || page.Redirected || page.SelectionRequired || !sameDesktopPath(page.ResolvedPath, path) {
			t.Fatalf("page=%+v err=%v", page, err)
		}
	})
}

func TestResumeSessionPageForTabRequiresExplicitRecoveryCandidate(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "ambiguous.jsonl")
	base := agent.NewSession("system")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
	base.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if err := base.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	makeBranch := func(tail string) string {
		s := agent.NewSession("system")
		s.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
		s.Add(provider.Message{Role: provider.RoleUser, Content: tail})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: tail + "-answer"})
		info, err := s.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: legacyPath})
		if err != nil {
			t.Fatal(err)
		}
		return info.Path
	}
	firstPath := makeBranch("first-candidate")
	secondPath := makeBranch("second-candidate")
	app, tab := resumeTestApp(t, dir, legacyPath)
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	app.sessionCatalog.Store(catalog)

	page, err := app.ResumeSessionPageForTab(tab.ID, legacyPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !page.SelectionRequired || len(page.RecoveryCandidates) != 2 {
		t.Fatalf("selection result = %+v", page)
	}
	if got := tab.Ctrl.SessionPath(); !sameDesktopPath(got, legacyPath) {
		t.Fatalf("selection rebound tab to %q", got)
	}
	resolution := app.ResolveRecoverySession(legacyPath)
	if !resolution.SelectionRequired || len(resolution.RecoveryCandidates) != 2 || !sameDesktopPath(tab.Ctrl.SessionPath(), legacyPath) {
		t.Fatalf("read-only resolution = %+v, bound=%q", resolution, tab.Ctrl.SessionPath())
	}
	for _, candidate := range page.RecoveryCandidates {
		if candidate.LastActivityAt == 0 || candidate.Turns != 2 || strings.TrimSpace(candidate.Summary) == "" || !strings.HasSuffix(candidate.Summary, "-answer") {
			t.Fatalf("incomplete candidate = %+v", candidate)
		}
	}
	if _, err := app.ResumeRecoveryCandidatePageForTab(tab.ID, legacyPath, filepath.Join(dir, "unrelated.jsonl"), 20); err == nil {
		t.Fatal("unrelated candidate was accepted")
	}
	if confirmedPath, err := app.ConfirmRecoverySessionCandidate(legacyPath, secondPath); err != nil || !sameDesktopPath(confirmedPath, secondPath) {
		t.Fatalf("confirmed path=%q err=%v", confirmedPath, err)
	}
	confirmed, err := app.ResumeRecoveryCandidatePageForTab(tab.ID, legacyPath, secondPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDesktopPath(confirmed.ResolvedPath, secondPath) || !sameDesktopPath(tab.Ctrl.SessionPath(), secondPath) {
		t.Fatalf("confirmation=%+v bound=%q", confirmed, tab.Ctrl.SessionPath())
	}
	if len(confirmed.Messages) != 5 || confirmed.Messages[3].Content != "second-candidate" {
		t.Fatalf("confirmed messages = %+v", confirmed.Messages)
	}
	if sameDesktopPath(firstPath, secondPath) {
		t.Fatal("fixture branches unexpectedly share a path")
	}
}

func TestResumeSessionPageForTabPersistsOnlyCanonicalAndReopens(t *testing.T) {
	isolateDesktopUserDirs(t)
	app, tab, legacyPath, canonicalPath := canonicalResumeFixture(t)
	legacyBefore, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ResumeSessionPageForTab(tab.ID, legacyPath, 20); err != nil {
		t.Fatal(err)
	}
	writePath := tab.Ctrl.SessionPath()
	if !sameDesktopPath(writePath, canonicalPath) {
		t.Fatalf("write path = %q, want canonical %q", writePath, canonicalPath)
	}
	continued, err := agent.LoadSession(writePath)
	if err != nil {
		t.Fatal(err)
	}
	continued.Add(provider.Message{Role: provider.RoleUser, Content: "post-resume-canonical-write"})
	if err := continued.SaveSnapshot(writePath); err != nil {
		t.Fatal(err)
	}
	legacyAfter, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(legacyAfter) != string(legacyBefore) {
		t.Fatal("legacy copy changed after canonical write")
	}
	canonicalDisk, err := agent.LoadSession(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := canonicalDisk.Snapshot(); got[len(got)-1].Content != "post-resume-canonical-write" {
		t.Fatalf("canonical tail = %+v", got)
	}

	tab.Ctrl.Close()
	tab.releaseSessionLease()
	reopenedApp, reopenedTab := resumeTestApp(t, filepath.Dir(legacyPath), legacyPath)
	reopenedApp.sessionCatalog.Store(app.sessionCatalog.Load())
	page, err := reopenedApp.ResumeSessionPageForTab(reopenedTab.ID, legacyPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDesktopPath(reopenedTab.Ctrl.SessionPath(), canonicalPath) {
		t.Fatalf("reopened path = %q, want %q", reopenedTab.Ctrl.SessionPath(), canonicalPath)
	}
	found := false
	for _, message := range page.Messages {
		if message.Content == "post-resume-canonical-write" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reopened page lacks canonical write: %+v", page.Messages)
	}
}

func TestOpenTopicSessionRequiresConfirmedRecoveryCandidate(t *testing.T) {
	isolateDesktopUserDirs(t)
	app, tab, legacyPath, _ := ambiguousCanonicalResumeFixture(t)
	beforeTabs := len(app.tabs)

	if _, err := app.OpenTopicSession("global", "", "", legacyPath); err == nil {
		t.Fatal("ambiguous recovery session opened without an explicit choice")
	}
	if len(app.tabs) != beforeTabs || !sameDesktopPath(tab.Ctrl.SessionPath(), legacyPath) {
		t.Fatalf("failed open changed state: tabs=%d bound=%q", len(app.tabs), tab.Ctrl.SessionPath())
	}

	resolution := app.ResolveRecoverySession(legacyPath)
	if !resolution.SelectionRequired || len(resolution.RecoveryCandidates) != 2 {
		t.Fatalf("resolution = %+v", resolution)
	}
	selectedPath := resolution.RecoveryCandidates[0].Path
	meta, err := app.OpenConfirmedTopicSession("global", "", "", legacyPath, selectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDesktopPath(meta.SessionPath, selectedPath) {
		t.Fatalf("opened path = %q, want %q", meta.SessionPath, selectedPath)
	}

	unrelated := filepath.Join(filepath.Dir(legacyPath), "unrelated.jsonl")
	unrelatedSession := agent.NewSession("system")
	unrelatedSession.Add(provider.Message{Role: provider.RoleUser, Content: "unrelated"})
	if err := unrelatedSession.Save(unrelated); err != nil {
		t.Fatal(err)
	}
	if _, err := app.OpenConfirmedTopicSession("global", "", "", legacyPath, unrelated); err == nil {
		t.Fatal("unrelated selected session was accepted")
	}
}

func TestStartTopicActivationAcceptsConfirmedRecoveryCandidate(t *testing.T) {
	isolateDesktopUserDirs(t)
	app, _, legacyPath, _ := ambiguousCanonicalResumeFixture(t)
	app.readyHook = func() {}
	installNoopRuntimeEvents(app)
	events := newActivationEventRecorder(app)
	resolution := app.ResolveRecoverySession(legacyPath)
	selectedPath := resolution.RecoveryCandidates[1].Path

	ticket, err := app.StartTopicActivation(TopicActivationRequest{
		Scope:                "global",
		SessionPath:          selectedPath,
		RecoveryOriginalPath: legacyPath,
		RequestID:            "confirmed-recovery",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sameDesktopPath(ticket.Meta.SessionPath, selectedPath) {
		t.Fatalf("ticket path = %q, want %q", ticket.Meta.SessionPath, selectedPath)
	}
	if got := events.next(t); got.RequestID != "confirmed-recovery" || got.Phase != topicActivationPhaseStarting {
		t.Fatalf("event = %+v", got)
	}
	if got := events.waitFor(t, activationEventFor("confirmed-recovery", topicActivationPhaseReady)); got.TabID != ticket.TabID {
		t.Fatalf("ready event = %+v, ticket = %+v", got, ticket)
	}
}

func canonicalResumeFixture(t *testing.T) (*App, *WorkspaceTab, string, string) {
	t.Helper()
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.jsonl")
	legacy := agent.NewSession("system")
	legacy.Add(provider.Message{Role: provider.RoleUser, Content: "legacy"})
	legacy.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if err := legacy.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	canonical := agent.NewSession("system")
	canonical.Add(provider.Message{Role: provider.RoleUser, Content: "legacy"})
	canonical.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	canonical.Add(provider.Message{Role: provider.RoleUser, Content: "latest"})
	canonical.Add(provider.Message{Role: provider.RoleAssistant, Content: "latest-answer"})
	info, err := canonical.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: legacyPath})
	if err != nil {
		t.Fatal(err)
	}
	app, tab := resumeTestApp(t, dir, legacyPath)
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	app.sessionCatalog.Store(catalog)
	return app, tab, legacyPath, info.Path
}

func ambiguousCanonicalResumeFixture(t *testing.T) (*App, *WorkspaceTab, string, []string) {
	t.Helper()
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, "ambiguous.jsonl")
	base := agent.NewSession("system")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
	base.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if err := base.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, 2)
	for _, tail := range []string{"first", "second"} {
		s := agent.NewSession("system")
		s.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
		s.Add(provider.Message{Role: provider.RoleUser, Content: tail})
		info, err := s.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: legacyPath})
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, info.Path)
	}
	app, tab := resumeTestApp(t, dir, legacyPath)
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	app.sessionCatalog.Store(catalog)
	return app, tab, legacyPath, paths
}

func resumeTestApp(t *testing.T, dir, path string) (*App, *WorkspaceTab) {
	t.Helper()
	exec := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	t.Cleanup(ctrl.Close)
	app := NewApp()
	tab := &WorkspaceTab{ID: "resume-tab", Scope: "global", WorkspaceRoot: globalTabWorkspaceRoot(), SessionPath: path, Ctrl: ctrl, Ready: true, sink: &tabEventSink{tabID: "resume-tab", app: app}, disabledMCP: map[string]ServerView{}}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	return app, tab
}
