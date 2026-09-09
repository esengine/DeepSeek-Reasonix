package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestDAGConcurrentAppendersKeepBothHeads(t *testing.T) {
	path := dagTestSession(t)
	_, base := dagLinearLog(t, path)
	dagAppend(t, path, sessionDAGEntry{Type: sessionDAGTypeFork, Head: SessionMainHead, NewHead: "B", From: "U2", Kind: HeadKindConcurrent, Writer: "w-b", At: base.Add(time.Minute)})
	const perWriter = 25
	var wg sync.WaitGroup
	run := func(head, writer, parent string) {
		defer wg.Done()
		for i := range perWriter {
			id := head + "-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
			e := dagMessageEntry(t, head, parent, "", dagMsg(provider.RoleUser, head, id), base.Add(time.Duration(i)*time.Millisecond))
			e.Writer = writer
			unlock, err := lockSessionFile(path)
			if err != nil {
				t.Errorf("lock: %v", err)
				return
			}
			_, err = appendSessionDAGEntries(path, []sessionDAGEntry{e}, false)
			unlock()
			if err != nil {
				t.Errorf("append: %v", err)
				return
			}
			parent = id
		}
	}
	wg.Add(2)
	go run(SessionMainHead, "w-a", "U2")
	go run("B", "w-b", "U2")
	wg.Wait()
	st := dagReplay(t, path)
	if st.damaged {
		t.Fatal("interleaved appends damaged the log")
	}
	if got := len(dagChain(st, SessionMainHead)); got != 4+perWriter {
		t.Fatalf("main chain length %d", got)
	}
	if got := len(dagChain(st, "B")); got != 4+perWriter {
		t.Fatalf("B chain length %d", got)
	}
	if st.writers["w-a"] == nil || st.writers["w-b"] == nil {
		t.Fatalf("writers = %v", st.writers)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		if store.IsSessionTranscriptName(entry.Name()) {
			t.Fatalf("concurrent writers created a transcript copy: %s", entry.Name())
		}
	}
}

func TestDAGIncrementalReplayFromKnownTail(t *testing.T) {
	path := dagTestSession(t)
	_, base := dagLinearLog(t, path)
	st := dagReplay(t, path)
	tail := st.lastGoodEnd
	dagAppend(t, path, dagMessageEntry(t, SessionMainHead, "U2", "", dagMsg(provider.RoleAssistant, "a2", "A2"), base.Add(time.Hour)))
	if err := st.replayFrom(context.Background(), tail, defaultSessionReplayLimits); err != nil {
		t.Fatal(err)
	}
	if st.heads[SessionMainHead].leaf != "A2" || st.records != 6 || st.lastGoodEnd <= tail {
		t.Fatalf("leaf %q records %d tail %d>%d", st.heads[SessionMainHead].leaf, st.records, st.lastGoodEnd, tail)
	}
}

