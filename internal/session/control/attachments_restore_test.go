package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// A copy names stored attachments workspace-relative; restoring resolves them
// against this workspace and nothing else.
func TestRestoreStoredAttachmentsAnswersOnePerPath(t *testing.T) {
	workspace := testenv.TempDir(t)
	kept := writeAttachment(t, workspace, "clipboard-20260101-000000.000000-000001.png")
	missing := ".reasonix/attachments/clipboard-20260101-000000.000000-000009.png"

	got := RestoreStoredAttachments(workspace, []string{kept, missing})
	if len(got) != 2 {
		t.Fatalf("answers = %+v, want one per path", got)
	}
	if got[0].Ref != "@"+kept || !got[0].Image || got[0].Error != "" {
		t.Fatalf("restored = %+v, want the token and that it is a picture", got[0])
	}
	if got[1].Ref != "" || got[1].Error == "" {
		t.Fatalf("missing attachment = %+v, want a refusal instead of a token", got[1])
	}
}

// A restore is not a way to read the rest of the disk: only the host's own
// attachment directory resolves, with or without the '@' a token carries.
func TestRestoreStoredAttachmentsRefusesEverythingElse(t *testing.T) {
	workspace := testenv.TempDir(t)
	writeAttachment(t, workspace, "clipboard-20260101-000000.000000-000001.png")
	if err := os.WriteFile(filepath.Join(workspace, "notes.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"notes.md",
		"../outside.png",
		".reasonix/attachments/../../../outside.png",
		".reasonix/attachments",
		filepath.ToSlash(filepath.Join(workspace, ".reasonix", "attachments", "clipboard-20260101-000000.000000-000001.png")),
	} {
		got := RestoreStoredAttachments(workspace, []string{path})
		if len(got) != 1 || got[0].Ref != "" || got[0].Error == "" {
			t.Fatalf("%q restored as %+v, want a refusal", path, got)
		}
	}
}

// A path written as a token, with the leading '@' and the './' a line may carry,
// is the same attachment.
func TestRestoreStoredAttachmentsAcceptsTheSpellingsACopyRecords(t *testing.T) {
	workspace := testenv.TempDir(t)
	kept := writeAttachment(t, workspace, "clipboard-20260101-000000.000000-000001.png")
	for _, path := range []string{kept, "@" + kept, "./" + kept} {
		got := RestoreStoredAttachments(workspace, []string{path})
		if len(got) != 1 || got[0].Ref != "@"+kept {
			t.Fatalf("%q restored as %+v", path, got)
		}
	}
}
