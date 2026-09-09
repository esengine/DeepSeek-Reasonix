package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/projectiondb"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func writeSchemaTwoSession(t *testing.T, path string) (*agent.Session, *agent.Session) {
	t.Helper()
	a := agent.NewSession("sys")
	a.Add(provider.Message{Role: provider.RoleUser, Content: "q1"})
	a.Add(provider.Message{Role: provider.RoleAssistant, Content: "a1"})
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{Scope: "global", TopicID: "topic-heads", TopicTitle: "Heads"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	b, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Add(provider.Message{Role: provider.RoleUser, Content: "q2 from a"})
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	b.Add(provider.Message{Role: provider.RoleUser, Content: "q2 from b"})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func TestReconcileProjectsSchemaTwoHeadsFromTheIndex(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "heads.jsonl")
	_, b := writeSchemaTwoSession(t, path)

	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	page, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "global", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("sessions = %+v err=%v", page.Items, err)
	}
	rec := page.Items[0]
	refB, _ := b.Head()
	if rec.LogFormat != 2 || rec.HeadCount != 2 || rec.SelectedHeadID != refB.HeadID {
		t.Fatalf("record = logFormat %d heads %d selected %q, want schema 2 with b's head selected (%q)", rec.LogFormat, rec.HeadCount, rec.SelectedHeadID, refB.HeadID)
	}
	if !strings.Contains(rec.ContentFingerprint, "|h:"+refB.HeadID+":") {
		t.Fatalf("fingerprint %q must bind the selected head", rec.ContentFingerprint)
	}
	if rec.TurnsState != TurnsValid || rec.Recovered || rec.RecoveryRole != RecoveryRoleNormal || !rec.OrdinaryVisible {
		t.Fatalf("schema-2 row must be an ordinary valid session: %+v", rec)
	}
	heads, err := catalog.ListHeads(ctx, path)
	if err != nil || len(heads) != 2 {
		t.Fatalf("heads = %+v err=%v", heads, err)
	}
	if heads[0].ID != agent.SessionMainHead || heads[0].Kind != agent.HeadKindMain || heads[0].Selected {
		t.Fatalf("main head = %+v", heads[0])
	}
	if heads[1].ID != refB.HeadID || heads[1].Kind != agent.HeadKindConcurrent || !heads[1].Selected || heads[1].Turns != 2 || heads[1].Path != path {
		t.Fatalf("concurrent head = %+v", heads[1])
	}
	if none, err := catalog.ListHeads(ctx, filepath.Join(dir, "missing.jsonl")); err != nil || none == nil || len(none) != 0 {
		t.Fatalf("missing session heads = %#v err=%v, want empty slice", none, err)
	}
}

func TestReconcileMarksStaleHeadIndexUnknownAndKeepsRows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "heads.jsonl")
	writeSchemaTwoSession(t, path)
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	// A log that grew without its index (a writer died mid-save) is projected
	// from the sidecar mirror and queued for repair; the head rows stay.
	if err := os.Remove(store.SessionEventIndex(path)); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(store.SessionEventLog(path), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	page, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "global", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("sessions = %+v err=%v", page.Items, err)
	}
	rec := page.Items[0]
	if rec.LogFormat != 2 || rec.HeadCount != 2 || rec.SelectedHeadID == "" || rec.TurnsState != TurnsUnknown {
		t.Fatalf("stale projection = %+v, want schema-2 mirror with unknown counts", rec)
	}
	if heads, err := catalog.ListHeads(ctx, path); err != nil || len(heads) != 2 {
		t.Fatalf("head rows after stale index = %+v err=%v", heads, err)
	}
}

func TestMigrationV12AddsHeadProjectionAndForcesRescan(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{
		Path: path, MemoryName: "session-catalog-v11-test", Migrations: sessionMigrations()[:11], RequireDisk: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO catalog_directories(path,path_key,scope) VALUES('/Sessions','/sessions','global')`,
		`INSERT INTO catalog_sessions(path,path_key,directory,directory_key,scope,topic_id) VALUES('/Sessions/Old.jsonl','/sessions/old.jsonl','/Sessions','/sessions','global','topic')`,
	} {
		if _, err := handle.DB.ExecContext(ctx, statement); err != nil {
			_ = handle.DB.Close()
			t.Fatal(err)
		}
	}
	if err := handle.DB.Close(); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	var directories, sessions, logFormat int
	if err := catalog.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_directories`).Scan(&directories); err != nil {
		t.Fatal(err)
	}
	if err := catalog.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(log_format) FROM catalog_sessions`).Scan(&sessions, &logFormat); err != nil {
		t.Fatal(err)
	}
	if directories != 0 || sessions != 1 || logFormat != 1 {
		t.Fatalf("after v12: directories=%d sessions=%d log_format=%d, want rescan forced and rows kept as schema 1", directories, sessions, logFormat)
	}
	if heads, err := catalog.ListHeads(ctx, "/Sessions/Old.jsonl"); err != nil || len(heads) != 0 {
		t.Fatalf("legacy row heads = %+v err=%v", heads, err)
	}
}
