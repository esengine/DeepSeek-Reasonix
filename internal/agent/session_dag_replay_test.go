package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func dagTestSession(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "dag.jsonl")
}

func dagMsg(role provider.Role, content, id string) provider.Message {
	return provider.Message{Role: role, Content: content, ID: id}
}

func dagMessageEntry(t *testing.T, head, parent, turn string, m provider.Message, at time.Time) sessionDAGEntry {
	t.Helper()
	e, err := newSessionDAGMessageEntry(head, parent, "", turn, m, at)
	if err != nil {
		t.Fatalf("message entry: %v", err)
	}
	return e
}

func dagAppend(t *testing.T, sessionPath string, entries ...sessionDAGEntry) int64 {
	t.Helper()
	size, err := appendSessionDAGEntries(sessionPath, entries, false)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	return size
}

func dagReplay(t *testing.T, sessionPath string) *sessionDAGState {
	t.Helper()
	st, err := replaySessionDAG(context.Background(), store.SessionEventLog(sessionPath), defaultSessionReplayLimits)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	return st
}

func dagContents(msgs []provider.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

// dagLinearLog writes log header + system + user + assistant + user on main.
func dagLinearLog(t *testing.T, sessionPath string) (ids []string, base time.Time) {
	t.Helper()
	base = time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC)
	msgs := []provider.Message{
		dagMsg(provider.RoleSystem, "sys", "S0"),
		dagMsg(provider.RoleUser, "q1", "U1"),
		dagMsg(provider.RoleAssistant, "a1", "A1"),
		dagMsg(provider.RoleUser, "q2", "U2"),
	}
	entries := []sessionDAGEntry{{Type: sessionDAGTypeLog, Generation: 1, At: base}}
	parent := ""
	for i, m := range msgs {
		entries = append(entries, dagMessageEntry(t, SessionMainHead, parent, "t1", m, base.Add(time.Duration(i)*time.Second)))
		parent = m.ID
		ids = append(ids, m.ID)
	}
	dagAppend(t, sessionPath, entries...)
	return ids, base
}

func TestDAGReplayLinearChainMaterializes(t *testing.T) {
	path := dagTestSession(t)
	ids, base := dagLinearLog(t, path)
	st := dagReplay(t, path)
	if st.damaged || st.generation != 1 || st.records != 5 {
		t.Fatalf("state damaged=%v generation=%d records=%d", st.damaged, st.generation, st.records)
	}
	if got := st.selectedHead(); got != SessionMainHead {
		t.Fatalf("selectedHead = %q", got)
	}
	msgs, times := st.materialize(SessionMainHead)
	if got := dagContents(msgs); strings.Join(got, ",") != "sys,q1,a1,q2" {
		t.Fatalf("materialized %v", got)
	}
	for i, m := range msgs {
		if m.ID != ids[i] {
			t.Fatalf("message %d id %q, want %q", i, m.ID, ids[i])
		}
		if !times[i].Equal(base.Add(time.Duration(i) * time.Second)) {
			t.Fatalf("message %d time %v", i, times[i])
		}
	}
	if st.heads[SessionMainHead].leaf != "U2" {
		t.Fatalf("main leaf = %q", st.heads[SessionMainHead].leaf)
	}
}

