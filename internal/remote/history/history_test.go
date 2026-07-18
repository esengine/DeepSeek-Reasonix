package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/remote/contentref"
	"reasonix/internal/remote/protocol"
)

func testBinding(name string) Binding {
	return Binding{
		SnapshotID: protocol.SnapshotID("snapshot-" + name), HostEpoch: "host-test",
		Target:       protocol.RuntimeTarget{WorkspaceID: protocol.WorkspaceID("workspace-" + name), SessionID: protocol.SessionID("session-" + name)},
		RuntimeEpoch: protocol.RuntimeEpoch("runtime-" + name),
	}
}

func newTestStore(t *testing.T, options Options) *Store {
	t.Helper()
	if options.SweepInterval == 0 {
		options.SweepInterval = -1
	}
	store, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func simpleCapture(binding Binding, turns int) Capture {
	messages := []provider.Message{{Role: provider.RoleSystem, Content: "system prefix"}}
	for turn := 0; turn < turns; turn++ {
		messages = append(messages,
			provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("user-%03d", turn)},
			provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("assistant-%03d", turn)},
		)
	}
	return Capture{Binding: binding, Messages: messages}
}

func content(message protocol.HistoryMessage) string {
	if message.Content == nil {
		return ""
	}
	return *message.Content
}

func requireSnapshotExpired(t *testing.T, err error) {
	t.Helper()
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) || remote.Code != protocol.ErrSnapshotExpired {
		t.Fatalf("error = %v, want SNAPSHOT_EXPIRED", err)
	}
}

func TestStoreValidDoesNotRevealBindingIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store := newTestStore(t, Options{
		SnapshotTTL:  time.Minute,
		MaxSnapshots: 2,
		Now:          func() time.Time { return now },
	})
	first := testBinding("valid-first")
	second := testBinding("valid-second")
	if err := store.CaptureSnapshot(simpleCapture(first, 1)); err != nil {
		t.Fatal(err)
	}
	if !store.Valid(first) {
		t.Fatal("fresh complete binding is not valid")
	}

	checks := []Binding{
		{},
		{SnapshotID: first.SnapshotID},
		func() Binding { changed := first; changed.HostEpoch = "host-other"; return changed }(),
		func() Binding { changed := first; changed.Target.SessionID = "session-other"; return changed }(),
		func() Binding { changed := first; changed.RuntimeEpoch = "runtime-other"; return changed }(),
		second,
	}
	for _, binding := range checks {
		if store.Valid(binding) {
			t.Fatalf("mismatched or unknown binding reported valid: %+v", binding)
		}
	}
}

