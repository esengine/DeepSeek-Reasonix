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
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/readcoord"
	"reasonix/internal/runtimepolicy"
	"reasonix/internal/tool"
)

func TestReadPipelineDefaultFullReadCompletesAndKeepsOneTask(t *testing.T) {
	path := makeShortPagedReadFixture(t, "pages.txt", 2105, 2050, "tail")
	p := &scriptedProvider{turns: [][]provider.Chunk{
		{toolCallChunk("first", "read_file", fmt.Sprintf(`{"path":%q,"intent":"full"}`, path)), {Type: provider.ChunkDone}},
		{toolCallChunk("second", "read_file", fmt.Sprintf(`{"path":%q,"offset":2000,"limit":2000}`, path)), {Type: provider.ChunkDone}},
		textTurn("Read through the tail."),
	}}
	sink := &incompleteReadEventSink{}
	a := newIncompleteReadTestAgent(p, incompleteReadBuiltin(t), NewSession("sys"), sink)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Run(ctx, "Read the whole file."); err != nil {
		t.Fatal(err)
	}
	obs := a.turn.readShadow.coord.Snapshot()
	if len(obs) != 1 || obs[0].State != readcoord.StateSatisfied || obs[0].Pages != 2 {
		t.Fatalf("logical read: %+v", obs)
	}
	if sink.hasCode(event.NoticeCodeReadContinuationRequired) {
		t.Fatal("new pipeline emitted legacy warning")
	}
	if len(a.task.ledger.TextObservations()) != 2 {
		t.Fatal("delivered windows were not recorded")
	}
}

func TestReadPipelineBudgetActuallyStopsDispatch(t *testing.T) {
	for _, axis := range []string{"pages", "time"} {
		t.Run(axis, func(t *testing.T) {
			path := makeShortPagedReadFixture(t, "large.txt", 6500, 6400, "tail")
			inner := &scriptedProvider{turns: [][]provider.Chunk{
				{toolCallChunk("first", "read_file", fmt.Sprintf(`{"path":%q,"intent":"full"}`, path)), {Type: provider.ChunkDone}},
				{toolCallChunk("blocked-next", "read_file", fmt.Sprintf(`{"path":%q,"intent":"full"}`, path)), {Type: provider.ChunkDone}},
				textTurn("Attempted the read."),
			}}
			var a *Agent
			p := &inspectingProvider{inner: inner, before: func(round int, _ provider.Request) {
				if round == 0 {
					policy := readcoord.DefaultPolicy()
					if axis == "pages" {
						policy.MaxPages = 1
					} else {
						policy.MaxActiveTime = time.Nanosecond
					}
					a.turn.readShadow.coord = readcoord.NewWithPolicy(policy)
				}
			}}
			a = newIncompleteReadTestAgent(p, incompleteReadBuiltin(t), NewSession("sys"), event.Discard)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var incomplete *IncompleteReadError
			if err := a.Run(ctx, "Read the whole file."); !errors.As(err, &incomplete) {
				t.Fatalf("want bounded pause, got %v", err)
			}
			if !strings.Contains(toolResultByID(a.Session(), "blocked-next"), "automatic read is paused") {
				t.Fatal("budget did not block execution")
			}
			if incomplete.Pause == nil || len(incomplete.Pause.Reads) != 1 || incomplete.Pause.Reads[0].Reason == "" {
				t.Fatal("bounded pause lost its structured terminal receipt")
			}
			var stored int
			for _, msg := range a.Session().Snapshot() {
				if msg.ReadPause != nil {
					stored++
					if !msg.LocalOnly {
						t.Fatal("pause was provider-visible")
					}
				}
			}
			if stored != 1 {
				t.Fatalf("stored %d terminal receipts", stored)
			}
			archive := filepath.Join(t.TempDir(), "paused.jsonl")
			if err := a.Session().SaveWithEphemeralWriter(archive, nil); err != nil {
				t.Fatal(err)
			}
			reloaded, err := LoadSession(archive)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, msg := range reloaded.Snapshot() {
				if msg.ReadPause != nil && msg.ReadPause.ID == incomplete.Pause.ID {
					found = true
				}
			}
			if !found {
				t.Fatal("save/load lost the terminal receipt")
			}
		})
	}
}

