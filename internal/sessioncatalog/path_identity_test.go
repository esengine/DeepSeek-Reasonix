package sessioncatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent"
)

func TestPathIdentityKeyMatchesFilesystemCaseSemantics(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "MixedCase.jsonl")
	variant := filepath.Join(dir, "mixedcase.jsonl")
	if err := os.WriteFile(original, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalInfo, err := os.Stat(original)
	if err != nil {
		t.Fatal(err)
	}
	variantInfo, variantErr := os.Stat(variant)
	if variantErr == nil && os.SameFile(originalInfo, variantInfo) {
		if got, want := PathIdentityKey(variant), PathIdentityKey(original); got != want {
			t.Fatalf("case-insensitive filesystem keys differ: %q != %q", got, want)
		}
		return
	}
	if variantErr != nil && !os.IsNotExist(variantErr) {
		t.Fatal(variantErr)
	}
	if err := os.WriteFile(variant, []byte("other session"), 0o600); err != nil {
		t.Skipf("filesystem cannot create case-distinct files: %v", err)
	}
	variantInfo, err = os.Stat(variant)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(originalInfo, variantInfo) {
		t.Skip("filesystem aliases the two case spellings")
	}
	if got, other := PathIdentityKey(original), PathIdentityKey(variant); got == other {
		t.Fatalf("case-sensitive filesystem collapsed distinct files to %q", got)
	}
}

func TestUniqueDirectoryTargetsUsesIdentityAndPreservesAccessSpelling(t *testing.T) {
	targets := []DirectoryTarget{
		{Path: filepath.Join("sessions", "Foo"), Scope: "project", WorkspaceRoot: "/work/first"},
		{Path: filepath.Join("sessions", "foo"), Scope: "project", WorkspaceRoot: "/work/second"},
		{Path: "  "},
	}

	folded := uniqueDirectoryTargetsBy(targets, func(path string) string {
		return strings.ToLower(filepath.Clean(path))
	})
	if len(folded) != 1 {
		t.Fatalf("folded targets = %#v, want one", folded)
	}
	if folded[0].Path != filepath.Join("sessions", "Foo") || folded[0].WorkspaceRoot != "/work/first" {
		t.Fatalf("first access spelling was not preserved: %#v", folded[0])
	}

	exact := uniqueDirectoryTargetsBy(targets, filepath.Clean)
	if len(exact) != 2 {
		t.Fatalf("case-sensitive targets = %#v, want two", exact)
	}
}

