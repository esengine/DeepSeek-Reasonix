package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func dagSavedSession(t *testing.T, path string, contents ...string) *Session {
	t.Helper()
	s := NewSession("sys")
	for i, c := range contents {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		s.Add(provider.Message{Role: role, Content: c})
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	return s
}

func dagEntryTypes(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(store.SessionEventLog(path))
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		_, rest, _ := strings.Cut(line, `"type":"`)
		typ, _, _ := strings.Cut(rest, `"`)
		types = append(types, typ)
	}
	return types
}

func assertNoTranscriptCopies(t *testing.T, path string) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		if store.IsSessionTranscriptName(entry.Name()) && entry.Name() != filepath.Base(path) {
			t.Fatalf("unexpected transcript copy %s", entry.Name())
		}
	}
}

func TestDAGSaveCreatesSchemaTwoLogAndAppendsDelta(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	probe, err := probeSessionEventLog(path)
	if err != nil || !probe.dag {
		t.Fatalf("probe = %+v err=%v", probe, err)
	}
	if got := dagEntryTypes(t, path); strings.Join(got, ",") != "log,writer,message,message,message" {
		t.Fatalf("entries = %v", got)
	}
	ref, ok := s.Head()
	if !ok || ref.HeadID != SessionMainHead || ref.LeafID != s.LeafID() || ref.LogGeneration != 1 {
		t.Fatalf("head = %+v ok=%v", ref, ok)
	}
	if b, err := os.ReadFile(path); err != nil || strings.Count(string(b), "\n") != 3 {
		t.Fatalf("checkpoint cache: %v %q", err, b)
	}
	idx, err := ReadSessionHeadIndex(path)
	if err != nil || idx == nil || !idx.Current(path) || idx.MessageCount != 3 || idx.SelectedHead != SessionMainHead {
		t.Fatalf("index = %+v err=%v", idx, err)
	}
	meta, _, err := LoadBranchMeta(path)
	if err != nil || meta.HeadID != SessionMainHead || meta.LogSchema != 2 || meta.HeadCount != 1 || meta.Revision == 0 {
		t.Fatalf("meta = %+v err=%v", meta, err)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "q2"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := dagEntryTypes(t, path); strings.Join(got, ",") != "log,writer,message,message,message,message" {
		t.Fatalf("entries after append = %v", got)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := len(dagEntryTypes(t, path)); got != 6 {
		t.Fatalf("no-op save appended: %d entries", got)
	}
	loaded, err := LoadSession(path)
	if err != nil || len(loaded.Messages) != 4 || loaded.LeafID() != s.LeafID() {
		t.Fatalf("reload: err=%v len=%d", err, len(loaded.Messages))
	}
	assertNoTranscriptCopies(t, path)
}

func TestDAGSaveDisabledByEnvKeepsSchemaOne(t *testing.T) {
	useSchemaOneLog(t)
	path := dagTestSession(t)
	dagSavedSession(t, path, "q1")
	probe, err := probeSessionEventLog(path)
	if err != nil || probe.dag || !probe.native {
		t.Fatalf("probe = %+v err=%v", probe, err)
	}
}

func TestDAGSaveLocalMetadataBecomesPatch(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	msgs := s.Snapshot()
	msgs[1].Edited = true
	msgs[1].WorkDurationMs = 42
	s.ReplaceLocalMetadata(msgs)
	if err := s.SaveRewrite(path); err != nil {
		t.Fatal(err)
	}
	types := dagEntryTypes(t, path)
	if types[len(types)-1] != sessionDAGTypePatch {
		t.Fatalf("entries = %v", types)
	}
	loaded, err := LoadSession(path)
	if err != nil || !loaded.Messages[1].Edited || loaded.Messages[1].WorkDurationMs != 42 || loaded.Messages[1].ID != msgs[1].ID {
		t.Fatalf("reload = %+v err=%v", loaded.Messages[1], err)
	}
	if reasons := s.DrainContentRewriteReasons(); len(reasons) != 0 {
		t.Fatalf("local metadata save queued cache reasons %v", reasons)
	}
}

func TestDAGSaveSystemPromptRefreshKeepsLaterIDs(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	before := s.Snapshot()
	s.SetLeadingSystemPrompt("sys-v2")
	if err := s.SaveRewrite(path); err != nil {
		t.Fatal(err)
	}
	types := dagEntryTypes(t, path)
	if types[len(types)-1] != sessionDAGTypeSystem {
		t.Fatalf("entries = %v", types)
	}
	loaded, err := LoadSession(path)
	if err != nil || loaded.Messages[0].Content != "sys-v2" {
		t.Fatalf("reload = %+v err=%v", loaded.Messages, err)
	}
	for i := range before {
		if loaded.Messages[i].ID != before[i].ID {
			t.Fatalf("message %d id changed across system refresh", i)
		}
	}
}

func TestDAGSaveTruncationRewindsWithoutErasingBytes(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1", "q2", "a2")
	logBefore, _ := os.ReadFile(store.SessionEventLog(path))
	msgs := s.Snapshot()
	s.Rewrite(msgs[:3], "rewind_truncate")
	if err := s.SaveRewrite(path); err != nil {
		t.Fatal(err)
	}
	logAfter, _ := os.ReadFile(store.SessionEventLog(path))
	if !strings.HasPrefix(string(logAfter), string(logBefore)) {
		t.Fatal("rewind must not rewrite earlier bytes")
	}
	types := dagEntryTypes(t, path)
	if types[len(types)-1] != sessionDAGTypeRewind {
		t.Fatalf("entries = %v", types)
	}
	loaded, err := LoadSession(path)
	if err != nil || len(loaded.Messages) != 3 || loaded.LeafID() != msgs[2].ID {
		t.Fatalf("reload len=%d leaf=%q err=%v", len(loaded.Messages), loaded.LeafID(), err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a2-new"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadSession(path)
	if err != nil || strings.Join(dagContents(loaded.Messages), ",") != "sys,q1,a1,a2-new" {
		t.Fatalf("after re-append: %v err=%v", dagContents(loaded.Messages), err)
	}
}

func TestDAGSaveUpgradesSchemaOneLogOnlyUnderLease(t *testing.T) {
	t.Setenv(SessionLogSchemaEnv, "v1")
	path := dagTestSession(t)
	v1 := dagSavedSession(t, path, "q1", "a1")
	if err := os.Unsetenv(SessionLogSchemaEnv); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "q2"})
	if err := loaded.Save(path); err != nil {
		t.Fatal(err)
	}
	if probe, _ := probeSessionEventLog(path); probe.dag {
		t.Fatal("unleased writer must not upgrade an existing schema-1 log")
	}
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "a2"})
	if err := loaded.Save(path); err != nil {
		t.Fatal(err)
	}
	probe, _ := probeSessionEventLog(path)
	if !probe.dag {
		t.Fatal("lease holder must upgrade the schema-1 log on save")
	}
	again, err := LoadSession(path)
	if err != nil || strings.Join(dagContents(again.Messages), ",") != "sys,q1,a1,q2,a2" {
		t.Fatalf("after upgrade: %v err=%v", dagContents(again.Messages), err)
	}
	for i := range v1.Messages {
		if again.Messages[i].ID != loaded.Messages[i].ID {
			t.Fatalf("message %d id changed across upgrade", i)
		}
	}
	if ref, ok := again.Head(); !ok || ref.HeadID != SessionMainHead {
		t.Fatalf("head after upgrade = %+v ok=%v", ref, ok)
	}
	if st := dagReplay(t, path); st.upgradedFrom != sessionEventSchemaVersion {
		t.Fatalf("upgradedFrom = %d", st.upgradedFrom)
	}
}