func TestReadPipelineCursorRejectsTamperedBindings(t *testing.T) {
	base := tool.ReadCursor{Version: 2, SessionID: "s", RunGen: 7, ReadID: "r", Path: "/w/a", Snapshot: "sha", NextStart: 5, RequestEnd: 10}
	changes := map[string]func(*tool.ReadCursor){
		"missing session": func(c *tool.ReadCursor) { c.SessionID = "" }, "missing generation": func(c *tool.ReadCursor) { c.RunGen = 0 },
		"missing snapshot": func(c *tool.ReadCursor) { c.Snapshot = "" }, "skip page": func(c *tool.ReadCursor) { c.NextStart = 8 },
		"change range": func(c *tool.ReadCursor) { c.RequestEnd = 99 },
	}
	r := newReadTasks("s", 7)
	base.Binding = r.binding
	other := newReadTasks("s", 7)
	other.remember("r", tool.ReadResultEnvelope{Source: tool.ReadResultSource{CanonicalPath: base.Path, Snapshot: base.Snapshot}, NextCursor: tool.EncodeReadCursor(base)})
	if other.accept(base, base.Path) {
		t.Fatal("another runtime accepted the cursor with the same session and generation")
	}
	r.remember("r", tool.ReadResultEnvelope{Source: tool.ReadResultSource{CanonicalPath: base.Path, Snapshot: base.Snapshot}, NextCursor: tool.EncodeReadCursor(base)})
	if !r.accept(base, base.Path) {
		t.Fatal("issued cursor rejected")
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			c := base
			change(&c)
			if r.accept(c, c.Path) {
				t.Fatal("tampered cursor accepted")
			}
		})
	}
}

func TestReadCursorRejectsConflictingWindows(t *testing.T) {
	for _, args := range []string{`{"path":"a","cursor":"token","offset":0}`, `{"path":"a","cursor":"token","limit":5}`} {
		if _, err := withResolvedReadWindow(json.RawMessage(args), tool.ReadCursor{NextStart: 10}); err == nil {
			t.Fatal("conflicting explicit window was silently discarded")
		}
	}
}

