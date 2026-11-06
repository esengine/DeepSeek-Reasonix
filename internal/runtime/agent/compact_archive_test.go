package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func archiveAgent(t *testing.T, sess *sessionstore.Session, sink event.Sink) (*Agent, *countingProvider, string) {
	t.Helper()
	prov := &countingProvider{reply: "## Standing facts\n- a summarizer was asked"}
	dir := testenv.TempDir(t)
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 200_000, CompactRatio: 0.8, SessionPath: filepath.Join(dir, "session.jsonl"),
		WorkspaceID: "ws", ModelRef: "p/m",
		ArchiveDir: testenv.TempDir(t),
	}, sink)
	return a, prov, dir
}

// A fold is archived, not summarized: no summarizer request is made, the
// folded span lands verbatim in a file beside the transcript, and what stays in
// context is a pointer to it.
func TestAnArchivedFoldMakesNoSummarizerCallAndPointsAtTheFile(t *testing.T) {
	sess := foldableSessionOverForce(60)
	for i := range sess.Messages {
		if sess.Messages[i].Role == provider.RoleAssistant {
			sess.Messages[i].Content = "decided: keep the public API stable"
		}
	}
	var done event.Compaction
	a, prov, dir := archiveAgent(t, sess, event.FuncSink(func(e event.Event) {
		if e.Kind == event.CompactionDone {
			done = e.Compaction
		}
	}))

	verdict, err := a.CompactNow(context.Background(), CompactRequest{Instructions: "the API contract"})
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if !verdict.Compacted() {
		t.Fatalf("verdict = %+v, want a fold", verdict)
	}
	if len(prov.got) != 0 {
		t.Fatalf("an archived fold made %d summarizer request(s)", len(prov.got))
	}

	pointer := done.Summary
	if !strings.Contains(pointer, "archived, not summarized") || !strings.Contains(pointer, "read_file") {
		t.Fatalf("the pointer does not tell the model to read the archive:\n%s", pointer)
	}
	if !strings.Contains(pointer, "the API contract") {
		t.Fatalf("the pointer dropped the requested focus:\n%s", pointer)
	}

	files, err := filepath.Glob(filepath.Join(dir, "session.outputs", "fold-*.md"))
	if err != nil || len(files) != 1 {
		t.Fatalf("archive files = %v (%v), want exactly one beside the transcript", files, err)
	}
	if !strings.Contains(pointer, files[0]) {
		t.Fatalf("the pointer does not name %s:\n%s", files[0], pointer)
	}
	body, err := os.ReadFile(files[0])
	if err != nil || !strings.Contains(string(body), "decided: keep the public API stable") {
		t.Fatalf("the archive does not hold the folded work verbatim (%v)", err)
	}
	visible := a.window().modelVisibleMessages()
	if len(visible) >= len(sess.Messages) {
		t.Fatalf("nothing left the context: %d visible of %d", len(visible), len(sess.Messages))
	}
	for _, m := range visible {
		if m.Role == provider.RoleSystem && strings.Contains(m.Content, "a summarizer was asked") {
			t.Fatal("a summarizer digest reached the context")
		}
	}
}

// What the host keeps for display only never reaches the model, so it never
// reaches the archive the model reads back either.
func TestAnArchiveNeverHoldsLocalOnlyOutput(t *testing.T) {
	sess := foldableSessionOverForce(60)
	local := provider.Message{Role: provider.RoleAssistant, Content: "local-only output that must stay out", LocalOnly: true}
	sess.Messages = append(sess.Messages[:20], append([]provider.Message{local}, sess.Messages[20:]...)...)
	a, _, _ := archiveAgent(t, sess, event.Discard)

	if _, err := a.CompactNow(context.Background(), CompactRequest{}); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(a.window().outputsDir(), "fold-*.md"))
	if len(files) == 0 {
		t.Fatal("no archive written")
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil || strings.Contains(string(body), "local-only output that must stay out") {
			t.Fatalf("the archive %s holds display-only output (%v)", f, err)
		}
	}
}

// Nothing of the conversation is held back: after a fold between turns the
// context is the system prompt and the one pointer, the first user turn, the
// recent turns and the user's own words included, and the archive has them all.
func TestAFoldRetainsNothingButThePointer(t *testing.T) {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "FIRST-TURN codeword blue-whale"},
		{Role: provider.RoleAssistant, Content: "ok " + strings.Repeat("filler ", 200)},
		{Role: provider.RoleUser, Content: "SECOND-TURN deadline friday"},
		{Role: provider.RoleAssistant, Content: "noted " + strings.Repeat("filler ", 200)},
		{Role: provider.RoleUser, Content: "LAST-TURN owner lin"},
		{Role: provider.RoleAssistant, Content: "done " + strings.Repeat("filler ", 200)},
	}}
	a, _, dir := archiveAgent(t, sess, event.Discard)
	verdict, err := a.CompactNow(context.Background(), CompactRequest{})
	if err != nil || !verdict.Compacted() {
		t.Fatalf("CompactNow = %+v, %v; want a fold", verdict, err)
	}
	visible := a.window().modelVisibleMessages()
	if len(visible) != 2 || visible[0].Role != provider.RoleSystem || !isCompactionSummary(visible[1]) {
		t.Fatalf("visible = %+v, want the system prompt and one pointer", visible)
	}
	for _, word := range []string{"blue-whale", "friday", "owner lin"} {
		if strings.Contains(visible[1].Content, word) {
			t.Fatalf("the pointer carries %q; it must only point", word)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "session.outputs", "fold-*.md"))
	if len(files) != 1 {
		t.Fatalf("archive files = %v", files)
	}
	body, _ := os.ReadFile(files[0])
	for _, word := range []string{"blue-whale", "friday", "owner lin"} {
		if !strings.Contains(string(body), word) {
			t.Fatalf("the archive lost %q", word)
		}
	}
}