func TestReconcileDirectoryUsesFilesystemIdentityWhenCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	originalDir := filepath.Join(root, "MixedDirectory")
	variantDir := filepath.Join(root, "mixeddirectory")
	if err := os.Mkdir(originalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(originalDir)
	if err != nil {
		t.Fatal(err)
	}
	variantInfo, err := os.Stat(variantDir)
	if err != nil || !os.SameFile(originalInfo, variantInfo) {
		t.Skip("test requires a case-insensitive filesystem directory")
	}
	sessionPath := filepath.Join(originalDir, "Session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		Scope: "global", TopicID: "topic", SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}

	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for _, dir := range []string{originalDir, variantDir} {
		if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "all", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("case-variant directory scans produced %#v, want one session", page.Items)
	}
	var directories int
	if err := catalog.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_directories`).Scan(&directories); err != nil {
		t.Fatal(err)
	}
	if directories != 1 {
		t.Fatalf("case-variant directory scans produced %d directory rows, want one", directories)
	}
}

func TestCatalogPathIdentityDefersFilesystemResolutionFromEnqueue(t *testing.T) {
	identityCalls := 0
	fold := func(path string) string {
		identityCalls++
		return strings.ToLower(filepath.Clean(path))
	}
	catalog := &Catalog{
		pathIdentity:   fold,
		writeCh:        make(chan string, 2),
		writeQueued:    map[string]SessionRecord{},
		repairCh:       make(chan string, 2),
		reconcileCh:    make(chan DirectoryTarget, 2),
		reconcileDirty: map[string]DirectoryTarget{},
		pathCh:         make(chan sessionPathRequest, 2),
		directoryLocks: map[string]*sync.Mutex{},
		stop:           make(chan struct{}),
	}
	upperPath := filepath.Join(string(filepath.Separator), "Sessions", "Mixed.jsonl")
	lowerPath := filepath.Join(string(filepath.Separator), "sessions", "mixed.jsonl")
	upperDir, lowerDir := filepath.Dir(upperPath), filepath.Dir(lowerPath)

	first := SessionRecord{Path: upperPath, Directory: upperDir}
	second := SessionRecord{Path: lowerPath, Directory: lowerDir}
	if !catalog.EnqueueSession(first) || !catalog.EnqueueSession(second) {
		t.Fatal("write queue rejected a case variant")
	}
	if len(catalog.writeQueued) != 2 || len(catalog.writeCh) != 2 {
		t.Fatalf("lexical write staging = rows=%d signals=%d, want both events", len(catalog.writeQueued), len(catalog.writeCh))
	}
	if !catalog.RequestReconcile(DirectoryTarget{Path: upperDir}) || !catalog.RequestReconcile(DirectoryTarget{Path: lowerDir}) {
		t.Fatal("reconcile queue rejected a case variant")
	}
	queuedReconciles := 0
	catalog.reconcileQueued.Range(func(_, _ any) bool { queuedReconciles++; return true })
	if queuedReconciles != 2 || len(catalog.reconcileCh) != 2 || len(catalog.reconcileDirty) != 0 {
		t.Fatalf("lexical reconcile staging: queued=%d signals=%d dirty=%d", queuedReconciles, len(catalog.reconcileCh), len(catalog.reconcileDirty))
	}
	if !catalog.RequestIndexSession(DirectoryTarget{Path: upperDir}, upperPath) ||
		!catalog.RequestIndexSession(DirectoryTarget{Path: lowerDir}, lowerPath) {
		t.Fatal("direct-index queue rejected a case variant")
	}
	queuedPaths := 0
	catalog.pathQueued.Range(func(_, _ any) bool { queuedPaths++; return true })
	if queuedPaths != 2 || len(catalog.pathCh) != 2 {
		t.Fatalf("lexical direct-index staging: queued=%d signals=%d", queuedPaths, len(catalog.pathCh))
	}
	if identityCalls != 0 {
		t.Fatalf("enqueue APIs performed %d filesystem identity resolutions", identityCalls)
	}
	catalog.enqueueRepair(upperPath)
	catalog.enqueueRepair(lowerPath)
	queuedRepairs := 0
	catalog.repairQueued.Range(func(_, _ any) bool { queuedRepairs++; return true })
	if queuedRepairs != 1 || len(catalog.repairCh) != 1 {
		t.Fatalf("repair queue split identity: queued=%d signals=%d", queuedRepairs, len(catalog.repairCh))
	}
	if catalog.directoryLock(upperDir) != catalog.directoryLock(lowerDir) {
		t.Fatal("case variants acquired different directory locks")
	}
}

func TestCatalogPathIdentityDeduplicatesEveryMutationBoundary(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	catalog.pathIdentity = func(path string) string {
		return strings.ToLower(filepath.Clean(path))
	}

	upperPath := filepath.Join(string(filepath.Separator), "Sessions", "Mixed.jsonl")
	lowerPath := filepath.Join(string(filepath.Separator), "sessions", "mixed.jsonl")
	upperDir := filepath.Dir(upperPath)
	lowerDir := filepath.Dir(lowerPath)
	first := SessionRecord{
		Path: upperPath, Directory: upperDir, Scope: "global", TopicID: "topic",
		Preview: "first", LastActivityAt: 1, TurnsState: TurnsValid, Health: HealthOK,
	}
	second := first
	second.Path = lowerPath
	second.Directory = lowerDir
	second.Preview = "second"
	second.LastActivityAt = 2
	if err := catalog.UpsertSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertSession(ctx, second); err != nil {
		t.Fatal(err)
	}

	page, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "all", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Path != lowerPath || page.Items[0].Preview != "second" {
		t.Fatalf("deduplicated sessions = %#v, want latest access spelling", page.Items)
	}
	if got, ok, err := catalog.GetSession(ctx, upperPath); err != nil || !ok || got.Path != lowerPath {
		t.Fatalf("GetSession(case variant) = %#v, %v, %v", got, ok, err)
	}
	if got, err := catalog.CountDirectorySessions(ctx, upperDir); err != nil || got != 1 {
		t.Fatalf("CountDirectorySessions(case variant) = %d, %v", got, err)
	}

	if err := catalog.RemoveSession(ctx, upperPath, "test"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := catalog.GetSession(ctx, lowerPath); err != nil || ok {
		t.Fatalf("removed case variant remained visible: ok=%v err=%v", ok, err)
	}
}

func TestCatalogPathIdentityPreservesCaseDistinctSessions(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	catalog.pathIdentity = filepath.Clean

	for index, path := range []string{"/sessions/Foo.jsonl", "/sessions/foo.jsonl"} {
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: path, Directory: filepath.Dir(path), Scope: "global", TopicID: "topic",
			LastActivityAt: int64(index + 1), TurnsState: TurnsValid, Health: HealthOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "all", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("case-distinct sessions = %#v, want two", page.Items)
	}
}

func TestWorkspaceRootIdentityJoinsRegistryAndSidecarSpellings(t *testing.T) {
	ctx := context.Background()
	revisionRoots := [][]string{}
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
		OnRevision: func(_ uint64, roots []string, _ string) {
			revisionRoots = append(revisionRoots, append([]string(nil), roots...))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	identityCalls := 0
	catalog.pathIdentity = func(path string) string {
		identityCalls++
		return strings.ToLower(filepath.Clean(path))
	}

	upperRoot := filepath.Join(string(filepath.Separator), "Workspaces", "Project")
	lowerRoot := filepath.Join(string(filepath.Separator), "workspaces", "project")
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: filepath.Join(lowerRoot, "sessions", "one.jsonl"), Directory: filepath.Join(lowerRoot, "sessions"),
		Scope: "project", WorkspaceRoot: lowerRoot, TopicID: "topic", Preview: "from sidecar",
		LastActivityAt: 1, TurnsState: TurnsValid, Health: HealthOK,
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SyncMetadata(ctx,
		[]ProjectRecord{{Scope: "project", WorkspaceRoot: upperRoot, Title: "Project"}},
		[]TopicMetadata{{Scope: "project", WorkspaceRoot: upperRoot, TopicID: "topic", Title: "Registry title"}},
	); err != nil {
		t.Fatal(err)
	}
	revisionRoots = nil
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: filepath.Join(lowerRoot, "sessions", "one.jsonl"), Directory: filepath.Join(lowerRoot, "sessions"),
		Scope: "project", WorkspaceRoot: lowerRoot, TopicID: "topic", Preview: "updated sidecar",
		LastActivityAt: 2, TurnsState: TurnsValid, Health: HealthOK,
	}); err != nil {
		t.Fatal(err)
	}
	if len(revisionRoots) != 1 || len(revisionRoots[0]) != 1 || revisionRoots[0][0] != upperRoot {
		t.Fatalf("sidecar revision roots = %#v, want registry spelling %q", revisionRoots, upperRoot)
	}

	identityCalls = 0
	topics, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "project", WorkspaceRoot: upperRoot, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(topics.Items) != 1 || len(topics.Items[0].Sessions) != 1 {
		t.Fatalf("registry spelling topics = %#v, want joined sidecar session", topics.Items)
	}
	if identityCalls != 1 {
		t.Fatalf("ListTopics workspace identity calls = %d, want one per request", identityCalls)
	}
	sessions, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "project", WorkspaceRoot: upperRoot, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions.Items) != 1 || sessions.Items[0].WorkspaceRoot != lowerRoot {
		t.Fatalf("registry spelling sessions = %#v, want original sidecar access spelling", sessions.Items)
	}
}

func TestCatalogIdentityRemapReplacesSameAccessSpelling(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	root := filepath.Join(dir, "workspace-link")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "project", WorkspaceRoot: root, TopicID: "topic", Preview: "first identity",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	generation := "identity-a:"
	catalog.pathIdentity = func(value string) string {
		return generation + filepath.Clean(value)
	}
	target := DirectoryTarget{Path: dir, Scope: "project", WorkspaceRoot: root}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}

	generation = "identity-b:"
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Preview = "second identity after remap"
		meta.Turns = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"catalog_directories", "catalog_sessions", "catalog_topics"} {
		var count int
		if err := catalog.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s rows after identity remap = %d, want one", table, count)
		}
	}
	got, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok || got.Preview != "second identity after remap" || got.Turns != 2 {
		t.Fatalf("remapped session = %#v, %v, %v", got, ok, err)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "project", WorkspaceRoot: root, Limit: 10})
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Sessions) != 1 {
		t.Fatalf("remapped topics = %#v, %v", page.Items, err)
	}
}

func TestWorkspaceRootIdentityPreservesCaseDistinctProjects(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	catalog.pathIdentity = filepath.Clean

	roots := []string{"/Workspaces/Foo", "/Workspaces/foo"}
	for index, root := range roots {
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: filepath.Join(root, "sessions", fmt.Sprintf("%d.jsonl", index)), Directory: filepath.Join(root, "sessions"),
			Scope: "project", WorkspaceRoot: root, TopicID: "topic", LastActivityAt: int64(index + 1),
			TurnsState: TurnsValid, Health: HealthOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range roots {
		page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "project", WorkspaceRoot: root, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || len(page.Items[0].Sessions) != 1 || page.Items[0].Sessions[0].WorkspaceRoot != root {
			t.Fatalf("case-distinct workspace %q topics = %#v", root, page.Items)
		}
	}
}
