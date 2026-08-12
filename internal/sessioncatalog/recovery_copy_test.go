package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestDefaultPathUsesV2CacheFile(t *testing.T) {
	t.Parallel()
	path := DefaultPath()
	if path == "" {
		// CacheDir unavailable in this environment; empty is still valid.
		return
	}
	if !strings.HasSuffix(filepath.ToSlash(path), "session-catalog/v2.sqlite") {
		t.Fatalf("DefaultPath = %q, want .../session-catalog/v2.sqlite", path)
	}
	if strings.Contains(path, "v1.sqlite") {
		t.Fatalf("DefaultPath must not reuse the 1.24.0 v1 cache: %q", path)
	}
}

func forkCatalogRecoveryBranch(t *testing.T, dir, name string) (parentPath, branchPath string, branchMsgs int) {
	t.Helper()
	parentPath = filepath.Join(dir, name+".jsonl")
	parent := agent.NewSession("sys")
	parent.Add(agentMessage("user", "first"))
	parent.Add(agentMessage("assistant", "one"))
	parent.Add(agentMessage("user", "disk "+name))
	if err := parent.Save(parentPath); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	stale := agent.NewSession("sys")
	stale.Add(agentMessage("user", "first"))
	stale.Add(agentMessage("assistant", "one"))
	stale.Add(agentMessage("user", "local "+name))
	info, err := stale.SaveRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: parentPath})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	return parentPath, info.Path, len(stale.Snapshot())
}

func coverCatalogRecoveryParent(t *testing.T, parentPath, branchPath string) {
	t.Helper()
	branch, err := agent.LoadSession(branchPath)
	if err != nil {
		t.Fatalf("Load branch: %v", err)
	}
	parent, err := agent.LoadSession(parentPath)
	if err != nil {
		t.Fatalf("Load parent: %v", err)
	}
	parent.Replace(append([]provider.Message(nil), branch.Snapshot()...))
	parent.Add(agentMessage("assistant", "parent kept the recovery content"))
	if err := parent.SaveRewrite(parentPath); err != nil {
		t.Fatalf("Save covering parent: %v", err)
	}
}

func agentMessage(role, content string) provider.Message {
	switch role {
	case "assistant":
		return provider.Message{Role: provider.RoleAssistant, Content: content}
	default:
		return provider.Message{Role: provider.RoleUser, Content: content}
	}
}

func assignCatalogTopic(t *testing.T, path, topicID string) {
	t.Helper()
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Scope = "global"
		meta.TopicID = topicID
		meta.TopicTitle = topicID
		meta.SchemaVersion = agent.BranchMetaCountsVersion
		return nil
	}); err != nil {
		t.Fatalf("UpdateBranchMeta %s: %v", path, err)
	}
}

func TestReconcileMarksCoveredRecoveryCopyWithoutMutatingSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	parentPath, coveredPath, _ := forkCatalogRecoveryBranch(t, dir, "covered")
	coverCatalogRecoveryParent(t, parentPath, coveredPath)
	_, divergedPath, _ := forkCatalogRecoveryBranch(t, dir, "diverged")
	assignCatalogTopic(t, parentPath, "shared")
	assignCatalogTopic(t, coveredPath, "shared")
	assignCatalogTopic(t, divergedPath, "adopted")

	// Capture pre-index fingerprints so a catalog open never rewrites authority.
	hashFile := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	before := map[string]string{
		parentPath:   hashFile(parentPath),
		coveredPath:  hashFile(coveredPath),
		divergedPath: hashFile(divergedPath),
	}
	for _, path := range []string{parentPath, coveredPath, divergedPath} {
		before[agent.BranchMetaPath(path)] = hashFile(agent.BranchMetaPath(path))
	}

	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}

	covered, ok, err := catalog.GetSession(ctx, coveredPath)
	if err != nil || !ok {
		t.Fatalf("GetSession covered: ok=%v err=%v", ok, err)
	}
	if !covered.Recovered || !covered.RecoveryCopy {
		t.Fatalf("covered record = %+v, want recovered recoveryCopy", covered)
	}
	diverged, ok, err := catalog.GetSession(ctx, divergedPath)
	if err != nil || !ok {
		t.Fatalf("GetSession diverged: ok=%v err=%v", ok, err)
	}
	if !diverged.Recovered || diverged.RecoveryCopy {
		t.Fatalf("diverged record = %+v, want recovered without recoveryCopy", diverged)
	}
	parent, ok, err := catalog.GetSession(ctx, parentPath)
	if err != nil || !ok || parent.RecoveryCopy {
		t.Fatalf("parent record = %+v ok=%v err=%v", parent, ok, err)
	}

	shared, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "shared"})
	if err != nil || !ok {
		t.Fatalf("GetTopic shared: ok=%v err=%v", ok, err)
	}
	if shared.Turns != parent.Turns {
		t.Fatalf("shared turns = %d, want parent turns %d without copy inflation", shared.Turns, parent.Turns)
	}
	if shared.RecoveryState == "recovery_only" {
		t.Fatalf("shared topic should not be recovery_only: %+v", shared)
	}
	adopted, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "adopted"})
	if err != nil || !ok {
		t.Fatalf("GetTopic adopted: ok=%v err=%v", ok, err)
	}
	if adopted.RecoveryState == "recovery_only" || adopted.Turns <= 0 {
		t.Fatalf("adopted topic = %+v, want visible continued recovery with turns", adopted)
	}

	for path, want := range before {
		if got := hashFile(path); got != want {
			t.Fatalf("authoritative file changed during catalog reconcile: %s", path)
		}
	}
}