func TestStoreValidHonorsAbsoluteTTLCapacityAndClose(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store := newTestStore(t, Options{
		SnapshotTTL:  time.Minute,
		MaxSnapshots: 1,
		Now:          func() time.Time { return now },
	})
	first := testBinding("valid-capacity-first")
	second := testBinding("valid-capacity-second")
	if err := store.CaptureSnapshot(simpleCapture(first, 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureSnapshot(simpleCapture(second, 1)); err != nil {
		t.Fatal(err)
	}
	if store.Valid(first) {
		t.Fatal("capacity-evicted binding reported valid")
	}
	if !store.Valid(second) {
		t.Fatal("retained binding is not valid")
	}

	now = now.Add(time.Minute)
	if store.Valid(second) {
		t.Fatal("binding at its absolute expiry boundary reported valid")
	}
	if stats := store.Stats(); stats.Snapshots != 0 {
		t.Fatalf("expired validation did not sweep snapshot: %+v", stats)
	}

	third := testBinding("valid-close")
	if err := store.CaptureSnapshot(simpleCapture(third, 1)); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if store.Valid(third) {
		t.Fatal("closed store reported a binding valid")
	}
}

func TestProjectionPreservesWorkbenchSemanticsAndFullBodies(t *testing.T) {
	binding := testBinding("projection")
	largeArguments := `{"command":"` + strings.Repeat("界", 30_000) + `"}`
	largeDiff := strings.Repeat("+full diff line\n", 6_000)
	largeResult := strings.Repeat("result line\n", 8_000)
	failedResult := "blocked: " + strings.Repeat("permission detail ", 8_000)
	display := "edited prompt"

	capture := Capture{
		Binding: binding,
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "system contract"},
			{Role: provider.RoleUser, Content: "edited prompt", Edited: true, Original: "original prompt"},
			{
				Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "complete reasoning", WorkDurationMs: 24_000,
				MemoryCitations: []provider.MemoryCitation{{ID: "memory-1", Source: "MEMORY.md", LineStart: 7, LineEnd: 9, Note: "constraint", Kind: "memory"}},
				ToolCalls: []provider.ToolCall{
					{ID: "call-success", Name: "bash", Arguments: largeArguments, Diff: largeDiff, Added: 9, Removed: 3},
					{ID: "call-failure", Name: "write_file", Arguments: `{"path":"blocked.txt","content":"body"}`},
				},
			},
			{Role: provider.RoleTool, ToolCallID: "call-success", Name: "bash", Content: largeResult},
			{Role: provider.RoleTool, ToolCallID: "call-failure", Name: "write_file", Content: failedResult},
		},
		Metadata:    []MessageMetadata{{MessageIndex: 1, DisplayContent: &display, CreatedAtMs: 1234}},
		Checkpoints: []CheckpointBinding{{MessageIndex: 1, CheckpointID: "checkpoint-opaque-1"}},
	}

	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(capture); err != nil {
		t.Fatal(err)
	}
	page, err := store.latest(binding, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.StartTurn != 0 || page.EndTurn != 1 || page.TotalTurns != 1 || page.ActualTurns != 1 || page.HasOlder {
		t.Fatalf("page range = %#v", page)
	}
	if len(page.Messages) != 5 || content(page.Messages[0]) != "system contract" {
		t.Fatalf("messages = %#v", page.Messages)
	}
	user := page.Messages[1]
	if content(user) != display || user.SubmitText == nil || *user.SubmitText != "original prompt" || user.CheckpointID != "checkpoint-opaque-1" || user.CreatedAtMs != 1234 {
		t.Fatalf("user projection = %#v", user)
	}
	assistant := page.Messages[2]
	if assistant.Reasoning == nil || *assistant.Reasoning != "complete reasoning" || assistant.WorkDurationMs != 24_000 {
		t.Fatalf("assistant reasoning = %#v", assistant)
	}
	if len(assistant.MemoryCitations) != 1 || assistant.MemoryCitations[0] != (eventwire.MemoryCitation{ID: "memory-1", Source: "MEMORY.md", LineStart: 7, LineEnd: 9, Note: "constraint", Kind: "memory"}) {
		t.Fatalf("memory citations = %#v", assistant.MemoryCitations)
	}
	if len(assistant.ToolCalls) != 2 {
		t.Fatalf("tool calls = %#v", assistant.ToolCalls)
	}
	success := assistant.ToolCalls[0]
	if success.Arguments == nil || *success.Arguments != largeArguments || success.ArgumentsArchived || success.Diff == nil || *success.Diff != largeDiff || success.Added != 9 || success.Removed != 3 {
		t.Fatalf("successful tool call lost full data: %#v", success)
	}
	if success.Summary == nil || *success.Summary != "8000 lines" {
		t.Fatalf("tool summary = %#v", success.Summary)
	}
	toolResult := page.Messages[3]
	if toolResult.ToolCallID != "call-success" || toolResult.ToolName != "bash" || content(toolResult) != largeResult || toolResult.ToolResultArchived || toolResult.ToolResultError != nil {
		t.Fatalf("successful tool result = %#v", toolResult)
	}
	failed := page.Messages[4]
	if content(failed) != failedResult || failed.ToolResultError == nil || *failed.ToolResultError != failedResult || failed.ToolResultArchived {
		t.Fatalf("failed tool result lost full error: %#v", failed)
	}
}

func TestProjectionCompactionAndSyntheticSummary(t *testing.T) {
	binding := testBinding("compaction")
	summary := strings.Repeat("summary body\n", 7_000)
	archive := strings.Repeat("archive body\n", 7_000)
	empty := ""
	capture := Capture{
		Binding: binding,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "real task"},
			{Role: provider.RoleAssistant, Content: "before compact"},
			{Role: provider.RoleUser, Content: "<compaction-summary>\ninternal digest\n</compaction-summary>"},
			{Role: provider.RoleAssistant, Content: "after compact"},
		},
		Supplemental: []SupplementalMessage{{
			AfterMessageIndex: 2,
			Message: protocol.HistoryMessage{
				Role: "compaction", Content: &empty, Trigger: "auto", Messages: 42,
				Summary: &summary, Archive: &archive,
			},
		}},
	}
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(capture); err != nil {
		t.Fatal(err)
	}
	page, err := store.latest(binding, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalTurns != 1 || len(page.Messages) != 4 {
		t.Fatalf("page = %#v", page)
	}
	roles := []string{page.Messages[0].Role, page.Messages[1].Role, page.Messages[2].Role, page.Messages[3].Role}
	if fmt.Sprint(roles) != "[user assistant compaction assistant]" {
		t.Fatalf("roles = %v", roles)
	}
	compaction := page.Messages[2]
	if compaction.Summary == nil || *compaction.Summary != summary || compaction.Archive == nil || *compaction.Archive != archive || compaction.Messages != 42 || compaction.Trigger != "auto" {
		t.Fatalf("compaction = %#v", compaction)
	}
}

