package control

import (
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func foldNoticeCodes(events []event.Event) []string {
	var out []string
	for _, e := range events {
		if e.Kind == event.Notice && (e.Code == event.NoticeCodeFoldNotReusedHistory || e.Code == event.NoticeCodeFoldNotReusedLineage) {
			out = append(out, e.Code)
		}
	}
	return out
}

func foldController(t *testing.T, sink event.Sink, coversHash string) (*Controller, *sessionstore.Session, string) {
	t.Helper()
	dir := testenv.TempDir(t)
	saved := sessionstore.NewSession("sys")
	saved.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	path := sessionstore.NewSessionPath(dir, "test")
	if err := saved.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := sessionstore.EnsureBranchMeta(path); err != nil {
		t.Fatalf("meta: %v", err)
	}
	if err := sessionstore.SaveCompactionState(path, sessionstore.CompactionState{
		SchemaVersion: sessionstore.CompactionStateSchemaV1,
		Projection: sessionstore.ContextProjection{
			Messages:          []provider.Message{{Role: provider.RoleSystem, Content: "summary"}},
			CoveredCount:      2,
			CoveredPrefixHash: coversHash,
		},
	}); err != nil {
		t.Fatalf("sidecar: %v", err)
	}
	exec := agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{ContextWindow: 1000, ArchiveDir: dir}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", Sink: sink})
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return c, loaded, path
}

// A fold saved beside a session that no longer matches the history behind it is
// not reused, and the person is told: the whole history goes out instead.
func TestResumeSaysWhenASavedFoldIsNotReused(t *testing.T) {
	var got []event.Event
	c, loaded, path := foldController(t, event.FuncSink(func(e event.Event) { got = append(got, e) }), "not-the-prefix-hash")
	defer c.Close()

	c.Resume(loaded, path)

	codes := foldNoticeCodes(got)
	if len(codes) != 1 || codes[0] != event.NoticeCodeFoldNotReusedLineage {
		t.Fatalf("fold notices = %v, want exactly the lineage one (the sidecar carries no lineage key)", codes)
	}
}

func TestResumeSaysNothingWhenTheFoldIsReused(t *testing.T) {
	var got []event.Event
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}, {Role: provider.RoleUser, Content: "task"}}
	c, loaded, path := foldController(t, event.FuncSink(func(e event.Event) { got = append(got, e) }), sessionstore.CoveredPrefixHash(msgs, 2))
	defer c.Close()

	c.Resume(loaded, path)

	if codes := foldNoticeCodes(got); len(codes) != 0 {
		t.Fatalf("a reusable fold produced notices %v", codes)
	}
}

func TestResumeWithoutASavedFoldSaysNothing(t *testing.T) {
	var got []event.Event
	c, path := coldNoticeController(t, event.FuncSink(func(e event.Event) { got = append(got, e) }))
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer c.Close()

	c.Resume(loaded, path)

	if codes := foldNoticeCodes(got); len(codes) != 0 {
		t.Fatalf("no fold on disk, yet notices %v", codes)
	}
}