func TestTopicTurnsIgnoreRecoveryCopyButKeepActivity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC).UnixMilli()
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: "/s/parent.jsonl", Directory: "/s", Scope: "global", TopicID: "t",
		Turns: 3, TurnsState: TurnsValid, Health: HealthOK, LastActivityAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: "/s/copy.jsonl", Directory: "/s", Scope: "global", TopicID: "t",
		Turns: 9, TurnsState: TurnsValid, Health: HealthOK, Recovered: true, RecoveryCopy: true,
		LastActivityAt: base + 60_000,
	}); err != nil {
		t.Fatal(err)
	}
	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "t"})
	if err != nil || !ok {
		t.Fatalf("GetTopic: ok=%v err=%v", ok, err)
	}
	if topic.Turns != 3 {
		t.Fatalf("turns = %d, want 3", topic.Turns)
	}
	if topic.LastActivityAt != base+60_000 {
		t.Fatalf("lastActivityAt = %d, want recovery activity", topic.LastActivityAt)
	}
	if topic.RecoveryState != "" {
		t.Fatalf("recovery_state = %q, want empty for mixed topic", topic.RecoveryState)
	}
}

func TestTopicTurnsUseMaxOfNormalLineageAndAdoptedRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	upsert := func(path string, turns int, recovered, recoveryCopy bool) {
		t.Helper()
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: path, Directory: "/s", Scope: "global", TopicID: "t",
			Turns: turns, TurnsState: TurnsValid, Health: HealthOK,
			Recovered: recovered, RecoveryCopy: recoveryCopy,
		}); err != nil {
			t.Fatal(err)
		}
	}
	upsert("/s/parent.jsonl", 3, false, false)
	upsert("/s/adopted.jsonl", 5, true, false)
	upsert("/s/copy.jsonl", 99, true, true)

	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "t"})
	if err != nil || !ok {
		t.Fatalf("GetTopic: ok=%v err=%v", ok, err)
	}
	if topic.Turns != 5 {
		t.Fatalf("turns = %d, want max(normal=3, adopted=5); recovery copy must not inflate", topic.Turns)
	}

	upsert("/s/second-normal.jsonl", 4, false, false)
	topic, ok, err = catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "t"})
	if err != nil || !ok {
		t.Fatalf("GetTopic after second normal: ok=%v err=%v", ok, err)
	}
	if topic.Turns != 7 {
		t.Fatalf("turns = %d, want max(normal=7, adopted=5)", topic.Turns)
	}
}

func TestRecoveryOnlyTopicState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: "/s/only-copy.jsonl", Directory: "/s", Scope: "global", TopicID: "copy-only",
		Turns: 4, TurnsState: TurnsValid, Health: HealthOK, Recovered: true, RecoveryCopy: true,
		LastActivityAt: 100,
	}); err != nil {
		t.Fatal(err)
	}
	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "copy-only"})
	if err != nil || !ok {
		t.Fatalf("GetTopic: ok=%v err=%v", ok, err)
	}
	if topic.RecoveryState != "recovery_only" {
		t.Fatalf("recovery_state = %q, want recovery_only", topic.RecoveryState)
	}
	if topic.Turns != 0 {
		t.Fatalf("turns = %d, want 0 for recovery-only topic", topic.Turns)
	}
}
