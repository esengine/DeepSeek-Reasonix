package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// --- fixtures ---------------------------------------------------------------

func historySliceTestApp(t *testing.T) *App {
	t.Helper()
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	return app
}

// saveHistorySliceSession builds a session from msgs and saves it to
// dir/name (which also publishes the display index sidecar).
func saveHistorySliceSession(t *testing.T, dir, name string, msgs []provider.Message) (*agent.Session, string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sess := agent.NewSession("")
	for _, m := range msgs {
		sess.Add(m)
	}
	path := filepath.Join(dir, name)
	if err := sess.Save(path); err != nil {
		t.Fatalf("save session: %v", err)
	}
	return sess, path
}

// newLiveHistoryTab installs a running controller for sess as the app's only
// tab and returns the tab.
func newLiveHistoryTab(t *testing.T, app *App, dir, sessionPath string, sess *agent.Session) *WorkspaceTab {
	t.Helper()
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Executor:    exec,
		SessionDir:  dir,
		SessionPath: sessionPath,
		Label:       "test",
		Sink:        event.Discard,
	})
	t.Cleanup(ctrl.Close)
	tab := &WorkspaceTab{
		ID:          "test",
		Scope:       "global",
		SessionPath: sessionPath,
		Ready:       true,
		Ctrl:        ctrl,
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	return tab
}

// newColdHistoryTab installs a controller-less tab; the session file is
// expected at tab.SessionPath inside tabSessionDir(tab).
func newColdHistoryTab(t *testing.T, app *App) *WorkspaceTab {
	t.Helper()
	tab := &WorkspaceTab{
		ID:          "cold",
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	return tab
}

func historySliceUser(i int, text string) provider.Message {
	return provider.Message{Role: provider.RoleUser, Content: text, CreatedAt: 1_700_000_000_000 + int64(i)}
}

func historySliceAssistant(i int, text string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, Content: text, CreatedAt: 1_700_000_000_000 + int64(i)}
}

// historySliceToolTurn builds one tool-heavy turn: user, assistant with two
// calls, two results, assistant with one call, one result, final answer.
func historySliceToolTurn(i int) []provider.Message {
	call := func(suffix string) provider.ToolCall {
		return provider.ToolCall{
			ID:        fmt.Sprintf("call-%d-%s", i, suffix),
			Name:      "read_file",
			Arguments: fmt.Sprintf(`{"path":"file-%d-%s.txt"}`, i, suffix),
		}
	}
	result := func(suffix string) provider.Message {
		return provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: fmt.Sprintf("call-%d-%s", i, suffix),
			Name:       "read_file",
			Content:    fmt.Sprintf("contents of file %d %s\nline2", i, suffix),
			CreatedAt:  1_700_000_000_000 + int64(i),
		}
	}
	return []provider.Message{
		historySliceUser(i, fmt.Sprintf("question-%d", i)),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call("a"), call("b")}, CreatedAt: 1_700_000_000_000 + int64(i)},
		result("a"),
		result("b"),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call("c")}, CreatedAt: 1_700_000_000_000 + int64(i)},
		result("c"),
		historySliceAssistant(i, fmt.Sprintf("answer-%d", i)),
	}
}

// referenceHistoryRows is the full-history conversion the slice pages must
// reassemble to, computed through the legacy full-walk helpers.
func referenceHistoryRows(t *testing.T, sessionDir, sessionPath string) []HistoryMessage {
	t.Helper()
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	msgs := historyProviderMessagesWithPersistedTimes(loaded.Snapshot(), sessionPath)
	return historyMessagesWithPlannerDisplays(
		msgs,
		sessionDisplayResolver(sessionDir, sessionPath),
		sessionPlannerDisplayTurns(sessionDir, sessionPath),
		nil,
	)
}

// collectHistorySlicePages pages from the latest to the oldest, failing on
// stale cursors or cursor cycles.
func collectHistorySlicePages(t *testing.T, app *App, tabID string, req HistorySliceRequest) []HistorySlice {
	t.Helper()
	pages := []HistorySlice{}
	cursor := ""
	for i := range 10000 {
		req.Cursor = cursor
		page := app.HistorySliceForTab(tabID, req)
		if page.Stale {
			t.Fatalf("page %d unexpectedly stale", i)
		}
		if page.Entries == nil {
			t.Fatalf("page %d: Entries is nil", i)
		}
		pages = append(pages, page)
		if !page.HasOlder {
			if page.NextCursor != "" {
				t.Fatalf("page %d: HasOlder=false but NextCursor set", i)
			}
			return pages
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			t.Fatalf("page %d: cursor did not advance", i)
		}
		cursor = page.NextCursor
	}
	t.Fatal("paging did not terminate")
	return nil
}

// concatHistoryPages stitches pages (newest-first) into the full row sequence.
func concatHistoryPages(pages []HistorySlice) []HistoryMessage {
	out := []HistoryMessage{}
	for _, page := range slices.Backward(pages) {
		for _, e := range page.Entries {
			out = append(out, e.Message)
		}
	}
	return out
}