func TestDAGSaveConcurrentWritersForkInsteadOfConflicting(t *testing.T) {
	path := dagTestSession(t)
	a := dagSavedSession(t, path, "q1", "a1")
	b, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Add(provider.Message{Role: provider.RoleUser, Content: "q2-from-a"})
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	b.Add(provider.Message{Role: provider.RoleUser, Content: "q2-from-b"})
	if err := b.Save(path); err != nil {
		t.Fatalf("second writer must not conflict: %v", err)
	}
	refA, _ := a.Head()
	refB, _ := b.Head()
	if refA.HeadID != SessionMainHead || refB.HeadID == SessionMainHead || refB.HeadID == "" {
		t.Fatalf("heads a=%+v b=%+v", refA, refB)
	}
	events := b.DrainHeadEvents()
	if len(events) != 1 || events[0].Kind != HeadEventForkedConcurrent || events[0].HeadID != refB.HeadID {
		t.Fatalf("events = %+v", events)
	}
	heads, err := ListSessionHeads(path)
	if err != nil || len(heads) != 2 || heads[1].Kind != HeadKindConcurrent || heads[1].MessageCount != 4 || heads[0].MessageCount != 4 {
		t.Fatalf("heads = %+v err=%v", heads, err)
	}
	st := dagReplay(t, path)
	if got := dagChain(st, SessionMainHead); strings.Join(got, ",") != "sys,q1,a1,q2-from-a" {
		t.Fatalf("main chain %v", got)
	}
	if got := dagChain(st, refB.HeadID); strings.Join(got, ",") != "sys,q1,a1,q2-from-b" {
		t.Fatalf("fork chain %v", got)
	}
	assertNoTranscriptCopies(t, path)
	// Each writer keeps extending its own head afterwards.
	a.Add(provider.Message{Role: provider.RoleAssistant, Content: "a2-from-a"})
	b.Add(provider.Message{Role: provider.RoleAssistant, Content: "a2-from-b"})
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	if len(b.DrainHeadEvents()) != 0 {
		t.Fatal("continuing on the fork must not fork again")
	}
	st = dagReplay(t, path)
	if len(st.heads) != 2 || len(dagChain(st, SessionMainHead)) != 5 || len(dagChain(st, refB.HeadID)) != 5 {
		t.Fatalf("heads=%d main=%d fork=%d", len(st.heads), len(dagChain(st, SessionMainHead)), len(dagChain(st, refB.HeadID)))
	}
}