func TestDAGForkRewindSelectRetireSemantics(t *testing.T) {
	path := dagTestSession(t)
	_, base := dagLinearLog(t, path)
	// Fork F from A1 and continue it; rewind main back to U1.
	dagAppend(t, path,
		sessionDAGEntry{Type: sessionDAGTypeFork, Head: SessionMainHead, NewHead: "F", From: "A1", Kind: HeadKindFork, Name: "alt", At: base.Add(10 * time.Second)},
		dagMessageEntry(t, "F", "A1", "t2", dagMsg(provider.RoleUser, "q2-alt", "U2b"), base.Add(11*time.Second)),
		sessionDAGEntry{Type: sessionDAGTypeRewind, Head: SessionMainHead, To: "U1", Cause: "test", At: base.Add(12 * time.Second)},
	)
	st := dagReplay(t, path)
	if got := dagChain(st, SessionMainHead); strings.Join(got, ",") != "sys,q1" {
		t.Fatalf("main after rewind %v", got)
	}
	if got := dagChain(st, "F"); strings.Join(got, ",") != "sys,q1,a1,q2-alt" {
		t.Fatalf("fork chain %v", got)
	}
	// Newest activity wins: the rewind marker on main is the latest entry.
	if got := st.selectedHead(); got != SessionMainHead {
		t.Fatalf("selectedHead = %q, want main (newest activity)", got)
	}
	dagAppend(t, path, sessionDAGEntry{Type: sessionDAGTypeSelect, Head: "F", At: base.Add(13 * time.Second)})
	if got := dagReplay(t, path).selectedHead(); got != "F" {
		t.Fatalf("selectedHead after select = %q", got)
	}
	dagAppend(t, path, sessionDAGEntry{Type: sessionDAGTypeRetire, Head: "F", At: base.Add(14 * time.Second)})
	st = dagReplay(t, path)
	if got := st.selectedHead(); got != SessionMainHead {
		t.Fatalf("selectedHead after retire = %q", got)
	}
	heads, err := ListSessionHeads(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 2 || heads[0].ID != SessionMainHead || heads[1].ID != "F" {
		t.Fatalf("heads = %+v", heads)
	}
	if !heads[1].Retired || heads[1].Name != "alt" || heads[1].ForkFrom != "A1" || heads[1].ParentHead != SessionMainHead || heads[1].MessageCount != 4 {
		t.Fatalf("fork head record = %+v", heads[1])
	}
	if !heads[0].Selected || heads[0].MessageCount != 2 || heads[0].Kind != HeadKindMain {
		t.Fatalf("main head record = %+v", heads[0])
	}
}

func TestDAGPatchSystemAndRedactOverlays(t *testing.T) {
	path := dagTestSession(t)
	_, base := dagLinearLog(t, path)
	sys, err := encodeSessionDAGMessage(dagMsg(provider.RoleSystem, "sys-v2", ""))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := encodeSessionDAGMessage(dagMsg(provider.RoleAssistant, "[redacted]", ""))
	if err != nil {
		t.Fatal(err)
	}
	patched, err := encodeSessionDAGMessage(provider.Message{Role: provider.RoleUser, Content: "q1", Edited: true, WorkDurationMs: 7})
	if err != nil {
		t.Fatal(err)
	}
	dagAppend(t, path,
		sessionDAGEntry{Type: sessionDAGTypePatch, Head: SessionMainHead, Target: "U1", Msgs: patched, At: base.Add(20 * time.Second)},
		sessionDAGEntry{Type: sessionDAGTypeSystem, Head: SessionMainHead, Msgs: sys, At: base.Add(21 * time.Second)},
		sessionDAGEntry{Type: sessionDAGTypeRedact, Head: SessionMainHead, Targets: map[string]json.RawMessage{"A1": replacement}, Reason: "secret", At: base.Add(22 * time.Second)},
	)
	st := dagReplay(t, path)
	msgs, _ := st.materialize(SessionMainHead)
	if msgs[0].Content != "sys-v2" || msgs[0].ID != "S0" {
		t.Fatalf("system override = %+v", msgs[0])
	}
	if !msgs[1].Edited || msgs[1].WorkDurationMs != 7 || msgs[1].Content != "q1" {
		t.Fatalf("patched message = %+v", msgs[1])
	}
	if msgs[2].Content != "[redacted]" || msgs[2].ID != "A1" {
		t.Fatalf("redacted message = %+v", msgs[2])
	}
	// The head's leaf and the chain are unaffected by overlays.
	if st.heads[SessionMainHead].leaf != "U2" || len(msgs) != 4 {
		t.Fatalf("leaf %q len %d", st.heads[SessionMainHead].leaf, len(msgs))
	}
}

func TestDAGTornTailStopsAtLastGoodEntryAndRepairsWhenQuiet(t *testing.T) {
	path := dagTestSession(t)
	dagLinearLog(t, path)
	logPath := store.SessionEventLog(path)
	good, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"schema_version":2,"type":"message","id":"X","head":"main","msgs":[{"role":"user","con`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	st := dagReplay(t, path)
	if !st.damaged || st.lastGoodEnd != good.Size()-1 || st.records != 5 {
		t.Fatalf("damaged=%v lastGoodEnd=%d good=%d records=%d", st.damaged, st.lastGoodEnd, good.Size(), st.records)
	}
	if got := dagChain(st, SessionMainHead); strings.Join(got, ",") != "sys,q1,a1,q2" {
		t.Fatalf("prefix %v", got)
	}
	// A young tail may still be another writer's in-progress append.
	info, _ := os.Stat(logPath)
	if repaired, err := repairSessionDAGTail(path, st, info.ModTime()); err != nil || repaired {
		t.Fatalf("young tail repaired=%v err=%v", repaired, err)
	}
	repaired, err := repairSessionDAGTail(path, st, info.ModTime().Add(sessionDAGTailRepairMinAge+time.Second))
	if err != nil || !repaired {
		t.Fatalf("quiet tail repaired=%v err=%v", repaired, err)
	}
	if _, err := os.Stat(store.SessionEventLogDamaged(path)); err != nil {
		t.Fatalf("damaged sidecar: %v", err)
	}
	after := dagReplay(t, path)
	if after.damaged || after.records != 5 {
		t.Fatalf("after repair damaged=%v records=%d", after.damaged, after.records)
	}
	b, _ := os.ReadFile(logPath)
	if !strings.HasSuffix(string(b), "\n") || strings.Contains(string(b), `"id":"X"`) {
		t.Fatalf("repaired log tail: %q", string(b[len(b)-40:]))
	}
}

func TestDAGDanglingParentBecomesOrphanRoot(t *testing.T) {
	path := dagTestSession(t)
	base := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	dagAppend(t, path,
		sessionDAGEntry{Type: sessionDAGTypeLog, Generation: 3, RotatedFrom: 2, At: base},
		dagMessageEntry(t, SessionMainHead, "GONE", "", dagMsg(provider.RoleUser, "orphan", "O1"), base),
		dagMessageEntry(t, SessionMainHead, "O1", "", dagMsg(provider.RoleAssistant, "child", "O2"), base.Add(time.Second)),
	)
	st := dagReplay(t, path)
	if st.damaged || len(st.orphans) != 1 || st.orphans[0] != "O1" {
		t.Fatalf("damaged=%v orphans=%v", st.damaged, st.orphans)
	}
	if got := dagChain(st, SessionMainHead); strings.Join(got, ",") != "orphan,child" {
		t.Fatalf("chain %v", got)
	}
}

func TestDAGUnknownTypeAndFutureSchemaAreHardErrors(t *testing.T) {
	path := dagTestSession(t)
	dagAppend(t, path, sessionDAGEntry{Type: sessionDAGTypeLog, Generation: 1})
	logPath := store.SessionEventLog(path)
	appendRaw := func(line string) {
		t.Helper()
		f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	appendRaw(`{"schema_version":2,"type":"teleport","head":"main","at":"2026-09-08T10:00:00Z"}`)
	if _, err := replaySessionDAG(context.Background(), logPath, defaultSessionReplayLimits); err == nil || !strings.Contains(err.Error(), `unsupported entry type "teleport"`) {
		t.Fatalf("unknown type err = %v", err)
	}
	if _, err := LoadSession(path); err == nil {
		t.Fatal("LoadSession must refuse a log with an unknown entry type")
	}
	if err := os.WriteFile(logPath, []byte(`{"schema_version":3,"type":"log","at":"2026-09-08T10:00:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := probeSessionEventLog(path)
	if err != nil || !probe.futureSchema || probe.dag || probe.native {
		t.Fatalf("schema 3 probe = %+v err=%v", probe, err)
	}
	if _, err := LoadSession(path); err == nil || !strings.Contains(err.Error(), "schema 3") {
		t.Fatalf("schema 3 load err = %v", err)
	}
}

func TestDAGReplayHonorsRecordBudget(t *testing.T) {
	path := dagTestSession(t)
	dagLinearLog(t, path)
	limits := defaultSessionReplayLimits
	limits.maxRecords = 3
	_, err := replaySessionDAG(context.Background(), store.SessionEventLog(path), limits)
	if !errors.Is(err, ErrSessionReplayLimitExceeded) {
		t.Fatalf("err = %v, want replay limit", err)
	}
}

func TestLoadSessionReadsSelectedHeadOfDAGLog(t *testing.T) {
	path := dagTestSession(t)
	ids, base := dagLinearLog(t, path)
	dagAppend(t, path,
		sessionDAGEntry{Type: sessionDAGTypeFork, Head: SessionMainHead, NewHead: "F", From: "A1", Kind: HeadKindConcurrent, At: base.Add(30 * time.Second)},
		dagMessageEntry(t, "F", "A1", "", dagMsg(provider.RoleUser, "from-other-writer", "U9"), base.Add(31*time.Second)),
	)
	probe, err := probeSessionEventLog(path)
	if err != nil || !probe.dag || probe.native || probe.futureSchema {
		t.Fatalf("probe = %+v err=%v", probe, err)
	}
	s, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := dagContents(s.Messages); strings.Join(got, ",") != "sys,q1,a1,from-other-writer" {
		t.Fatalf("loaded newest head %v", got)
	}
	ref, ok := s.Head()
	if !ok || ref.HeadID != "F" || ref.LeafID != "U9" || ref.LogGeneration != 1 || ref.LogOffset <= 0 {
		t.Fatalf("head ref = %+v ok=%v", ref, ok)
	}
	if s.Messages[1].ID != ids[1] || s.LeafID() != "U9" {
		t.Fatalf("ids not preserved: %q leaf %q", s.Messages[1].ID, s.LeafID())
	}
	users, err := LoadSessionUserMessages(path)
	if err != nil || len(users) != 2 || users[1].Message.Content != "from-other-writer" || !users[1].At.Equal(base.Add(31*time.Second)) {
		t.Fatalf("user messages = %+v err=%v", users, err)
	}
	// A save extends the loaded head in place: the log grows by the new entry,
	// the selected-head cache follows, and no transcript copy appears.
	before, _ := os.ReadFile(store.SessionEventLog(path))
	s.Add(dagMsg(provider.RoleAssistant, "reply", ""))
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	after, _ := os.ReadFile(store.SessionEventLog(path))
	if !strings.HasPrefix(string(after), string(before)) || len(after) == len(before) {
		t.Fatal("save must append to the schema-2 log without rewriting it")
	}
	if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), `"reply"`) {
		t.Fatalf("checkpoint not written: %v", err)
	}
	reloaded, err := LoadSession(path)
	if err != nil || reloaded.LeafID() != s.LeafID() || len(reloaded.Messages) != 5 {
		t.Fatalf("reload after save: err=%v leaf %q vs %q len %d", err, reloaded.LeafID(), s.LeafID(), len(reloaded.Messages))
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		if store.IsSessionTranscriptName(entry.Name()) && entry.Name() != filepath.Base(path) {
			t.Fatalf("save created a transcript copy: %s", entry.Name())
		}
	}
}

func TestSchemaOneReaderRefusesDAGLogWithoutTruncating(t *testing.T) {
	path := dagTestSession(t)
	dagLinearLog(t, path)
	logPath := store.SessionEventLog(path)
	before, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replaySessionEventLog(logPath); err == nil || !strings.Contains(err.Error(), "unsupported schema version 2") {
		t.Fatalf("schema-1 replay err = %v", err)
	}
	if err := repairSessionEventLogTail(path); err == nil {
		t.Fatal("schema-1 tail repair must fail closed on a schema-2 log")
	}
	after, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("schema-1 tail repair modified a schema-2 log")
	}
}

func dagChain(st *sessionDAGState, head string) []string {
	msgs, _ := st.materialize(head)
	return dagContents(msgs)
}

// useSchemaOneLog pins a test to the schema-1 writer: it exercises mechanics
// (replace records, revision CAS, recovery copies) that only that path has.
func useSchemaOneLog(t *testing.T) {
	t.Helper()
	t.Setenv(SessionLogSchemaEnv, "v1")
}

func schemaOneTempDir(t *testing.T) string {
	t.Helper()
	useSchemaOneLog(t)
	return t.TempDir()
}

func schemaOneSessionPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(schemaOneTempDir(t), name)
}