// assertPagesMatchReference asserts the paged rows exactly reassemble the full
// conversion: no duplication, no omission.
func assertPagesMatchReference(t *testing.T, pages []HistorySlice, reference []HistoryMessage) {
	t.Helper()
	got := concatHistoryPages(pages)
	if !reflect.DeepEqual(got, reference) {
		n := min(len(got), len(reference))
		for i := range n {
			if !reflect.DeepEqual(got[i], reference[i]) {
				t.Fatalf("row %d differs:\n got: %+v\nwant: %+v", i, got[i], reference[i])
			}
		}
		t.Fatalf("row count = %d, want %d", len(got), len(reference))
	}
	// Entry IDs and orders must be unique and strictly increasing.
	seen := map[string]bool{}
	lastOrder, lastTurn := -1, -1
	for _, page := range slices.Backward(pages) {
		for _, e := range page.Entries {
			if seen[e.EntryID] {
				t.Fatalf("duplicate entry ID %s", e.EntryID)
			}
			seen[e.EntryID] = true
			if e.Order < lastOrder {
				t.Fatalf("entry order regressed: %d after %d", e.Order, lastOrder)
			}
			if e.Turn < lastTurn {
				t.Fatalf("entry turn regressed: %d after %d", e.Turn, lastTurn)
			}
			lastOrder, lastTurn = e.Order, e.Turn
		}
	}
}

// --- budget tests -----------------------------------------------------------

func TestHistorySliceTurnBudget(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}
	for i := range 30 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("question-%d", i)), historySliceAssistant(i, fmt.Sprintf("answer-%d", i)))
	}
	sess, path := saveHistorySliceSession(t, dir, "turns.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	pages := collectHistorySlicePages(t, app, "test", HistorySliceRequest{Turns: 12, Entries: 1000, Bytes: 8 << 20})
	if len(pages) != 3 {
		t.Fatalf("pages = %d, want 3 (12+12+6 turns)", len(pages))
	}
	if pages[0].TotalTurns != 30 {
		t.Fatalf("TotalTurns = %d, want 30", pages[0].TotalTurns)
	}
	if pages[0].EndTurn-pages[0].StartTurn+1 != 12 {
		t.Fatalf("page 1 spans turns %d..%d, want exactly 12", pages[0].StartTurn, pages[0].EndTurn)
	}
	if pages[2].StartTurn != 1 || pages[2].HasOlder {
		t.Fatalf("oldest page StartTurn = %d HasOlder = %v, want 1/false", pages[2].StartTurn, pages[2].HasOlder)
	}
	// The oldest page includes the pre-turn system message.
	if got := pages[2].Entries[0].Message.Role; got != "system" {
		t.Fatalf("oldest page first row role = %q, want system", got)
	}
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
}

func TestHistorySliceEntryBudget(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	var msgs []provider.Message
	for i := range 150 {
		// One user + one assistant per turn = 2 entries per turn.
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, fmt.Sprintf("a%d", i)))
	}
	sess, path := saveHistorySliceSession(t, dir, "entries.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	pages := collectHistorySlicePages(t, app, "test", HistorySliceRequest{Turns: 500, Entries: 120, Bytes: 8 << 20})
	if len(pages) != 3 {
		t.Fatalf("pages = %d, want 3 (120+120+60)", len(pages))
	}
	if len(pages[0].Entries) != 120 || len(pages[1].Entries) != 120 || len(pages[2].Entries) != 60 {
		t.Fatalf("entry counts = %d/%d/%d, want 120/120/60", len(pages[0].Entries), len(pages[1].Entries), len(pages[2].Entries))
	}
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
}

func TestHistorySliceByteBudget(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	// 60KiB contents stay under the 64KiB ref threshold, so they inline and
	// count against the byte budget: 512KiB fits 8 entries (480KiB), the 9th
	// would exceed it.
	body := strings.Repeat("x", 60<<10)
	var msgs []provider.Message
	for i := range 20 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, body))
	}
	sess, path := saveHistorySliceSession(t, dir, "bytes.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	page := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 500, Entries: 1000, Bytes: 512 << 10})
	if page.Stale {
		t.Fatal("unexpected stale page")
	}
	// The byte budget keeps the maximal suffix under 512KiB: 8 user+assistant
	// pairs (8×60KiB ≈ 480KiB); the 9th assistant body would exceed it.
	if len(page.Entries) != 16 {
		t.Fatalf("entries = %d, want 16 (8 pairs × 60KiB under 512KiB)", len(page.Entries))
	}
	if !page.HasOlder {
		t.Fatal("HasOlder = false, want true")
	}
	pages := collectHistorySlicePages(t, app, "test", HistorySliceRequest{Turns: 500, Entries: 1000, Bytes: 512 << 10})
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
}

