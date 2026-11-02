package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func newAttachmentController(t *testing.T, workspace string) *Controller {
	t.Helper()
	dir := testenv.TempDir(t)
	exec := agent.New(&turnIdentityProvider{}, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{
		Runner: exec, Executor: exec, Sink: event.Discard, WorkspaceRoot: workspace,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	t.Cleanup(func() { c.Close(); c.autosaveWG.Wait() })
	return c
}

func TestStoredAttachmentsAreWhatTheHostStored(t *testing.T) {
	workspace := testenv.TempDir(t)
	rel := writeAttachment(t, workspace, "clipboard-20260101-000000.000000-000001.png")
	c := newAttachmentController(t, workspace)

	got := c.StoredAttachments("look @" + rel + " and @.reasonix/attachments/clipboard-missing.png and @README.md")
	if len(got) != 1 || got[0].Path != rel {
		t.Fatalf("attachments = %+v, want only the file the host stored", got)
	}
	if len(c.StoredAttachments("no references here")) != 0 {
		t.Fatal("a message without references carried attachments")
	}
}

// Whatever spelling resolves for the model resolves for the preview, to one
// normalized path, because both read the same detectRefs result.
func TestStoredAttachmentsNormalizeEverySpellingTheModelResolves(t *testing.T) {
	workspace := testenv.TempDir(t)
	rel := writeAttachment(t, workspace, "clipboard-20260101-000000.000000-000001.png")
	spaced := writeAttachment(t, workspace, "with space.png")
	c := newAttachmentController(t, workspace)
	abs := filepath.ToSlash(filepath.Join(workspace, filepath.FromSlash(rel)))

	for name, text := range map[string]string{
		"dot prefix":   "@./" + rel,
		"absolute":     "@" + abs,
		"parent hop":   "@.reasonix/attachments/x/../clipboard-20260101-000000.000000-000001.png",
		"quoted twice": "@" + rel + " again @./" + rel,
	} {
		got := c.StoredAttachments(text)
		if len(got) != 1 || got[0].Path != rel {
			t.Errorf("%s: attachments = %+v, want one %q", name, got, rel)
		}
	}
	got := c.StoredAttachments("@" + EscapeRefPath(spaced))
	if len(got) != 1 || got[0].Path != spaced {
		t.Errorf("escaped space: attachments = %+v, want %q", got, spaced)
	}
}

func TestStoredAttachmentsRefuseWhatTheModelWouldNotSee(t *testing.T) {
	workspace := testenv.TempDir(t)
	rel := writeAttachment(t, workspace, "clipboard-20260101-000000.000000-000001.png")
	outside := filepath.Join(testenv.TempDir(t), "private.png")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, ".reasonix", "attachments", "link.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	c := newAttachmentController(t, workspace)
	for name, text := range map[string]string{
		"symlinked file":  "@.reasonix/attachments/link.png",
		"outside the dir": "@../private.png @" + filepath.ToSlash(outside),
		"not an image":    "@.reasonix/attachments/clipboard-note.txt",
		"lookalike":       "someone@.reasonix/attachments/missing.png",
	} {
		if got := c.StoredAttachments(text); len(got) != 0 {
			t.Errorf("%s: attachments = %+v", name, got)
		}
	}
	_ = rel
}

// A history read is a GET: finding nothing must not create the directory.
func TestStoredAttachmentsNeverCreateTheAttachmentDirectory(t *testing.T) {
	workspace := testenv.TempDir(t)
	c := newAttachmentController(t, workspace)
	if got := c.StoredAttachments("@.reasonix/attachments/clipboard-gone.png"); len(got) != 0 {
		t.Fatalf("attachments = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".reasonix", "attachments")); !os.IsNotExist(err) {
		t.Fatalf("reading the history created the attachment directory: %v", err)
	}
}

func TestTurnStartedCarriesTheStoredAttachments(t *testing.T) {
	workspace := testenv.TempDir(t)
	rel := writeAttachment(t, workspace, "clipboard-20260101-000000.000000-000002.png")
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	exec := agent.New(&turnIdentityProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	sink, done, events := collectSink()
	c := New(Options{
		Runner: exec, Executor: exec, Sink: sink, WorkspaceRoot: workspace,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.Close()
	defer c.autosaveWG.Wait()

	c.Submit("describe @" + rel)
	waitForDone(t, done)
	for _, e := range events() {
		if e.Kind == event.TurnStarted && e.AuthoredTurn != nil {
			if len(e.Attachments) != 1 || e.Attachments[0].Path != rel {
				t.Fatalf("turn_started attachments = %+v", e.Attachments)
			}
			// Another window draws this line, so it must not read the host's
			// token back to the person as text they typed.
			if e.Text != "describe" {
				t.Fatalf("turn_started text = %q, want the words without the token", e.Text)
			}
			return
		}
	}
	t.Fatal("no authored turn_started")
}