func TestLogicalTurnsSkipSyntheticAndSteerAndPrefixIsOldestOnly(t *testing.T) {
	binding := testBinding("turns")
	capture := Capture{Binding: binding, Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system prefix"},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleUser, Content: "Continue pursuing the active goal. If it is complete, provide the concise final result."},
		{Role: provider.RoleAssistant, Content: "hidden continuation"},
		{Role: provider.RoleUser, Content: "second"},
		{Role: provider.RoleAssistant, Content: "two"},
		{Role: provider.RoleUser, Content: agent.MidTurnSteerPrefix + "\nupdate the plan"},
		{Role: provider.RoleUser, Content: "third"},
		{Role: provider.RoleAssistant, Content: "three"},
	}}
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(capture); err != nil {
		t.Fatal(err)
	}
	latest, err := store.latest(binding, 2)
	if err != nil {
		t.Fatal(err)
	}
	if latest.StartTurn != 1 || latest.EndTurn != 3 || latest.TotalTurns != 3 || latest.ActualTurns != 2 || !latest.HasOlder || latest.NextCursor == "" {
		t.Fatalf("latest = %#v", latest)
	}
	if len(latest.Messages) != 5 || content(latest.Messages[0]) != "second" || latest.Messages[2].Role != "notice" || content(latest.Messages[2]) != "↪ update the plan" || content(latest.Messages[3]) != "third" {
		t.Fatalf("latest messages = %#v", latest.Messages)
	}
	for _, message := range latest.Messages {
		if message.Role == "system" {
			t.Fatal("system prefix repeated on a middle page")
		}
	}
	oldest, err := store.older(binding, latest.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if oldest.StartTurn != 0 || oldest.EndTurn != 1 || oldest.TotalTurns != 3 || oldest.ActualTurns != 1 || oldest.HasOlder {
		t.Fatalf("oldest = %#v", oldest)
	}
	if len(oldest.Messages) != 4 || oldest.Messages[0].Role != "system" || content(oldest.Messages[1]) != "first" || content(oldest.Messages[3]) != "hidden continuation" {
		t.Fatalf("oldest messages = %#v", oldest.Messages)
	}
}