func TestHistorySliceGiantTurnSpansPages(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	// One user turn followed by 600 tool interactions — a single turn that no
	// page can hold; pagination must cut at message boundaries.
	msgs := []provider.Message{historySliceUser(0, "giant turn")}
	for i := range 300 {
		id := fmt.Sprintf("call-%d", i)
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "bash", Arguments: fmt.Sprintf(`{"command":"step %d"}`, i)}}, CreatedAt: 1_700_000_000_000 + int64(i)},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "bash", Content: fmt.Sprintf("step %d output", i), CreatedAt: 1_700_000_000_000 + int64(i)},
		)
	}
	msgs = append(msgs, historySliceAssistant(0, "done"))
	sess, path := saveHistorySliceSession(t, dir, "giant.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	pages := collectHistorySlicePages(t, app, "test", HistorySliceRequest{Turns: 12, Entries: 50, Bytes: 512 << 10})
	if len(pages) < 5 {
		t.Fatalf("pages = %d, want the giant turn spread over many pages", len(pages))
	}
	for i, page := range pages {
		if page.TotalTurns != 1 {
			t.Fatalf("page %d TotalTurns = %d, want 1", i, page.TotalTurns)
		}
	}
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
}

func TestHistorySliceCachesTodoDerivationAcrossPages(t *testing.T) {
	app := historySliceTestApp(t)
	msgs := []provider.Message{historySliceUser(0, "large todo turn")}
	for i := range 300 {
		msgs = append(msgs, historySliceAssistant(i, fmt.Sprintf("progress-%d", i)))
	}
	msgs = append(msgs,
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "todo-1", Name: "todo_write", Arguments: `{"todos":[{"content":"ship","status":"in_progress"}]}`,
		}}},
		provider.Message{Role: provider.RoleTool, ToolCallID: "todo-1", Name: "todo_write", Content: "Todos updated"},
	)
	src := newInMemoryHistorySliceSource("todo-cache", msgs, func(s string) string { return s }, agent.PersistedState{}, false)
	src.digest = "todo-cache-digest"
	src.cacheKey = "todo-cache-key"
	originalFetch := src.fetch
	fetches := 0
	src.fetch = func(lo, hi int) ([]provider.Message, error) {
		fetches++
		return originalFetch(lo, hi)
	}
	req := HistorySliceRequest{Turns: 12, Entries: 1000, Bytes: 8 << 20}
	if page, err := app.pageHistorySliceSource(src, req, func(s string) string { return s }, nil, nil, ""); err != nil || len(page.Entries) == 0 {
		t.Fatalf("first todo page = entries:%d err:%v", len(page.Entries), err)
	}
	firstFetches := fetches
	if firstFetches < 5 {
		t.Fatalf("first todo derivation used %d fetches, want a full two-pass scan", firstFetches)
	}
	if _, err := app.pageHistorySliceSource(src, req, func(s string) string { return s }, nil, nil, ""); err != nil {
		t.Fatalf("second todo page: %v", err)
	}
	if delta := fetches - firstFetches; delta != 1 {
		t.Fatalf("cached page added %d fetches, want only its page window", delta)
	}
}

func TestHistoryDerivedCacheRetriesAfterTransientFailure(t *testing.T) {
	var cache historyDerivedCache
	calls := 0
	compute := func() (map[string]string, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary read failure")
		}
		return map[string]string{"todo-1": `{"todos":[]}`}, nil
	}
	if _, err := cache.todoArgs("session-identity", compute); err == nil {
		t.Fatal("first derivation error = nil, want transient failure")
	}
	got, err := cache.todoArgs("session-identity", compute)
	if err != nil {
		t.Fatalf("retry derivation: %v", err)
	}
	if calls != 2 || got["todo-1"] == "" {
		t.Fatalf("retry result = %+v after %d calls, want successful recompute", got, calls)
	}
}

// --- content refs + chunks --------------------------------------------------

