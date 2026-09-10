package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/readcoord"
	"reasonix/internal/tool"
)

func TestPartialRangeFinalAndFullFinalHaveDifferentContracts(t *testing.T) {
	for _, intent := range []tool.ReadIntent{tool.ReadIntentInspect, tool.ReadIntentRange, tool.ReadIntentFull} {
		t.Run(string(intent), func(t *testing.T) {
			a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard)
			a.turn.readShadow = newReadShadowState(true)
			a.reads.tasks = newReadTasks("test", 1)
			env := tool.ReadResultEnvelope{ReadID: "read", Source: tool.ReadResultSource{CanonicalPath: "/w/file", Identity: "version", Snapshot: "snapshot"}, Intent: intent, RequestedRange: &tool.ReadRange{Start: 0, End: 10}, DeliveredRanges: []tool.ReadRange{{Start: 0, End: 1}}, HasMore: true}
			tr, _ := a.turn.readShadow.coord.Observe(env, 1)
			a.issueReadContinuation(tr, env)
			instruction, err := a.readContinuation(true)
			if err != nil {
				t.Fatal(err)
			}
			if (instruction != "") != (intent == tool.ReadIntentFull) {
				t.Fatalf("intent=%s instruction=%s", intent, instruction)
			}
			if intent != tool.ReadIntentFull {
				a.finishReadRun(nil)
				found := false
				for _, m := range a.Session().Snapshot() {
					if m.ReadCompletion != nil {
						found = true
						if !m.LocalOnly || m.ReadCompletion.Reads[0].Verdict != "partial_read_sufficient" {
							t.Fatal("invalid receipt")
						}
					}
				}
				if !found {
					t.Fatal("missing terminal coverage receipt")
				}
			}
		})
	}
}

func TestLegacyPartialReadCanEditItsVisibleRange(t *testing.T) {
	path := makeIncompleteReadFixture(t, "legacy.txt", 430, 96, 362, incompleteReadKeyRule)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(string(data), "\r\n", 2)[0]
	args, _ := json.Marshal(map[string]any{"path": path, "old_string": first, "new_string": "changed visible line"})
	p := &scriptedProvider{turns: [][]provider.Chunk{
		{toolCallChunk("read", "read_file", fmt.Sprintf(`{"path":%q}`, path)), {Type: provider.ChunkDone}},
		{toolCallChunk("edit", "edit_file", string(args)), {Type: provider.ChunkDone}}, textTurn("Updated the local range."),
	}}
	writer, _ := tool.LookupBuiltin("edit_file")
	a := newLegacyIncompleteReadTestAgent(p, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, writer)
	if err := a.Run(withNoClosedLoop(context.Background()), "Update the first line."); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(got), "changed visible line") {
		t.Fatalf("local edit failed: %v %s", err, toolResultByID(a.Session(), "edit"))
	}
}

func TestReadHardStopCannotBeHiddenBehindAnotherContinuation(t *testing.T) {
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard)
	a.turn.readShadow = newReadShadowState(true)
	a.reads.tasks = newReadTasks("test", 1)
	for _, key := range []string{"a-first", "z-stopped"} {
		env := tool.ReadResultEnvelope{ReadID: key, Source: tool.ReadResultSource{CanonicalPath: "/w/" + key, Identity: key, Snapshot: key}, Intent: tool.ReadIntentFull, DeliveredRanges: []tool.ReadRange{{Start: 0, End: 1}}, HasMore: true}
		tr, _ := a.turn.readShadow.coord.Observe(env, 1)
		a.issueReadContinuation(tr, env)
	}
	a.turn.readShadow.coord.Fail("z-stopped", readcoord.Block{Code: tool.ReadHardStop, Detail: "budget", Recovery: "inspect a range"})
	instruction, err := a.readContinuation(true)
	var pause *IncompleteReadError
	if instruction != "" || !errors.As(err, &pause) {
		t.Fatalf("stop hidden: %q %v", instruction, err)
	}
}

func TestDefaultBudgetStopOffersValidatedStrategy(t *testing.T) {
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard)
	a.turn.readShadow = newReadShadowState(true)
	a.reads.tasks = newReadTasks("test", 1)
	env := tool.ReadResultEnvelope{ReadID: "r", Source: tool.ReadResultSource{CanonicalPath: "/w/a", Identity: "id", Snapshot: "s"}, Intent: tool.ReadIntentFull, HasMore: true}
	a.turn.readShadow.coord.Begin("r", readcoord.Scope{CanonicalPath: "/w/a"}, readcoord.Requirement{Intent: tool.ReadIntentFull, WholeFile: true})
	a.armDefaultReadStrategy("r", env)
	a.turn.readShadow.coord.Narrow("r", readcoord.Block{Code: "no_headroom", Detail: "budget", Recovery: "read a narrower window"})
	instruction, err := a.readContinuation(false)
	if err != nil || !strings.Contains(instruction, "READ STRATEGY") || !strings.Contains(instruction, "grep") {
		t.Fatalf("instruction=%q err=%v", instruction, err)
	}
}