func TestDAGUpgradeFromSchemaOneKeepsIDsAndTranscript(t *testing.T) {
	useSchemaOneLog(t)
	path := dagTestSession(t)
	v1 := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "q1", CreatedAt: 1},
		{Role: provider.RoleAssistant, Content: "a1"},
	}}
	if err := v1.Save(path); err != nil {
		t.Fatalf("v1 save: %v", err)
	}
	checkpointBefore, _ := os.ReadFile(path)
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Head(); ok {
		t.Fatal("schema-1 session must not report a head")
	}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	inFlight := &InFlightTurnMeta{ID: "turn-7", StartMessageIndex: 3, PreserveUser: true, StartedAt: now.Add(-time.Minute)}
	if err := upgradeSessionLogToDAG(path, loaded.Messages, nil, inFlight, now); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	probe, err := probeSessionEventLog(path)
	if err != nil || !probe.dag {
		t.Fatalf("probe after upgrade = %+v err=%v", probe, err)
	}
	st := dagReplay(t, path)
	if st.generation != 1 || st.upgradedFrom != sessionEventSchemaVersion {
		t.Fatalf("header generation=%d upgradedFrom=%d", st.generation, st.upgradedFrom)
	}
	if turn := st.heads[SessionMainHead].openTurn; turn == nil || turn.turn != "turn-7" || turn.leaf != loaded.Messages[2].ID || !turn.preserveUser {
		t.Fatalf("open turn = %+v", turn)
	}
	again, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Messages) != len(loaded.Messages) {
		t.Fatalf("len %d vs %d", len(again.Messages), len(loaded.Messages))
	}
	for i := range loaded.Messages {
		if again.Messages[i].ID != loaded.Messages[i].ID || again.Messages[i].Content != loaded.Messages[i].Content {
			t.Fatalf("message %d changed across upgrade: %+v vs %+v", i, again.Messages[i], loaded.Messages[i])
		}
	}
	if ref, ok := again.Head(); !ok || ref.HeadID != SessionMainHead || ref.LeafID != loaded.Messages[2].ID {
		t.Fatalf("head after upgrade = %+v ok=%v", ref, ok)
	}
	if b, _ := os.ReadFile(path); string(b) != string(checkpointBefore) {
		t.Fatal("upgrade must not touch the .jsonl checkpoint")
	}
	if err := writeSessionDAGIndex(context.Background(), path, st); err != nil {
		t.Fatal(err)
	}
	idx, err := ReadSessionHeadIndex(path)
	if err != nil || idx == nil || !idx.Current(path) || idx.SelectedHead != SessionMainHead || idx.MessageCount != 3 || len(idx.Heads) != 1 {
		t.Fatalf("index = %+v err=%v", idx, err)
	}
	if _, err := readSessionEventIndex(path); err == nil {
		t.Fatal("schema-1 index reader must reject the schema-2 index")
	}
	dagAppend(t, path, dagMessageEntry(t, SessionMainHead, loaded.Messages[2].ID, "", dagMsg(provider.RoleUser, "q2", "N1"), now))
	if idx.Current(path) {
		t.Fatal("index must go stale once the log grows")
	}
}