func TestReadPipelineRepeatedRejectedPagesReachBoundedPause(t *testing.T) {
	path := makeShortPagedReadFixture(t, "repeat.txt", 2105, 2050, "tail")
	var turns [][]provider.Chunk
	for i := range 7 {
		turns = append(turns, []provider.Chunk{toolCallChunk(fmt.Sprintf("repeat-%d", i), "read_file", fmt.Sprintf(`{"path":%q,"intent":"full"}`, path)), {Type: provider.ChunkDone}})
	}
	turns = append(turns, textTurn("Read is incomplete."))
	a := newIncompleteReadTestAgent(&scriptedProvider{turns: turns}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var incomplete *IncompleteReadError
	if err := a.Run(ctx, "Read the whole file."); !errors.As(err, &incomplete) {
		t.Fatalf("want bounded pause, got %v", err)
	}
	obs := a.turn.readShadow.coord.Snapshot()
	if len(obs) != 1 || !obs[0].Pivoted || obs[0].Stagnant != 4 {
		t.Fatalf("ladder not enforced: %+v", obs)
	}
}

func TestReadPipelineCursorRejectsCurrentSourceChange(t *testing.T) {
	path := makeShortPagedReadFixture(t, "changing.txt", 2105, 2050, "tail")
	inner := &scriptedProvider{turns: [][]provider.Chunk{
		{toolCallChunk("first", "read_file", fmt.Sprintf(`{"path":%q,"intent":"full"}`, path)), {Type: provider.ChunkDone}}, nil, textTurn("Cannot complete the old snapshot."),
	}}
	var a *Agent
	p := &inspectingProvider{inner: inner, before: func(round int, _ provider.Request) {
		if round == 1 {
			var cursor string
			for _, task := range a.reads.tasks.byID {
				cursor = tool.EncodeReadCursor(task.cursor)
			}
			args, _ := json.Marshal(map[string]string{"path": path, "cursor": cursor})
			inner.turns[1] = []provider.Chunk{toolCallChunk("changed", "read_file", string(args)), {Type: provider.ChunkDone}}
			if err := os.WriteFile(path, []byte(strings.Repeat("y\r\n", 2105)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}}
	a = newIncompleteReadTestAgent(p, incompleteReadBuiltin(t), NewSession("sys"), event.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var incomplete *IncompleteReadError
	if err := a.Run(ctx, "Read the whole file."); !errors.As(err, &incomplete) {
		t.Fatalf("want pause, got %v", err)
	}
	if !strings.Contains(toolResultByID(a.Session(), "changed"), "source changed") {
		t.Fatal("changed source accepted")
	}
}

func TestReadPipelinePartialLineNeverBecomesEvidence(t *testing.T) {
	env := tool.ReadResultEnvelope{Source: tool.ReadResultSource{CanonicalPath: "/a"}, DeliveredRanges: []tool.ReadRange{{Start: 0, End: 2}}, EOF: true}
	got := clipDeliveredRead(env, "1→alpha\n2→beta\n", "1→alpha\n2→be")
	if len(got.DeliveredRanges) != 1 || got.DeliveredRanges[0].End != 1 || got.EOF {
		t.Fatalf("partial line credited: %+v", got)
	}
}

// seedPriorRead represents a completed, model-visible read from a previous
// provider round in tests that exercise executeBatch directly.
func seedPriorRead(t *testing.T, a *Agent, path string) {
	t.Helper()
	reader := incompleteReadBuiltin(t)
	args, _ := json.Marshal(map[string]string{"path": path, "intent": "full"})
	output, env, err := reader.(tool.ReadExecutor).ExecuteRead(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	obs, ok := reader.(tool.ModelTextObserver).ObserveModelText(args, output)
	if !ok {
		t.Fatal("fixture has no numbered lines")
	}
	obs.Snapshot = env.Source.Snapshot
	a.recordModelTextObservationValue(obs)
}

func TestReadEvidenceBlockClearsOnlyAfterPreviousRoundDelivery(t *testing.T) {
	w := evidenceWriter{target: tool.EvidenceTargetInfo{Path: "/w/a", Ranges: []tool.ReadRange{{Start: 0, End: 1}}, Hashes: hashesFor("alpha")}}
	a, ledger := newEvidenceAgent(t, w, true)
	a.svc.tools.Add(undeclaredWriter{name: "bash"})
	if _, blocked := runEvidenceGate(a, "/w/a"); !blocked {
		t.Fatal("unread writer allowed")
	}
	boundary := ledger.ObservationBoundary()
	ledger.RecordTextObservation(evidence.TextObservation{Path: "/w/a", StartLine: 1, LineHashes: hashesFor("alpha")})
	plan := &toolCallPlan{call: provider.ToolCall{Name: "bash", Arguments: `{"command":"echo changed > /w/a"}`}}
	if _, blocked := a.applyEvidenceGates(withObservationBoundary(context.Background(), boundary), plan); !blocked {
		t.Fatal("same-batch read cleared the requirement")
	}
	if out, blocked := a.applyEvidenceGates(context.Background(), plan); blocked {
		t.Fatalf("completed earlier read did not clear the requirement: %s", out.output)
	}
	if len(a.turn.evidenceBlocked.snapshot()) != 0 {
		t.Fatal("historical failure remained pending")
	}
}

func TestReadPipelineEditsRequirePreviousRound(t *testing.T) {
	for _, sameBatch := range []bool{true, false} {
		t.Run(fmt.Sprint(sameBatch), func(t *testing.T) {
			path := makeShortPagedReadFixture(t, "edit.txt", 3, 2, "alpha")
			read := toolCallChunk("read", "read_file", fmt.Sprintf(`{"path":%q,"offset":1,"limit":1}`, path))
			edit := toolCallChunk("edit", "edit_file", fmt.Sprintf(`{"path":%q,"old_string":"alpha","new_string":"beta"}`, path))
			turns := [][]provider.Chunk{{read, {Type: provider.ChunkDone}}, {edit, {Type: provider.ChunkDone}}, textTurn("Finished the attempt.")}
			if sameBatch {
				turns = [][]provider.Chunk{{read, edit, {Type: provider.ChunkDone}}, textTurn("The edit needs a previous read.")}
			}
			writer, ok := tool.LookupBuiltin("edit_file")
			if !ok {
				t.Fatal("missing builtin")
			}
			a := newIncompleteReadTestAgent(&scriptedProvider{turns: turns}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, writer)
			ctx, cancel := context.WithTimeout(withNoClosedLoop(context.Background()), 5*time.Second)
			defer cancel()
			if err := a.Run(ctx, "Inspect and edit this sample."); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if strings.Contains(string(got), "beta") == sameBatch {
				t.Fatalf("wrong write outcome (sameBatch=%v): %q %s", sameBatch, got, toolResultByID(a.Session(), "edit"))
			}
		})
	}
}

func TestRebuildAuthorizationIsPathAndClauseScoped(t *testing.T) {
	a := &Agent{}
	a.writeWorkspaceRoot = "/w"
	for _, tc := range []struct {
		text, path string
		want       bool
	}{
		{"完全重写 a/config.go", "/w/a/config.go", true},
		{"完全重写 a/config.go", "/w/b/config.go", false},
		{"不要完全重写 a/config.go", "/w/a/config.go", false},
		{"完全重写 a/config.go；查看 b/config.go", "/w/b/config.go", false},
		{"rewrite notes.md from scratch", "/w/notes.md", true},
		{"rewrite old-notes.md from scratch", "/w/notes.md", false},
	} {
		a.turn.turnInput = tc.text
		a.turn.constraints = runtimepolicy.ParseConstraints(tc.text)
		a.recordRebuildAuthorization()
		if got := a.rebuildAuthorized(tc.path); got != tc.want {
			t.Errorf("%q %s: %v", tc.text, tc.path, got)
		}
	}
}
