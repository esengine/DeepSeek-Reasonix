package control

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestCompatibilityRewindRequiresConfirmationForPartialCoverage(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	path := filepath.Join(root, "partial.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := agent.NewSession("sys")
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{
		Runner:        ag,
		Executor:      ag,
		SessionDir:    dir,
		SessionPath:   filepath.Join(dir, "partial.jsonl"),
		WorkspaceRoot: root,
		Sink:          event.Discard,
	})
	c.beginCheckpoint("edit partial.txt")
	c.mutationObserver.BeforeMutation("partial.txt", "write_file", checkpoint.CaptureBeforeMutation)
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.mutationObserver.AfterMutation("partial.txt", "write_file")
	c.mutationObserver.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapBashSideEffect, Tool: "bash"})

	plan, err := c.PrepareRewind(0, RewindCode)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CanFiles || !RewindPlanRequiresConfirmation(plan) {
		t.Fatalf("partial plan = %+v, want restorable files with explicit confirmation", plan)
	}
	if err := c.Rewind(0, RewindCode); !errors.Is(err, ErrRewindCoverageConfirmationRequired) {
		t.Fatalf("compatibility Rewind error = %v, want confirmation-required", err)
	}
	if got := string(mustReadFile(t, path)); got != "after" {
		t.Fatalf("unconfirmed rewind changed file to %q", got)
	}

	result, err := c.CommitRewind(plan.PlanID)
	if err != nil || !result.OK {
		t.Fatalf("confirmed CommitRewind result=%+v err=%v", result, err)
	}
	if got := string(mustReadFile(t, path)); got != "before" {
		t.Fatalf("confirmed rewind left file at %q, want before", got)
	}
}