func TestMemoryCompilerContractNeverBecomesReplayText(t *testing.T) {
	binding := testBinding("memory-compiler")
	raw := `<memory-compiler-execution>{"planner_ir":{"source_event":"/reasonix-develop ship it"}}</memory-compiler-execution>`
	display := "ship it"
	capture := Capture{
		Binding:  binding,
		Messages: []provider.Message{{Role: provider.RoleUser, Content: raw}},
		Metadata: []MessageMetadata{{MessageIndex: 0, DisplayContent: &display}},
	}
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(capture); err != nil {
		t.Fatal(err)
	}
	page, err := store.latest(binding, 1)
	if err != nil {
		t.Fatal(err)
	}
	user := page.Messages[0]
	if content(user) != display || user.SubmitText == nil || *user.SubmitText != "/reasonix-develop ship it" {
		t.Fatalf("compiled history = %#v", user)
	}
	encoded, _ := json.Marshal(user)
	if strings.Contains(string(encoded), "memory-compiler-execution") {
		t.Fatalf("compiler contract leaked: %s", encoded)
	}
}

func TestLegacyPositionalToolIDsAreProjectedIntoAValidPair(t *testing.T) {
	binding := testBinding("legacy-tool")
	capture := Capture{Binding: binding, Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{Name: "todo_write", Arguments: `{"todos":[]}`}}},
		{Role: provider.RoleTool, Name: "todo_write", Content: "Todos updated"},
	}}
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(capture); err != nil {
		t.Fatal(err)
	}
	page, err := store.latest(binding, 1)
	if err != nil {
		t.Fatal(err)
	}
	call := page.Messages[1].ToolCalls[0]
	result := page.Messages[2]
	if call.ID == "" || call.Name != "todo_write" || result.ToolCallID != call.ID || result.ToolName != "todo_write" {
		t.Fatalf("legacy pair = call %#v result %#v", call, result)
	}
}

func TestCaptureAndReturnedPagesAreImmutable(t *testing.T) {
	binding := testBinding("immutable")
	display := "visible"
	summary := "captured summary"
	empty := ""
	capture := Capture{
		Binding: binding,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "raw"},
			{Role: provider.RoleAssistant, Content: "answer", ToolCalls: []provider.ToolCall{{ID: "call", Name: "bash", Arguments: `{"command":"pwd"}`}}},
		},
		Metadata:     []MessageMetadata{{MessageIndex: 0, DisplayContent: &display}},
		Supplemental: []SupplementalMessage{{AfterMessageIndex: 1, Message: protocol.HistoryMessage{Role: "compaction", Content: &empty, Summary: &summary}}},
	}
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(capture); err != nil {
		t.Fatal(err)
	}
	capture.Messages[0].Content = "mutated raw"
	capture.Messages[1].ToolCalls[0].Arguments = "mutated args"
	display = "mutated display"
	summary = "mutated summary"

	first, err := store.latest(binding, 1)
	if err != nil {
		t.Fatal(err)
	}
	if content(first.Messages[0]) != "visible" || *first.Messages[1].ToolCalls[0].Arguments != `{"command":"pwd"}` || *first.Messages[2].Summary != "captured summary" {
		t.Fatalf("capture was not immutable: %#v", first.Messages)
	}
	*first.Messages[0].Content = "client mutation"
	*first.Messages[1].ToolCalls[0].Arguments = "client args"
	*first.Messages[2].Summary = "client summary"
	second, err := store.latest(binding, 1)
	if err != nil {
		t.Fatal(err)
	}
	if content(second.Messages[0]) != "visible" || *second.Messages[1].ToolCalls[0].Arguments != `{"command":"pwd"}` || *second.Messages[2].Summary != "captured summary" {
		t.Fatalf("returned page aliased retained state: %#v", second.Messages)
	}
}