func TestHistorySliceContentRefChunkRoundTrip(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	big := strings.Repeat("abcdefghij", 10_000) // 100KiB ASCII
	msgs := []provider.Message{
		historySliceUser(0, "q"),
		historySliceAssistant(0, big),
	}
	sess, path := saveHistorySliceSession(t, dir, "big.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	page := app.HistorySliceForTab("test", HistorySliceRequest{})
	if page.Stale || len(page.Entries) != 2 {
		t.Fatalf("page = stale:%v entries:%d, want 2 fresh entries", page.Stale, len(page.Entries))
	}
	entry := page.Entries[1]
	if len(entry.Refs) != 1 {
		t.Fatalf("refs = %+v, want exactly one content ref", entry.Refs)
	}
	ref := entry.Refs[0]
	if ref.Field != "content" || ref.Size != len(big) {
		t.Fatalf("ref = %+v, want content size %d", ref, len(big))
	}
	if len(entry.Message.Content) > historyFieldPreviewBytes {
		t.Fatalf("inline preview = %d bytes, want <= %d", len(entry.Message.Content), historyFieldPreviewBytes)
	}
	if !strings.HasPrefix(big, entry.Message.Content) {
		t.Fatal("inline value is not a prefix preview of the original")
	}

	var b strings.Builder
	chunks := 0
	for i := 0; ; i++ {
		chunk := app.HistoryContentForTab("test", ref, i)
		if chunk.Stale {
			t.Fatalf("chunk %d unexpectedly stale", i)
		}
		if chunk.EntryID != ref.EntryID || chunk.Field != "content" {
			t.Fatalf("chunk %d identity = %s/%s", i, chunk.EntryID, chunk.Field)
		}
		if i == 0 {
			chunks = chunk.Chunks
			if chunks != ref.Chunks {
				t.Fatalf("chunk count = %d, ref says %d", chunks, ref.Chunks)
			}
		}
		if len(chunk.Data) > historyContentChunkBytes {
			t.Fatalf("chunk %d = %d bytes, over budget", i, len(chunk.Data))
		}
		b.WriteString(chunk.Data)
		if chunk.Done {
			break
		}
	}
	if b.String() != big {
		t.Fatal("reassembled chunks do not equal the original content")
	}
}

func TestHistorySliceContentRefUTF8Boundary(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	// "界🙂" is 7 bytes (3 + 4); repeating it guarantees 256KiB chunk
	// boundaries land mid-rune unless the splitter backs off.
	big := strings.Repeat("界🙂", 40_000) // 280KiB
	msgs := []provider.Message{
		historySliceUser(0, "q"),
		historySliceAssistant(0, big),
	}
	sess, path := saveHistorySliceSession(t, dir, "utf8.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	page := app.HistorySliceForTab("test", HistorySliceRequest{})
	if len(page.Entries) != 2 || len(page.Entries[1].Refs) != 1 {
		t.Fatalf("entries = %+v, want the big assistant row with one ref", page.Entries)
	}
	ref := page.Entries[1].Refs[0]
	var b strings.Builder
	for i := 0; ; i++ {
		chunk := app.HistoryContentForTab("test", ref, i)
		if chunk.Stale {
			t.Fatalf("chunk %d stale", i)
		}
		if !utf8.ValidString(chunk.Data) {
			t.Fatalf("chunk %d is not valid UTF-8 (split mid-rune)", i)
		}
		if len(chunk.Data) > historyContentChunkBytes {
			t.Fatalf("chunk %d = %d bytes, over budget", i, len(chunk.Data))
		}
		b.WriteString(chunk.Data)
		if chunk.Done {
			break
		}
	}
	if b.String() != big {
		t.Fatal("UTF-8 reassembly mismatch")
	}
}

func TestHistorySliceContentRefStaleAfterSave(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	big := strings.Repeat("z", 100<<10)
	msgs := []provider.Message{historySliceUser(0, "q"), historySliceAssistant(0, big)}
	sess, path := saveHistorySliceSession(t, dir, "stale-ref.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	page := app.HistorySliceForTab("test", HistorySliceRequest{})
	ref := page.Entries[1].Refs[0]

	sess.Add(historySliceUser(1, "more"))
	sess.Add(historySliceAssistant(1, "more"))
	if err := sess.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	chunk := app.HistoryContentForTab("test", ref, 0)
	if !chunk.Stale {
		t.Fatal("chunk after revision bump should be stale")
	}
}

func TestHistorySliceColdContentRefUsesAuthoritativeEventTail(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	sess, path := saveHistorySliceSession(t, dir, "cold-content-tail.jsonl", []provider.Message{
		historySliceUser(0, "old question"), historySliceAssistant(0, "old answer"),
	})
	oldModel, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("authoritative-tail-", 8_000)
	sess.Add(historySliceUser(1, "new question"))
	sess.Add(historySliceAssistant(1, big))
	if err := sess.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot tail: %v", err)
	}
	if err := os.WriteFile(path, oldModel, 0o600); err != nil {
		t.Fatalf("restore stale display model: %v", err)
	}
	logFile, err := os.OpenFile(store.SessionEventLog(path), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open event log: %v", err)
	}
	if _, err := logFile.WriteString(`{"schema_version":1,"type":"append","mess`); err != nil {
		logFile.Close()
		t.Fatalf("append torn event: %v", err)
	}
	if err := logFile.Close(); err != nil {
		t.Fatalf("close event log: %v", err)
	}
	tab.SessionPath = path

	page := app.HistorySliceForTab("cold", HistorySliceRequest{})
	if page.Source != "event-log" {
		t.Fatalf("Source = %q, want event-log recovery", page.Source)
	}
	entry := page.Entries[len(page.Entries)-1]
	if len(entry.Refs) != 1 {
		t.Fatalf("tail refs = %+v, want one content ref", entry.Refs)
	}
	chunk := app.HistoryContentForTab("cold", entry.Refs[0], 0)
	if chunk.Stale || chunk.Data == "" || !strings.HasPrefix(big, chunk.Data) {
		t.Fatalf("damaged-log prefix content chunk = stale:%v bytes:%d", chunk.Stale, len(chunk.Data))
	}
}

// --- cursor staleness -------------------------------------------------------

func TestHistorySliceCursorStaleOnRevisionBump(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	var msgs []provider.Message
	for i := range 20 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, fmt.Sprintf("a%d", i)))
	}
	sess, path := saveHistorySliceSession(t, dir, "stale.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	page1 := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 5})
	if !page1.HasOlder || page1.NextCursor == "" {
		t.Fatalf("page 1 HasOlder=%v cursor=%q", page1.HasOlder, page1.NextCursor)
	}
	if !page1.RevisionKnown || page1.Revision <= 0 || page1.Digest == "" {
		t.Fatalf("page 1 identity = known:%v revision:%d digest:%q, want canonical fingerprint", page1.RevisionKnown, page1.Revision, page1.Digest)
	}

	sess.Add(historySliceUser(20, "q20"))
	sess.Add(historySliceAssistant(20, "a20"))
	if err := sess.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	page2 := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 5, Cursor: page1.NextCursor})
	if !page2.Stale {
		t.Fatal("continuing with a pre-save cursor must be stale")
	}
	if page2.Entries == nil || len(page2.Entries) != 0 {
		t.Fatalf("stale page entries = %v, want empty non-nil", page2.Entries)
	}
	if !page2.RevisionKnown || page2.Revision <= page1.Revision || page2.Digest == "" || page2.Digest == page1.Digest {
		t.Fatalf("stale page identity = known:%v revision:%d digest:%q, want advanced canonical fingerprint", page2.RevisionKnown, page2.Revision, page2.Digest)
	}
	encoded, _ := json.Marshal(page2)
	if !strings.Contains(string(encoded), `"entries":[]`) {
		t.Fatalf("stale page JSON must encode entries as []: %s", encoded)
	}
}

