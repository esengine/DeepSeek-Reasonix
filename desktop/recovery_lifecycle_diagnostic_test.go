package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
	"reasonix/internal/store"
)

func recoveryLifecycleOutcomes(t *testing.T, sessionPaths ...string) []string {
	t.Helper()
	out := []string{}
	for _, sessionPath := range sessionPaths {
		file, err := os.Open(store.SessionConflictLog(sessionPath))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var record struct {
				Outcome string `json:"outcome"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			out = append(out, record.Outcome)
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestGetRecoveryLineagePersistsRequestedFinalClassification(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "private-root.jsonl")
	branch := filepath.Join(dir, "private-branch.jsonl")
	save := func(path, unique string) {
		t.Helper()
		session := agent.NewSession("system")
		session.Add(provider.Message{Role: provider.RoleUser, Content: "shared"})
		session.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
		session.Add(provider.Message{Role: provider.RoleUser, Content: unique})
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	save(root, "private root content")
	save(branch, "private branch content")
	now := time.Now()
	for path, meta := range map[string]agent.BranchMeta{
		root:   {ID: "private-root", Scope: "global", TopicID: "private-topic", TopicTitle: "private title", CreatedAt: now, UpdatedAt: now},
		branch: {ID: "private-branch", Scope: "global", TopicID: "private-topic", TopicTitle: "private title", Recovered: true, ParentID: "private-root", RecoveryDepth: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	app.sessionCatalog.Store(catalog)

	plain := app.GetRecoveryLineage(ProjectTopicKey{Scope: "global", TopicID: "private-topic", Path: branch})
	if plain.State != "diverged" {
		t.Fatalf("plain classification = %+v", plain)
	}
	if _, err := os.Stat(store.SessionConflictLog(branch)); !os.IsNotExist(err) {
		t.Fatalf("ordinary lineage read wrote diagnostics: %v", err)
	}
	view := app.GetRecoveryLineage(ProjectTopicKey{
		Scope: "global", TopicID: "private-topic", Path: branch, RecordClassification: true,
	})
	if view.State != "diverged" {
		t.Fatalf("recorded classification = %+v", view)
	}
	raw, err := os.ReadFile(store.SessionConflictLog(branch))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"outcome":"classified_diverged"`) {
		t.Fatalf("classification diagnostic = %s", raw)
	}
	for _, secret := range []string{dir, "private-topic", "private title", "private root content", "private branch content"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("classification diagnostic leaked %q: %s", secret, raw)
		}
	}
}

func TestRecoveryCopySweepPersistsCleanupOutcomes(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent, copies := forkThreeIdenticalRecoveryCopies(t, dir, "diagnostics")
	catalog := openSweepTestCatalog(t, dir)
	app := sweepTestApp(catalog)
	app.tabs["open"] = &WorkspaceTab{ID: "open", Scope: "global", SessionPath: copies[1], Ready: true}

	if moved := app.sweepExcessRecoveryCopiesIn(catalog, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}, time.Now(), 0); moved != 1 {
		t.Fatalf("moved = %d, want one moved and one in-use skip", moved)
	}
	outcomes := strings.Join(recoveryLifecycleOutcomes(t, append([]string{parent}, copies...)...), ",")
	for _, want := range []string{"cleanup_kept", "cleanup_skipped_in_use", "cleanup_moved"} {
		if !strings.Contains(outcomes, want) {
			t.Fatalf("cleanup outcomes %q omit %q", outcomes, want)
		}
	}
}

func TestRecoveryCopySweepRecordsFailedRevalidation(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent, copies := forkThreeIdenticalRecoveryCopies(t, dir, "revalidation")
	catalog := openSweepTestCatalog(t, dir)
	changed, err := agent.LoadSession(copies[0])
	if err != nil {
		t.Fatal(err)
	}
	changed.Add(provider.Message{Role: provider.RoleUser, Content: "new unique content after catalog classification"})
	if err := changed.SaveRewrite(copies[0]); err != nil {
		t.Fatal(err)
	}
	app := sweepTestApp(catalog)
	app.sweepExcessRecoveryCopiesIn(catalog, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}, time.Now(), 0)

	outcomes := strings.Join(recoveryLifecycleOutcomes(t, append([]string{parent}, copies...)...), ",")
	if !strings.Contains(outcomes, "cleanup_revalidation_failed") {
		t.Fatalf("cleanup outcomes %q omit failed revalidation", outcomes)
	}
	if _, err := os.Stat(copies[0]); err != nil {
		t.Fatalf("diverged copy was moved despite failed revalidation: %v", err)
	}
}