func TestDefaultStrategyBindsPathAndReceiptID(t *testing.T) {
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard)
	a.turn.readShadow = newReadShadowState(true)
	a.reads.tasks = newReadTasks("test", 1)
	for _, id := range []string{"r-a", "r-b"} {
		path := "/w/" + id
		a.armDefaultReadStrategy(id, tool.ReadResultEnvelope{ReadID: id, Source: tool.ReadResultSource{CanonicalPath: path, Identity: id, Snapshot: id}, Intent: tool.ReadIntentFull})
	}
	grep := &toolCallPlan{evidenceName: "grep", execArgs: json.RawMessage(`{"path":"/w/r-b","pattern":"x"}`)}
	if _, blocked := a.gateReadOperation(context.Background(), grep); blocked || grep.incompleteReadRoot != "r-b" {
		t.Fatalf("grep root=%q blocked=%v", grep.incompleteReadRoot, blocked)
	}
	receipt := &toolCallPlan{evidenceName: "session_read_strategy_receipt", evidenceArgs: json.RawMessage(`{"read_id":"r-b"}`)}
	if _, blocked := a.gateReadOperation(context.Background(), receipt); blocked || receipt.incompleteReadRoot != "r-b" {
		t.Fatalf("receipt root=%q blocked=%v", receipt.incompleteReadRoot, blocked)
	}
}

func TestLegacyIncompleteReadErrorCarriesPauseReceipt(t *testing.T) {
	path := makeIncompleteReadFixture(t, "legacy-pause.txt", 430, 96, 362, incompleteReadKeyRule)
	args := fmt.Sprintf(`{"path":%q,"intent":"full"}`, path)
	p := &scriptedProvider{turns: [][]provider.Chunk{{toolCallChunk("read", "read_file", args), {Type: provider.ChunkDone}}, textTurn("premature"), textTurn("premature")}}
	a := newLegacyIncompleteReadTestAgentWithOptions(p, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, Options{ContextWindow: 256_000})
	var pause *IncompleteReadError
	if err := a.Run(context.Background(), "Read the complete file."); !errors.As(err, &pause) || pause.Pause == nil || len(pause.Pause.Reads) == 0 {
		t.Fatalf("legacy pause=%+v err=%v", pause, err)
	}
}

func TestReadReferenceAfterWriteRedeliversCurrentEvidence(t *testing.T) {
	a, env, body := deliveryFixture(t)
	a.task.ledger = evidence.NewLedger()
	finalizeDelivery(t, a, "original", body, env)
	a.freezeVisibleReads(provider.ModelMessages(a.Session().Snapshot()))
	a.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Paths: []string{"/w/a"}, Success: true, Mutation: true, Write: true})
	repeated := finalizeDelivery(t, a, "reread", body, env)
	if repeated.readReference != nil || repeated.output != body {
		t.Fatal("fresh evidence was replaced by a historical satisfied marker")
	}
}

func TestDeletedTargetObservationRetiresOnlyItsOperation(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}
	writer, _ := tool.LookupBuiltin("edit_file")
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, writer, undeclaredWriter{name: "bash"})
	for i, path := range paths {
		if err := os.WriteFile(path, []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
		evidenceRound(t, a, fmt.Sprint(i), "edit_file", map[string]any{"path": path, "old_string": "before", "new_string": "after"})
	}
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	out := evidenceRound(t, a, "missing", "read_file", map[string]any{"path": paths[0]})
	if !strings.Contains(out, tool.WriteTargetAbsent) {
		t.Fatalf("missing structured absence: %s", out)
	}
	remaining := a.outstandingReadEvidence(context.Background(), a.task.ledger.ObservationBoundary())
	if len(remaining) != 1 || remaining[0] != paths[1] {
		t.Fatalf("requirements: %v", remaining)
	}
}

func TestIndependentEvidenceWriterInMixedBatch(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(dir, "unread"), filepath.Join(dir, "observed")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writer, _ := tool.LookupBuiltin("edit_file")
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, writer)
	evidenceRound(t, a, "read", "read_file", map[string]any{"path": two})
	var calls []provider.ToolCall
	for i, p := range []string{one, two} {
		args, _ := json.Marshal(map[string]any{"path": p, "old_string": "before", "new_string": "after"})
		calls = append(calls, provider.ToolCall{ID: fmt.Sprint(i), Name: "edit_file", Arguments: string(args)})
	}
	a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})
	b := a.executeBatch(context.Background(), &a.turn, calls)
	if !strings.Contains(b.results[0], "evidence required") {
		t.Fatalf("missing first rejection: %v", b.results)
	}
	got, err := os.ReadFile(two)
	if err != nil || string(got) != "after\n" {
		t.Fatalf("independent write: %s %v %v", got, err, b.results)
	}
}