func TestHistorySliceGarbageCursorServesLatest(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	msgs := []provider.Message{historySliceUser(0, "q"), historySliceAssistant(0, "a")}
	sess, path := saveHistorySliceSession(t, dir, "garbage.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	page := app.HistorySliceForTab("test", HistorySliceRequest{Cursor: "!!!not-a-cursor!!!"})
	if page.Stale || len(page.Entries) != 2 {
		t.Fatalf("garbage cursor: stale=%v entries=%d, want latest page", page.Stale, len(page.Entries))
	}
}

// --- cold tabs --------------------------------------------------------------

func TestHistorySliceColdTabFromIndex(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	var msgs []provider.Message
	for i := range 20 {
		msgs = append(msgs, historySliceToolTurn(i)...)
	}
	_, path := saveHistorySliceSession(t, dir, "cold.jsonl", msgs)
	tab.SessionPath = path

	if _, err := os.Stat(store.SessionDisplayIndex(path)); err != nil {
		t.Fatalf("save should have written the display index: %v", err)
	}
	pages := collectHistorySlicePages(t, app, "cold", HistorySliceRequest{Turns: 5, Entries: 40})
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
}

func TestHistorySliceColdTabScanFallbackAndRebuild(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	var msgs []provider.Message
	for i := range 10 {
		msgs = append(msgs, historySliceToolTurn(i)...)
	}
	_, path := saveHistorySliceSession(t, dir, "cold-scan.jsonl", msgs)
	tab.SessionPath = path
	indexPath := store.SessionDisplayIndex(path)

	// Delete the index: the first request must page correctly via streaming
	// scan (no full LoadSession) and republish the index.
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	pages := collectHistorySlicePages(t, app, "cold", HistorySliceRequest{Turns: 4, Entries: 30})
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
	if _, err := agent.LoadSessionDisplayIndex(indexPath); err != nil {
		t.Fatalf("index should be republished after scan fallback: %v", err)
	}

	// Corrupt the index: same guarantees.
	if err := os.WriteFile(indexPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	pages = collectHistorySlicePages(t, app, "cold", HistorySliceRequest{Turns: 4, Entries: 30})
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
	idx, err := agent.LoadSessionDisplayIndex(indexPath)
	if err != nil {
		t.Fatalf("corrupt index should be rebuilt: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if idx.TranscriptSize != info.Size() {
		t.Fatalf("rebuilt index TranscriptSize = %d, file size = %d", idx.TranscriptSize, info.Size())
	}
}

func TestHistorySliceColdTabSizeGuard(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	var msgs []provider.Message
	for i := range 5 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, fmt.Sprintf("a%d", i)))
	}
	_, path := saveHistorySliceSession(t, dir, "cold-size.jsonl", msgs)
	tab.SessionPath = path
	// Model a pre-WAL checkpoint. Once a native event log exists it is the
	// canonical transcript, so direct edits to the compatibility JSONL anchor
	// must not supersede it.
	removeHistorySliceNativeState(t, path)

	// Append a message line directly to the .jsonl: the index TranscriptSize
	// no longer matches the file size and must be treated as stale — the page
	// must come from a rescan and include the appended message, not corrupt
	// offset slicing.
	extra, err := json.Marshal(historySliceAssistant(5, "appended-externally"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(extra, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	page := app.HistorySliceForTab("cold", HistorySliceRequest{Turns: 12})
	if page.Stale {
		t.Fatal("unexpected stale page")
	}
	last := page.Entries[len(page.Entries)-1]
	if last.Message.Content != "appended-externally" {
		t.Fatalf("latest entry content = %q, want the externally appended message", last.Message.Content)
	}
}

func TestHistorySliceColdUsesAuthoritativeEventLogTail(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	base := []provider.Message{historySliceUser(0, "old question"), historySliceAssistant(0, "old answer")}
	sess, path := saveHistorySliceSession(t, dir, "cold-event-tail.jsonl", base)
	oldReadModel, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read old display model: %v", err)
	}
	// Current saves advance both files. Restore the old read model afterward to
	// model a crash/older build and prove the event log still wins.
	sess.Add(historySliceUser(1, "new question"))
	sess.Add(historySliceAssistant(1, "new answer"))
	if err := sess.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}
	if err := os.WriteFile(path, oldReadModel, 0o600); err != nil {
		t.Fatalf("restore stale display model: %v", err)
	}
	tab.SessionPath = path

	page := app.HistorySliceForTab("cold", HistorySliceRequest{Turns: 12})
	if page.Source != "event-log" {
		t.Fatalf("Source = %q, want event-log", page.Source)
	}
	if len(page.Entries) == 0 || page.Entries[len(page.Entries)-1].Message.Content != "new answer" {
		t.Fatalf("latest cold entry = %+v, want event-log tail", page.Entries)
	}
}

func TestHistorySliceColdEmptySession(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tab.SessionPath = path

	page := app.HistorySliceForTab("cold", HistorySliceRequest{})
	if page.Stale || page.HasOlder || len(page.Entries) != 0 || page.TotalTurns != 0 {
		t.Fatalf("empty session page = %+v", page)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"entries":[]`) {
		t.Fatalf("empty page must encode entries as []: %s", encoded)
	}
}

// --- live fallback + unsaved tail -------------------------------------------

func TestHistorySliceLiveUnsavedTail(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	var msgs []provider.Message
	for i := range 5 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, fmt.Sprintf("a%d", i)))
	}
	sess, path := saveHistorySliceSession(t, dir, "tail.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	// Unsaved appends live only in memory; the index covers the persisted
	// prefix and the tail must be classified in memory.
	sess.Add(historySliceUser(5, "q5-unsaved"))
	sess.Add(historySliceAssistant(5, "a5-unsaved"))

	page := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 12})
	if page.Stale {
		t.Fatal("unexpected stale page")
	}
	if page.TotalTurns != 6 {
		t.Fatalf("TotalTurns = %d, want 6 including the unsaved tail", page.TotalTurns)
	}
	last := page.Entries[len(page.Entries)-1]
	if last.Message.Content != "a5-unsaved" || last.Turn != 6 {
		t.Fatalf("latest entry = %q turn %d, want a5-unsaved turn 6", last.Message.Content, last.Turn)
	}
}

func TestHistorySliceLiveFallbackRebuildsIndex(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	var msgs []provider.Message
	for i := range 6 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, fmt.Sprintf("a%d", i)))
	}
	sess, path := saveHistorySliceSession(t, dir, "fallback.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)
	indexPath := store.SessionDisplayIndex(path)
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}

	// The request falls back to in-memory classification and stays correct…
	pages := collectHistorySlicePages(t, app, "test", HistorySliceRequest{Turns: 2, Entries: 10})
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))

	// …and the single-flight background rebuild republishes the index.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := agent.LoadSessionDisplayIndex(indexPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background rebuild did not republish the display index")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestHistorySliceSourceField pins the diagnostic read-path label: cold pages
// report "index" (display-index hit) or "scan" (streaming rebuild), live
// pages "live-index" or "live-fallback".
func TestHistorySliceSourceField(t *testing.T) {
	newSession := func(t *testing.T, name string) (*App, *agent.Session, string) {
		app := historySliceTestApp(t)
		tab := newColdHistoryTab(t, app)
		dir := tabSessionDir(tab)
		var msgs []provider.Message
		for i := range 4 {
			msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, fmt.Sprintf("a%d", i)))
		}
		sess, path := saveHistorySliceSession(t, dir, name, msgs)
		tab.SessionPath = path
		return app, sess, path
	}

	t.Run("cold index hit", func(t *testing.T) {
		app, _, _ := newSession(t, "src-index.jsonl")
		if page := app.HistorySliceForTab("cold", HistorySliceRequest{}); page.Source != "index" {
			t.Fatalf("Source = %q, want index", page.Source)
		}
	})

	t.Run("cold scan fallback", func(t *testing.T) {
		app, _, path := newSession(t, "src-scan.jsonl")
		if err := os.Remove(store.SessionDisplayIndex(path)); err != nil {
			t.Fatal(err)
		}
		if page := app.HistorySliceForTab("cold", HistorySliceRequest{}); page.Source != "scan" {
			t.Fatalf("Source = %q, want scan", page.Source)
		}
	})

	t.Run("live index hit", func(t *testing.T) {
		app, sess, path := newSession(t, "src-live-index.jsonl")
		newLiveHistoryTab(t, app, filepath.Dir(path), path, sess)
		if page := app.HistorySliceForTab("test", HistorySliceRequest{}); page.Source != "live-index" {
			t.Fatalf("Source = %q, want live-index", page.Source)
		}
	})

	t.Run("live fallback", func(t *testing.T) {
		app, sess, path := newSession(t, "src-live-fallback.jsonl")
		newLiveHistoryTab(t, app, filepath.Dir(path), path, sess)
		if err := os.Remove(store.SessionDisplayIndex(path)); err != nil {
			t.Fatal(err)
		}
		if page := app.HistorySliceForTab("test", HistorySliceRequest{}); page.Source != "live-fallback" {
			t.Fatalf("Source = %q, want live-fallback", page.Source)
		}
	})
}

// --- entry IDs --------------------------------------------------------------

func TestHistorySliceEntryIDsStableAcrossAppends(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	var msgs []provider.Message
	for i := range 3 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, fmt.Sprintf("a%d", i)))
	}
	sess, path := saveHistorySliceSession(t, dir, "ids.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	before := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 500, Entries: 1000})

	sess.Add(historySliceUser(3, "q3"))
	sess.Add(historySliceAssistant(3, "a3"))
	if err := sess.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	after := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 500, Entries: 1000})
	if len(after.Entries) != len(before.Entries)+2 {
		t.Fatalf("entries after append = %d, want %d", len(after.Entries), len(before.Entries)+2)
	}
	for i := range before.Entries {
		if before.Entries[i].EntryID != after.Entries[i].EntryID {
			t.Fatalf("entry %d ID changed across append-only save: %s -> %s", i, before.Entries[i].EntryID, after.Entries[i].EntryID)
		}
	}
}

// --- classification ---------------------------------------------------------

func TestHistorySliceSyntheticAndSteerTurns(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	msgs := []provider.Message{
		historySliceUser(0, "real question 1"),
		historySliceAssistant(0, "answer 1"),
		{Role: provider.RoleUser, Content: agent.MidTurnSteerPrefix + "\nfocus on tests", CreatedAt: 1_700_000_000_100},
		{Role: provider.RoleUser, Content: "<compaction-summary>\nfolded", CreatedAt: 1_700_000_000_101},
		historySliceUser(1, "real question 2"),
		historySliceAssistant(1, "answer 2"),
	}
	sess, path := saveHistorySliceSession(t, dir, "classify.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	page := app.HistorySliceForTab("test", HistorySliceRequest{})
	if page.TotalTurns != 2 {
		t.Fatalf("TotalTurns = %d, want 2 (steer and synthetic excluded)", page.TotalTurns)
	}
	assertPagesMatchReference(t, []HistorySlice{page}, referenceHistoryRows(t, dir, path))
	foundSteerNotice := false
	for _, e := range page.Entries {
		if e.Message.Role == "notice" && strings.HasPrefix(e.Message.Content, "↪ ") {
			foundSteerNotice = true
		}
		if e.Turn > 2 {
			t.Fatalf("entry turn = %d, want <= 2", e.Turn)
		}
	}
	if !foundSteerNotice {
		t.Fatal("steer should surface as a ↪ notice row")
	}
}

func TestHistorySliceImagesAndUnicode(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "看看这张截图 🖼️", Images: []string{"data:image/png;base64,iVBORw0KGgo="}, CreatedAt: 1_700_000_000_000},
		historySliceAssistant(0, "图中是……界面，🙂 已识别。"),
		historySliceUser(1, "第二个问题：中文与 emoji 👍 混排"),
		historySliceAssistant(1, "回答：混排正常。"),
	}
	sess, path := saveHistorySliceSession(t, dir, "unicode.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	pages := collectHistorySlicePages(t, app, "test", HistorySliceRequest{Turns: 1, Entries: 2})
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
	if pages[len(pages)-1].TotalTurns != 2 {
		t.Fatalf("TotalTurns = %d, want 2", pages[len(pages)-1].TotalTurns)
	}
}

// --- large shapes -----------------------------------------------------------

func TestHistorySliceToolHeavy3255(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	var msgs []provider.Message
	for i := range 465 { // 465 × 7 = 3255 messages
		msgs = append(msgs, historySliceToolTurn(i)...)
	}
	sess, path := saveHistorySliceSession(t, dir, "tool-heavy.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	pages := collectHistorySlicePages(t, app, "test", HistorySliceRequest{Turns: 12, Entries: 120, Bytes: 512 << 10})
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
}

func TestHistorySlice46Turn625MessagesCold(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	// 1 system + 45 turns × 10 messages + one 174-message turn = 625.
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}
	for i := range 45 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)))
		for j := range 4 {
			id := fmt.Sprintf("call-%d-%d", i, j)
			msgs = append(msgs,
				provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "bash", Arguments: `{"command":"x"}`}}, CreatedAt: 1_700_000_000_000 + int64(i)},
				provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "bash", Content: "ok", CreatedAt: 1_700_000_000_000 + int64(i)},
			)
		}
		msgs = append(msgs, historySliceAssistant(i, fmt.Sprintf("a%d", i)))
	}
	last := []provider.Message{historySliceUser(45, "q45")}
	for j := range 86 {
		id := fmt.Sprintf("call-45-%d", j)
		last = append(last,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "bash", Arguments: `{"command":"y"}`}}, CreatedAt: 1_700_000_000_045},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "bash", Content: "ok", CreatedAt: 1_700_000_000_045},
		)
	}
	last = append(last, historySliceAssistant(45, "a45")) // 174 messages
	msgs = append(msgs, last...)
	if len(msgs) != 625 {
		t.Fatalf("fixture = %d messages, want 625", len(msgs))
	}
	_, path := saveHistorySliceSession(t, dir, "46-turns.jsonl", msgs)
	tab.SessionPath = path

	pages := collectHistorySlicePages(t, app, "cold", HistorySliceRequest{Turns: 7, Entries: 90})
	if pages[0].TotalTurns != 46 {
		t.Fatalf("TotalTurns = %d, want 46", pages[0].TotalTurns)
	}
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
}

// --- JSON contract ----------------------------------------------------------

func TestHistorySliceArraysNeverNull(t *testing.T) {
	encoded, err := json.Marshal(HistorySlice{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"entries":[]`) {
		t.Fatalf("zero HistorySlice must encode entries as []: %s", encoded)
	}
	encoded, err = json.Marshal(staleHistorySlice(3, true, "digest"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"entries":[]`) {
		t.Fatalf("stale HistorySlice must encode entries as []: %s", encoded)
	}
	encoded, err = json.Marshal(HistoryEntry{Refs: []HistoryContentRef{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"refs":[]`) {
		t.Fatalf("HistoryEntry must encode refs as []: %s", encoded)
	}
}

// --- chunk helpers ----------------------------------------------------------

func TestHistoryContentChunksRuneAligned(t *testing.T) {
	if got := historyContentChunkCount(""); got != 1 {
		t.Fatalf("empty string chunks = %d, want 1", got)
	}
	if data, chunks := historyContentChunkAt("", 0); data != "" || chunks != 1 {
		t.Fatalf("empty chunk = %q/%d", data, chunks)
	}
	small := "hello 世界"
	if got := historyContentChunkCount(small); got != 1 {
		t.Fatalf("small string chunks = %d, want 1", got)
	}
	// Build a string whose 256KiB boundary falls inside a 4-byte rune.
	unit := "🙂" // 4 bytes
	big := strings.Repeat(unit, (historyContentChunkBytes/4)+10)
	if chunks := historyContentChunkCount(big); chunks != 2 {
		t.Fatalf("chunks = %d, want 2", chunks)
	}
	first, _ := historyContentChunkAt(big, 0)
	second, _ := historyContentChunkAt(big, 1)
	if !utf8.ValidString(first) || !utf8.ValidString(second) {
		t.Fatal("chunks are not valid UTF-8")
	}
	if first+second != big {
		t.Fatal("chunk split lost content")
	}
	if len(first) > historyContentChunkBytes {
		t.Fatalf("first chunk = %d bytes, over budget", len(first))
	}
	// Out-of-range chunk index returns empty data with the total count.
	if data, chunks := historyContentChunkAt(big, 5); data != "" || chunks != 2 {
		t.Fatalf("out-of-range chunk = %q/%d", data, chunks)
	}
}

// --- concurrency ------------------------------------------------------------

func TestHistorySliceConcurrentReadsDuringSave(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	var msgs []provider.Message
	for i := range 30 {
		msgs = append(msgs, historySliceToolTurn(i)...)
	}
	sess, path := saveHistorySliceSession(t, dir, "race.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	const readers = 4
	start := make(chan struct{})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for r := range readers {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			<-start
			cursor := ""
			for {
				select {
				case <-stop:
					return
				default:
				}
				page := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 3, Entries: 25, Cursor: cursor})
				if page.Entries == nil {
					errs <- fmt.Errorf("reader %d: nil entries", r)
					return
				}
				if page.Stale {
					// A save landed between pages: restart from latest, as the
					// frontend would.
					cursor = ""
					continue
				}
				if !page.HasOlder {
					cursor = ""
					continue
				}
				cursor = page.NextCursor
			}
		}(r)
	}

	// Writer: append + save in a loop while readers page.
	close(start)
	for i := 30; i < 38; i++ {
		sess.Add(historySliceUser(i, fmt.Sprintf("q%d", i)))
		sess.Add(historySliceAssistant(i, fmt.Sprintf("a%d", i)))
		if err := sess.Save(path); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("save: %v", err)
		}
	}
	close(stop)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// The final state must page cleanly end to end.
	pages := collectHistorySlicePages(t, app, "test", HistorySliceRequest{Turns: 5, Entries: 40})
	assertPagesMatchReference(t, pages, referenceHistoryRows(t, dir, path))
}