func TestPageTurnExactBoundaries(t *testing.T) {
	binding := testBinding("boundaries")
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(simpleCapture(binding, 201)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.latest(binding, 0); !errors.Is(err, ErrInvalidPageTurns) {
		t.Fatalf("pageTurns=0 error = %v", err)
	}
	if _, err := store.latest(binding, 201); !errors.Is(err, ErrInvalidPageTurns) {
		t.Fatalf("pageTurns=201 error = %v", err)
	}
	latest, err := store.latest(binding, 200)
	if err != nil {
		t.Fatal(err)
	}
	if latest.StartTurn != 1 || latest.EndTurn != 201 || latest.ActualTurns != 200 || len(latest.Messages) != 400 {
		t.Fatalf("200-turn page = start=%d end=%d actual=%d messages=%d", latest.StartTurn, latest.EndTurn, latest.ActualTurns, len(latest.Messages))
	}
	oldest, err := store.older(binding, latest.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if oldest.StartTurn != 0 || oldest.EndTurn != 1 || oldest.ActualTurns != 1 || len(oldest.Messages) != 3 || oldest.Messages[0].Role != "system" {
		t.Fatalf("one-turn oldest page = %#v", oldest)
	}
}

func TestFittingReducesOnlyAtWholeTurnBoundaries(t *testing.T) {
	binding := testBinding("fitting")
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(simpleCapture(binding, 3)); err != nil {
		t.Fatal(err)
	}
	var attempts []int
	page, err := store.LatestFitting(binding, 3, func(candidate protocol.HistoryPage) (bool, error) {
		attempts = append(attempts, candidate.ActualTurns)
		return candidate.ActualTurns <= 1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(attempts) != "[3 2 1]" || page.StartTurn != 2 || page.EndTurn != 3 || page.ActualTurns != 1 || len(page.Messages) != 2 {
		t.Fatalf("attempts/page = %v / %#v", attempts, page)
	}
	if stats := store.Stats(); stats.Cursors != 1 {
		t.Fatalf("failed budget probes leaked cursors: %#v", stats)
	}
}

func TestBudgetedPageShrinksAtTurnBoundaryAndKeepsOldestPrefix(t *testing.T) {
	binding := testBinding("budget-many")
	capture := simpleCapture(binding, 20)
	largeCode := strings.Repeat("C", 150_000)
	empty := ""
	for turn := 0; turn < 20; turn++ {
		// provider indexes: system=0, user=1+2*turn, assistant=2+2*turn.
		capture.Supplemental = append(capture.Supplemental, SupplementalMessage{
			AfterMessageIndex: 2 + 2*turn,
			Message:           protocol.HistoryMessage{Role: "notice", Content: &empty, Code: largeCode},
		})
	}
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(capture); err != nil {
		t.Fatal(err)
	}
	contents, err := contentref.New(binding.HostEpoch, contentref.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(contents.Close)

	latest, err := store.LatestBudgeted(binding, 20, contents, contentref.ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(latest)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) > protocol.SnapshotHistoryBytes {
		t.Fatalf("latest wire bytes = %d, max %d", len(wire), protocol.SnapshotHistoryBytes)
	}
	if latest.ActualTurns >= 20 || latest.ActualTurns < 1 || latest.EndTurn != 20 || latest.StartTurn != 20-latest.ActualTurns || !latest.HasOlder {
		t.Fatalf("latest budget range = %#v", latest)
	}
	if len(latest.Messages) != latest.ActualTurns*3 {
		t.Fatalf("latest split a turn: messages=%d actualTurns=%d", len(latest.Messages), latest.ActualTurns)
	}
	if latest.Messages[0].Role != "user" || content(latest.Messages[0]) != fmt.Sprintf("user-%03d", latest.StartTurn) {
		t.Fatalf("latest first complete turn = %#v", latest.Messages[0])
	}
	for _, message := range latest.Messages {
		if message.Role == "system" {
			t.Fatal("system prefix repeated on budget-reduced middle page")
		}
	}

	oldest, err := store.OlderBudgeted(binding, latest.NextCursor, 20, contents, contentref.ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	oldestWire, _ := json.Marshal(oldest)
	if len(oldestWire) > protocol.SnapshotHistoryBytes || oldest.StartTurn != 0 || oldest.EndTurn != latest.StartTurn || oldest.HasOlder || oldest.Messages[0].Role != "system" {
		t.Fatalf("oldest budget page = range %d-%d, bytes=%d, hasOlder=%v, first=%#v", oldest.StartTurn, oldest.EndTurn, len(oldestWire), oldest.HasOlder, oldest.Messages[0])
	}
	if len(oldest.Messages) != 1+oldest.ActualTurns*3 {
		t.Fatalf("oldest split a turn or lost prefix: messages=%d actual=%d", len(oldest.Messages), oldest.ActualTurns)
	}
}

func TestBudgetedSingleOversizedFieldKeepsTurnAndMarksObjectTruncation(t *testing.T) {
	binding := testBinding("budget-object")
	original := "HEAD" + strings.Repeat("界", protocol.ContentRefObjectBytes/3+100) + "TAIL"
	capture := Capture{Binding: binding, Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "one turn"},
		{Role: provider.RoleAssistant, Content: original},
	}}
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(capture); err != nil {
		t.Fatal(err)
	}
	contents, err := contentref.New(binding.HostEpoch, contentref.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(contents.Close)
	page, err := store.LatestBudgeted(binding, 1, contents, contentref.ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(page)
	if page.ActualTurns != 1 || page.StartTurn != 0 || page.EndTurn != 1 || len(wire) > protocol.SnapshotHistoryBytes {
		t.Fatalf("single turn page = %#v bytes=%d", page, len(wire))
	}
	var descriptor *protocol.ExternalizedField
	for index := range page.Externalized {
		if page.Externalized[index].JSONPointer == "/messages/1/content" {
			descriptor = &page.Externalized[index]
			break
		}
	}
	if descriptor == nil || !descriptor.Truncated || descriptor.OriginalBytes == nil || *descriptor.OriginalBytes != int64(len([]byte(original))) || descriptor.TruncationReason != contentref.ContentObjectLimitReason || descriptor.TotalBytes > protocol.ContentRefObjectBytes {
		t.Fatalf("oversized descriptor = %#v", descriptor)
	}
}

func TestCursorCannotCrossSnapshotTargetOrEpoch(t *testing.T) {
	firstBinding := testBinding("cursor-one")
	secondBinding := testBinding("cursor-two")
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(simpleCapture(firstBinding, 2)); err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureSnapshot(simpleCapture(secondBinding, 2)); err != nil {
		t.Fatal(err)
	}
	latest, err := store.latest(firstBinding, 1)
	if err != nil {
		t.Fatal(err)
	}
	if latest.NextCursor == "" || strings.Contains(string(latest.NextCursor), string(firstBinding.SnapshotID)) || !strings.HasPrefix(string(latest.NextCursor), "hc_") {
		t.Fatalf("cursor is not opaque: %q", latest.NextCursor)
	}
	again, err := store.latest(firstBinding, 1)
	if err != nil || again.NextCursor != latest.NextCursor {
		t.Fatalf("repeated page cursor = %q, err=%v; want %q", again.NextCursor, err, latest.NextCursor)
	}
	_, err = store.older(secondBinding, latest.NextCursor, 1)
	requireSnapshotExpired(t, err)

	variants := []Binding{firstBinding, firstBinding, firstBinding}
	variants[0].Target.SessionID = "other-session"
	variants[1].RuntimeEpoch = "other-runtime"
	variants[2].HostEpoch = "other-host"
	for _, variant := range variants {
		_, err := store.older(variant, latest.NextCursor, 1)
		requireSnapshotExpired(t, err)
	}
	if !store.Release(firstBinding) {
		t.Fatal("release failed")
	}
	_, err = store.older(firstBinding, latest.NextCursor, 1)
	requireSnapshotExpired(t, err)
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Add(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func TestSnapshotTTLExpiresSnapshotAndCursor(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000, 0)}
	binding := testBinding("ttl")
	store := newTestStore(t, Options{SnapshotTTL: time.Minute, Now: clock.Now})
	if err := store.CaptureSnapshot(simpleCapture(binding, 2)); err != nil {
		t.Fatal(err)
	}
	latest, err := store.latest(binding, 1)
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Minute)
	if stats := store.Stats(); stats.Snapshots != 0 || stats.Cursors != 0 || stats.Messages != 0 {
		t.Fatalf("expired stats = %#v", stats)
	}
	_, err = store.older(binding, latest.NextCursor, 1)
	requireSnapshotExpired(t, err)
}

func TestSnapshotCapacityUsesLRUAndCleansCursors(t *testing.T) {
	store := newTestStore(t, Options{MaxSnapshots: 2})
	first := testBinding("capacity-one")
	second := testBinding("capacity-two")
	third := testBinding("capacity-three")
	for _, binding := range []Binding{first, second} {
		if err := store.CaptureSnapshot(simpleCapture(binding, 2)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.latest(binding, 1); err != nil {
			t.Fatal(err)
		}
	}
	// Touch first, making second the least recently used snapshot.
	if _, err := store.latest(first, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureSnapshot(simpleCapture(third, 2)); err != nil {
		t.Fatal(err)
	}
	if stats := store.Stats(); stats.Snapshots != 2 || stats.Cursors != 1 {
		// First has a cursor; third has not been paged. Second's cursor must be gone.
		t.Fatalf("capacity stats = %#v", stats)
	}
	_, err := store.latest(second, 1)
	requireSnapshotExpired(t, err)
	for _, binding := range []Binding{first, third} {
		if _, err := store.latest(binding, 1); err != nil {
			t.Fatalf("retained %s: %v", binding.SnapshotID, err)
		}
	}
}

func TestCaptureValidationRejectsAmbiguousMetadataAndArchivedSupplement(t *testing.T) {
	binding := testBinding("invalid")
	empty := ""
	tests := []Capture{
		{Binding: binding, Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}, Metadata: []MessageMetadata{{MessageIndex: 1}}},
		{Binding: binding, Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}, Metadata: []MessageMetadata{{MessageIndex: 0}, {MessageIndex: 0}}},
		{Binding: binding, Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}, Checkpoints: []CheckpointBinding{{MessageIndex: 0}}},
		{Binding: binding, Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}, Supplemental: []SupplementalMessage{{AfterMessageIndex: 0, Message: protocol.HistoryMessage{Role: "user", Content: &empty}}}},
		{Binding: binding, Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}, Supplemental: []SupplementalMessage{{AfterMessageIndex: 0, Message: protocol.HistoryMessage{Role: "assistant", Content: &empty, ToolResultArchived: true}}}},
		{Binding: binding, Messages: []provider.Message{{Role: provider.Role("alien"), Content: "x"}}},
	}
	for index, capture := range tests {
		store := newTestStore(t, Options{})
		if err := store.CaptureSnapshot(capture); !errors.Is(err, ErrInvalidCapture) {
			t.Fatalf("case %d error = %v, want ErrInvalidCapture", index, err)
		}
	}
}

func TestStressPagesEveryTurnExactlyOnce(t *testing.T) {
	const total = 2_003
	binding := testBinding("stress")
	store := newTestStore(t, Options{})
	if err := store.CaptureSnapshot(simpleCapture(binding, total)); err != nil {
		t.Fatal(err)
	}
	page, err := store.latest(binding, 137)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, total)
	for {
		users := 0
		for _, message := range page.Messages {
			if message.Role != "user" {
				continue
			}
			users++
			text := content(message)
			if seen[text] {
				t.Fatalf("duplicate turn %q", text)
			}
			seen[text] = true
		}
		if users != page.ActualTurns {
			t.Fatalf("range %d-%d contains %d users", page.StartTurn, page.EndTurn, users)
		}
		if !page.HasOlder {
			break
		}
		page, err = store.older(binding, page.NextCursor, 137)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != total {
		t.Fatalf("seen turns = %d, want %d", len(seen), total)
	}
}