func TestResumeRecoversCommittingCombinedRewind(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	filePath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	diskMode := uint32(fileInfo.Mode().Perm())
	fullMessages := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "answer"},
		{Role: provider.RoleUser, Content: "second"},
		{Role: provider.RoleAssistant, Content: "later"},
	}
	saved := agent.NewSession("")
	saved.Replace(fullMessages[:3])
	if err := saved.Save(sessionPath); err != nil {
		t.Fatal(err)
	}
	forward, err := json.Marshal(fullMessages)
	if err != nil {
		t.Fatal(err)
	}
	checkpointBackup, err := json.Marshal([]*checkpoint.Checkpoint{{
		SchemaVersion: checkpoint.SchemaV2,
		Turn:          1,
		Prompt:        "second",
		MsgIndex:      3,
	}})
	if err != nil {
		t.Fatal(err)
	}
	checkpointDir := ckptDir(sessionPath)
	if err := os.MkdirAll(filepath.Join(checkpointDir, "transactions"), 0o755); err != nil {
		t.Fatal(err)
	}
	tx := checkpoint.TransactionManifest{
		SchemaVersion:       checkpoint.SchemaV2,
		ID:                  "tx-resume-recovery",
		WorkspaceRoot:       root,
		State:               checkpoint.TxCommitting,
		Kind:                "rewind",
		Turn:                1,
		Scope:               checkpoint.RewindBoth,
		HasBoundary:         true,
		BoundaryIndex:       3,
		TruncateFrom:        1,
		ConversationForward: forward,
		CheckpointBackup:    checkpointBackup,
		Targets: []checkpoint.TransactionTarget{{
			Path: "a.txt", AbsPath: filePath, Action: "write", Published: true,
			RestoreExisted: true, RestoreSHA: checkpoint.Digest([]byte("before")), RestoreMode: diskMode,
			ForwardExisted: true, ForwardSHA: checkpoint.Digest([]byte("after")), ForwardMode: diskMode,
			ForwardInline: []byte("after"), BackupPath: filepath.Join(root, ".a.txt.reasonix-recovery.bak"),
		}},
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(checkpointDir, "transactions", tx.ID+".json")
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, Runner: ag, SessionDir: dir, WorkspaceRoot: root})
	c.Resume(loaded, sessionPath)
	if got := ag.Session().Snapshot(); len(got) != len(fullMessages) || got[len(got)-1].Content != "later" {
		t.Fatalf("recovered conversation = %#v, want full forward transcript", got)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "after" {
		t.Fatalf("recovered file = %q, want after", data)
	}
	if got := c.Checkpoints(); len(got) != 1 || got[0].Turn != 1 {
		t.Fatalf("recovered checkpoints = %+v, want turn 1", got)
	}
	if err := json.Unmarshal(mustReadFile(t, manifestPath), &tx); err != nil {
		t.Fatal(err)
	}
	if tx.State != checkpoint.TxAborted {
		t.Fatalf("transaction state = %s, want aborted", tx.State)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func runTwoTurns(t *testing.T) (*Controller, *agent.Agent, *[]event.Event) {
	t.Helper()
	dir := t.TempDir()
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("first answer"),
		textTurn("second answer"),
		textTurn("edited answer"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var events []event.Event
	c := New(Options{
		Runner:     ag,
		Executor:   ag,
		SessionDir: dir,
		Label:      "test",
		Sink:       event.FuncSink(func(e event.Event) { events = append(events, e) }),
	})
	c.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := c.runTurnWithRaw(context.Background(), "first prompt", "first prompt"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if err := c.runTurnWithRaw(context.Background(), "second prompt", "second prompt"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	return c, ag, &events
}

// TestRewindConversationFailsLoudlyAfterCompaction reproduces #3598: once
// compaction shrinks the message log below a turn's recorded boundary, a
// conversation/both rewind to that turn skipped the truncation but still emitted
// a success notice — code rolled back, conversation silently did not.
func TestRewindConversationFailsLoudlyAfterCompaction(t *testing.T) {
	c, ag, events := runTwoTurns(t)

	c.checkpoints.mu.Lock()
	lastTurn := c.checkpoints.turn - 1
	boundary := c.checkpoints.bound[lastTurn]
	c.checkpoints.mu.Unlock()
	if boundary <= 1 {
		t.Fatalf("expected the latest turn's boundary above 1, got bound=%v", c.checkpoints.bound)
	}

	// Auto-compaction replaces the prefix with a summary, shrinking the log below
	// the recorded boundary; compaction does not rewrite checkpoint boundaries.
	sess := ag.Session()
	sess.Messages = []provider.Message{{Role: provider.RoleUser, Content: "summary"}}

	*events = nil
	err := c.Rewind(lastTurn, RewindBoth)
	if err == nil || !strings.Contains(err.Error(), "compacted") {
		t.Fatalf("Rewind after compaction error = %v, want a 'compacted past' failure", err)
	}
	for _, e := range *events {
		if e.Kind == event.Notice && strings.Contains(e.Text, "rewound conversation") {
			t.Fatalf("emitted a false conversation-rewind success after skipping truncation: %q", e.Text)
		}
	}
	if got := len(ag.Session().Messages); got != 1 {
		t.Fatalf("session messages = %d, want the compacted log left intact at 1", got)
	}
}

// TestRewindConversationSucceedsWithLiveBoundary is the companion happy path: a
// boundary still within the log truncates the conversation and reports success.
func TestRewindConversationSucceedsWithLiveBoundary(t *testing.T) {
	c, ag, events := runTwoTurns(t)

	c.checkpoints.mu.Lock()
	lastTurn := c.checkpoints.turn - 1
	boundary := c.checkpoints.bound[lastTurn]
	c.checkpoints.mu.Unlock()

	*events = nil
	if err := c.Rewind(lastTurn, RewindConversation); err != nil {
		t.Fatalf("Rewind with a live boundary: %v", err)
	}
	if got := len(ag.Session().Messages); got != boundary {
		t.Fatalf("session truncated to %d messages, want boundary %d", got, boundary)
	}
	ok := false
	for _, e := range *events {
		if e.Kind == event.Notice && strings.Contains(e.Text, "rewound conversation") {
			ok = true
		}
	}
	if !ok {
		t.Fatal("expected a conversation-rewind success notice")
	}
}

func TestPositionalCompressionPreservesCheckpointLineage(t *testing.T) {
	c, ag, _ := runTwoTurns(t)
	sess := ag.Session()
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("large completed output ", 240)})
	beforeMessages := sess.Snapshot()
	beforeRewrite := sess.RewriteVersion()
	beforeRevision := atomic.LoadInt64(&c.sessionRevision)
	c.checkpoints.mu.Lock()
	beforeBounds := make(map[int]int, len(c.checkpoints.bound))
	maps.Copy(beforeBounds, c.checkpoints.bound)
	c.checkpoints.mu.Unlock()

	if err := c.SummarizeFrom(context.Background(), 0); err != nil {
		t.Fatalf("SummarizeFrom: %v", err)
	}
	if !reflect.DeepEqual(sess.Snapshot(), beforeMessages) {
		t.Fatal("positional compression changed canonical history")
	}
	if got := sess.RewriteVersion(); got != beforeRewrite {
		t.Fatalf("rewrite version = %d, want unchanged %d", got, beforeRewrite)
	}
	if got := atomic.LoadInt64(&c.sessionRevision); got != beforeRevision {
		t.Fatalf("controller session revision = %d, want unchanged %d", got, beforeRevision)
	}
	c.checkpoints.mu.Lock()
	afterBounds := make(map[int]int, len(c.checkpoints.bound))
	maps.Copy(afterBounds, c.checkpoints.bound)
	c.checkpoints.mu.Unlock()
	if !reflect.DeepEqual(afterBounds, beforeBounds) {
		t.Fatalf("checkpoint boundaries changed: before=%v after=%v", beforeBounds, afterBounds)
	}
	state, ok, err := agent.LoadCompactionState(c.SessionPath())
	if err != nil || !ok {
		t.Fatalf("load projection sidecar: ok=%v err=%v", ok, err)
	}
	if state.LastTrigger != agent.CompactionTriggerManual || state.Projection.ProjectionVersion == 0 {
		t.Fatalf("projection state = %+v", state)
	}
	if _, ok := c.checkpoints.boundary(1); !ok {
		t.Fatal("conversation rewind boundary disappeared after compression")
	}
	plan, err := c.PrepareRewind(1, RewindConversation)
	if err != nil || !plan.CanConversation {
		t.Fatalf("conversation rewind unavailable after compression: plan=%+v err=%v", plan, err)
	}
	if err := c.SummarizeFrom(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "no longer present in the model context") {
		t.Fatalf("second positional compression error = %v, want folded-boundary explanation", err)
	}
}

func TestEditPromptPersistsOriginalPrompt(t *testing.T) {
	c, ag, _ := runTwoTurns(t)

	if err := c.Rewind(1, RewindConversation); err != nil {
		t.Fatal(err)
	}
	c.SubmitEditedDisplay("edited prompt", "edited prompt", "second prompt")
	defer c.autosaveWG.Wait()

	var loaded *agent.Session
	deadline := time.Now().Add(time.Second)
	for {
		var err error
		loaded, err = agent.LoadSession(c.SessionPath())
		if err == nil {
			msgs := loaded.Snapshot()
			if len(msgs) >= 2 {
				last := msgs[len(msgs)-2]
				if last.Role == provider.RoleUser && last.Content == "edited prompt" {
					break
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("edited prompt was not persisted before deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	msgs := loaded.Snapshot()
	last := msgs[len(msgs)-2]
	if last.Role != provider.RoleUser || last.Content != "edited prompt" {
		t.Fatalf("last user message = %+v, want edited prompt", last)
	}
	if !last.Edited || last.Original != "second prompt" {
		t.Fatalf("edit metadata = edited:%v original:%q, want edited:true original:%q", last.Edited, last.Original, "second prompt")
	}
	for _, m := range ag.Session().Snapshot() {
		if m.Role == provider.RoleUser && m.Content == "second prompt" {
			t.Fatalf("original prompt stayed as an active model turn: %+v", ag.Session().Snapshot())
		}
	}
}