func TestDAGSaveBehindDiskReportsStalePrefix(t *testing.T) {
	path := dagTestSession(t)
	a := dagSavedSession(t, path, "q1", "a1")
	b, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Add(provider.Message{Role: provider.RoleUser, Content: "q2"})
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	err = b.Save(path)
	if !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("behind writer err = %v, want stale prefix conflict", err)
	}
	if kind, ok := SnapshotConflictKind(err); !ok || kind != SessionSnapshotConflictStalePrefix {
		t.Fatalf("kind = %q ok=%v", kind, ok)
	}
	if st := dagReplay(t, path); len(st.heads) != 1 || len(dagChain(st, SessionMainHead)) != 4 {
		t.Fatal("a behind writer must not append or fork")
	}
	assertNoTranscriptCopies(t, path)
}

func TestDAGSaveRedactionCompactErasesBytesUnderLease(t *testing.T) {
	path := dagTestSession(t)
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	s := dagSavedSession(t, path, "q1 secret-token", "a1")
	msgs := s.Snapshot()
	ids := []string{msgs[0].ID, msgs[1].ID, msgs[2].ID}
	msgs[1].Content = "q1 [redacted]"
	s.Rewrite(msgs, "redact")
	if err := s.SaveRewriteCompact(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(store.SessionEventLog(path))
	if strings.Contains(string(raw), "secret-token") {
		t.Fatal("redaction left the secret in the log")
	}
	st := dagReplay(t, path)
	if st.generation != 2 {
		t.Fatalf("generation = %d, want rotation", st.generation)
	}
	loaded, err := LoadSession(path)
	if err != nil || loaded.Messages[1].Content != "q1 [redacted]" {
		t.Fatalf("reload = %+v err=%v", loaded.Messages, err)
	}
	for i, id := range ids {
		if loaded.Messages[i].ID != id {
			t.Fatalf("message %d id changed by redaction", i)
		}
	}
	if ref, _ := s.Head(); ref.LogGeneration != 2 {
		t.Fatalf("session did not follow the rotation: %+v", ref)
	}
}

func TestDAGSaveOversizeLogRotatesUnderLease(t *testing.T) {
	path := dagTestSession(t)
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	big := strings.Repeat("x", 100<<10)
	s := dagSavedSession(t, path, big+"1", big+"2", big+"3", big+"4", big+"5", big+"6")
	msgs := s.Snapshot()
	s.Rewrite(msgs[:2], "rewind_truncate")
	if err := s.SaveRewrite(path); err != nil {
		t.Fatal(err)
	}
	st := dagReplay(t, path)
	if st.generation != 2 || len(st.nodes) != 2 {
		t.Fatalf("generation=%d nodes=%d, want rotated log with only the live chain", st.generation, len(st.nodes))
	}
	if info, _ := os.Stat(store.SessionEventLog(path)); info.Size() > int64(len(big))*3 {
		t.Fatalf("rotated log still %d bytes", info.Size())
	}
	loaded, err := LoadSession(path)
	if err != nil || len(loaded.Messages) != 2 {
		t.Fatalf("reload len=%d err=%v", len(loaded.Messages), err)
	}
}

func TestDAGSaveCrashAtAppendRecoversOnNextSave(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1")
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a1"})
	fileutil.CrashPoint = func(op, _ string) {
		if op == "dag-append" {
			panic("crash:dag-append")
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("crash point did not fire")
			}
		}()
		_ = s.Save(path)
	}()
	fileutil.CrashPoint = nil
	// The crash happened inside the locked save; a later save from the same
	// session must still land exactly one copy of the message.
	if err := s.Save(path); err != nil {
		t.Fatalf("save after crash: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil || strings.Join(dagContents(loaded.Messages), ",") != "sys,q1,a1" {
		t.Fatalf("after crash: %v err=%v", dagContents(loaded.Messages), err)
	}
}

func TestDAGSaveConcurrentGoroutinesExtendTheirOwnHeads(t *testing.T) {
	path := dagTestSession(t)
	a := dagSavedSession(t, path, "q1", "a1")
	b, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	const rounds = 15
	var wg sync.WaitGroup
	run := func(s *Session, tag string) {
		defer wg.Done()
		for i := range rounds {
			s.Add(provider.Message{Role: provider.RoleUser, Content: tag + string(rune('a'+i))})
			if err := s.Save(path); err != nil {
				t.Errorf("%s save %d: %v", tag, i, err)
				return
			}
		}
	}
	wg.Add(2)
	go run(a, "A")
	go run(b, "B")
	wg.Wait()
	st := dagReplay(t, path)
	if st.damaged || len(st.heads) != 2 {
		t.Fatalf("damaged=%v heads=%d", st.damaged, len(st.heads))
	}
	for _, id := range st.headOrder {
		if got := len(dagChain(st, id)); got != 3+rounds {
			t.Fatalf("head %s chain length %d", id, got)
		}
	}
	assertNoTranscriptCopies(t, path)
}

func TestExportSessionSchemaOneWritesReadableSchemaOneSession(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	dst := filepath.Join(t.TempDir(), "export.jsonl")
	if err := ExportSessionSchemaOne(path, dst); err != nil {
		t.Fatal(err)
	}
	probe, err := probeSessionEventLog(dst)
	if err != nil || !probe.native || probe.dag {
		t.Fatalf("export probe = %+v err=%v", probe, err)
	}
	exported, err := LoadSession(dst)
	if err != nil || strings.Join(dagContents(exported.Messages), ",") != strings.Join(dagContents(s.Messages), ",") {
		t.Fatalf("export reload = %v err=%v", dagContents(exported.Messages), err)
	}
	if _, ok := exported.Head(); ok {
		t.Fatal("exported session must be schema 1")
	}
	if err := ExportSessionSchemaOne(path, dst); err == nil {
		t.Fatal("export must refuse to overwrite an existing destination")
	}
}

func TestDAGSaveIndependentIdenticalTranscriptsConverge(t *testing.T) {
	path := dagTestSession(t)
	a := dagSavedSession(t, path, "q1", "a1")
	b := NewSession("sys")
	b.Add(provider.Message{Role: provider.RoleUser, Content: "q1"})
	b.Add(provider.Message{Role: provider.RoleAssistant, Content: "a1"})
	b.Add(provider.Message{Role: provider.RoleUser, Content: "q2"})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	st := dagReplay(t, path)
	if len(st.heads) != 1 || strings.Join(dagChain(st, SessionMainHead), ",") != "sys,q1,a1,q2" {
		t.Fatalf("identical prefix must extend main: heads=%d chain=%v", len(st.heads), dagChain(st, SessionMainHead))
	}
	for i := range a.Messages {
		if b.Messages[i].ID != a.Messages[i].ID {
			t.Fatalf("message %d: independent writer did not adopt the persisted id", i)
		}
	}
	if ref, _ := b.Head(); ref.HeadID != SessionMainHead || ref.LeafID != b.LeafID() {
		t.Fatalf("b head = %+v", ref)
	}
}

func TestDAGSaveUnrelatedWriterForksInsteadOfRewinding(t *testing.T) {
	path := dagTestSession(t)
	dagSavedSession(t, path, "q1", "a1", "q2", "a2")
	b := NewSession("sys")
	b.Add(provider.Message{Role: provider.RoleUser, Content: "q1"})
	b.Add(provider.Message{Role: provider.RoleAssistant, Content: "a1"})
	b.Add(provider.Message{Role: provider.RoleUser, Content: "q2-other"})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	st := dagReplay(t, path)
	ref, _ := b.Head()
	if len(st.heads) != 2 || ref.HeadID == SessionMainHead {
		t.Fatalf("unrelated writer must fork: heads=%d ref=%+v", len(st.heads), ref)
	}
	if got := dagChain(st, SessionMainHead); strings.Join(got, ",") != "sys,q1,a1,q2,a2" {
		t.Fatalf("main was rewritten by an unrelated writer: %v", got)
	}
	if got := dagChain(st, ref.HeadID); strings.Join(got, ",") != "sys,q1,a1,q2-other" {
		t.Fatalf("fork chain %v", got)
	}
}