func TestDAGRotationDropsUnreachableAndAppliesRedactions(t *testing.T) {
	path := dagTestSession(t)
	_, base := dagLinearLog(t, path)
	replacement, err := encodeSessionDAGMessage(dagMsg(provider.RoleAssistant, "[gone]", ""))
	if err != nil {
		t.Fatal(err)
	}
	patchedU2, err := encodeSessionDAGMessage(provider.Message{Role: provider.RoleUser, Content: "q2", Edited: true})
	if err != nil {
		t.Fatal(err)
	}
	dagAppend(t, path,
		sessionDAGEntry{Type: sessionDAGTypeFork, Head: SessionMainHead, NewHead: "F", From: "U1", Kind: HeadKindFork, Name: "side", At: base.Add(time.Minute)},
		dagMessageEntry(t, "F", "U1", "", dagMsg(provider.RoleAssistant, "side-answer-secret", "F1"), base.Add(2*time.Minute)),
		sessionDAGEntry{Type: sessionDAGTypeRetire, Head: "F", At: base.Add(3 * time.Minute)},
		sessionDAGEntry{Type: sessionDAGTypeRedact, Head: SessionMainHead, Targets: map[string]json.RawMessage{"A1": replacement}, At: base.Add(4 * time.Minute)},
		sessionDAGEntry{Type: sessionDAGTypePatch, Head: SessionMainHead, Target: "U2", Msgs: patchedU2, At: base.Add(5 * time.Minute)},
		sessionDAGEntry{Type: sessionDAGTypeRename, Head: SessionMainHead, Name: "primary", At: base.Add(6 * time.Minute)},
		sessionDAGEntry{Type: sessionDAGTypeCompaction, Head: SessionMainHead, CoveredLeaf: "A1", CoveredCount: 3, PrefixHash: "h", At: base.Add(7 * time.Minute)},
		sessionDAGEntry{Type: sessionDAGTypeSelect, Head: SessionMainHead, At: base.Add(8 * time.Minute)},
	)
	st := dagReplay(t, path)
	if err := rotateSessionDAG(path, st, base.Add(time.Hour)); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	raw, _ := os.ReadFile(store.SessionEventLog(path))
	if strings.Contains(string(raw), "side-answer-secret") || strings.Contains(string(raw), `"a1"`) {
		t.Fatalf("rotated log still carries dropped or redacted bytes:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"dropped":["F1"]`) || !strings.Contains(string(raw), `"tombstones":["A1"]`) {
		t.Fatalf("checkpoint manifest missing:\n%s", raw)
	}
	after := dagReplay(t, path)
	if after.generation != 2 || after.damaged || len(after.nodes) != 4 || len(after.heads) != 1 {
		t.Fatalf("after rotation generation=%d damaged=%v nodes=%d heads=%d", after.generation, after.damaged, len(after.nodes), len(after.heads))
	}
	msgs, _ := after.materialize(SessionMainHead)
	if got := dagContents(msgs); strings.Join(got, ",") != "sys,q1,[gone],q2" {
		t.Fatalf("rotated chain %v", got)
	}
	if !msgs[3].Edited || msgs[2].ID != "A1" || msgs[3].ID != "U2" {
		t.Fatalf("patch fold or ids lost: %+v", msgs)
	}
	main := after.heads[SessionMainHead]
	if main.name != "primary" || main.compaction == nil || main.compaction.coveredLeaf != "A1" || after.selected != SessionMainHead {
		t.Fatalf("main head metadata lost: name=%q compaction=%+v selected=%q", main.name, main.compaction, after.selected)
	}
	if len(after.patches) != 0 || len(after.redactions) != 0 {
		t.Fatal("rotation must fold overlays physically")
	}
}

func TestDAGSingleWriterProof(t *testing.T) {
	path := dagTestSession(t)
	_, base := dagLinearLog(t, path)
	st := dagReplay(t, path)
	now := time.Now().UTC()
	var denied *SessionRotationDeniedError
	if err := sessionDAGSingleWriterProof(path, st, now); !errors.As(err, &denied) || !strings.Contains(denied.Reason, "lease") {
		t.Fatalf("without lease err = %v", err)
	}
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	defer lease.Release()
	if err := sessionDAGSingleWriterProof(path, st, now); err != nil {
		t.Fatalf("with lease err = %v", err)
	}
	recent := dagMessageEntry(t, SessionMainHead, "U2", "", dagMsg(provider.RoleAssistant, "other", "X1"), now.Add(-10*time.Second))
	recent.Writer = "other-writer"
	dagAppend(t, path, recent)
	st = dagReplay(t, path)
	if err := sessionDAGSingleWriterProof(path, st, now); !errors.As(err, &denied) || !strings.Contains(denied.Reason, "other-writer") {
		t.Fatalf("recent foreign writer err = %v", err)
	}
	if err := sessionDAGSingleWriterProof(path, st, now.Add(sessionDAGWriterQuietPeriod+time.Second)); err != nil {
		t.Fatalf("quiet foreign writer err = %v", err)
	}
	_ = base
}

func TestDAGLogOversizedUsesLiveChains(t *testing.T) {
	path := dagTestSession(t)
	dagLinearLog(t, path)
	st := dagReplay(t, path)
	if sessionDAGLogOversized(st) {
		t.Fatal("small log must not be oversized")
	}
	st.size = sessionEventLogCompactFloor*2 + 1
	if !sessionDAGLogOversized(st) {
		t.Fatal("log far beyond its live chains must be oversized")
	}
}

func TestDAGCrashPointsLeaveLogUntouched(t *testing.T) {
	path := dagTestSession(t)
	_, base := dagLinearLog(t, path)
	logPath := store.SessionEventLog(path)
	before, _ := os.ReadFile(logPath)
	for _, op := range []string{"dag-append", "dag-rotate"} {
		fileutil.CrashPoint = func(got, _ string) {
			if got == op {
				panic("crash:" + op)
			}
		}
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("%s: crash point did not fire", op)
				}
			}()
			switch op {
			case "dag-append":
				dagAppend(t, path, dagMessageEntry(t, SessionMainHead, "U2", "", dagMsg(provider.RoleUser, "lost", "L1"), base))
			case "dag-rotate":
				_ = rotateSessionDAG(path, dagReplay(t, path), base)
			}
		}()
		fileutil.CrashPoint = nil
		after, _ := os.ReadFile(logPath)
		if string(after) != string(before) {
			t.Fatalf("%s: log changed despite crash", op)
		}
		if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp")); len(leftovers) != 0 {
			t.Fatalf("%s: temp files left behind: %v", op, leftovers)
		}
	}
}
