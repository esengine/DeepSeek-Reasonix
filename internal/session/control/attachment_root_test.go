package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// writeAttachment stages a decodable PNG: an attachment whose dimensions cannot
// be read is refused before the wire, so a header-only fixture tests nothing.
func writeAttachment(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, ".reasonix", "attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(filepath.Join(".reasonix", "attachments", name))
}

// The desktop window is launched from Finder, so its process directory is "/"
// while the attachment sits under the workspace. Resolving the reference
// against the process directory made every pasted image read as missing — the
// turn then carried no image candidates, the vision role was never consulted,
// and the model was left with the prompt's suggestion to OCR the path itself.
func TestAttachmentResolvesAgainstWorkspaceNotProcessDir(t *testing.T) {
	workspace := testenv.TempDir(t)
	rel := writeAttachment(t, workspace, "pasted.png")

	elsewhere := testenv.TempDir(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(elsewhere); err != nil {
		t.Fatal(err)
	}

	if _, err := visionImageDataURL(workspace, rel); err != nil {
		t.Fatalf("attachment under the workspace must resolve from any process dir: %v", err)
	}
	// The reference itself still may not escape .reasonix/attachments.
	if _, err := visionImageDataURL(workspace, "../secrets.png"); err == nil {
		t.Fatal("a path outside .reasonix/attachments must still be refused")
	}
	if _, err := visionImageDataURL(workspace, filepath.Join(workspace, "abs.png")); err == nil {
		t.Fatal("an absolute path must still be refused")
	}
}

// A turn whose attachment resolves has image candidates, which is what makes
// the routing note appear and hands the bytes to the vision role.
func TestTurnCarriesImageCandidatesForWorkspaceAttachment(t *testing.T) {
	workspace := testenv.TempDir(t)
	rel := writeAttachment(t, workspace, "shot.png")

	c := New(Options{WorkspaceRoot: workspace})
	defer c.Close()
	candidates, skipped := c.resolveInputImageCandidates("@" + rel + " 这是什么？")
	if len(candidates) != 1 || len(skipped) != 0 {
		t.Fatalf("resolved %d image candidates (%d skipped), want the attachment", len(candidates), len(skipped))
	}
	if (&turnImages{candidates: candidates}).unreadable() == 0 {
		t.Fatal("a text-only model must be told the turn carries an image it cannot read")
	}
}

// The reference block must not propose a route of its own. Two blocks giving
// different advice is exactly how the model came to write an OCR script while
// the configured vision model went unused.
func TestImageReferenceBlockProposesNothing(t *testing.T) {
	note := imageFileRefNote("shot.png", "image/png", 1234, true)
	for _, banned := range []string{"OCR", "vision tool", "can still use"} {
		if strings.Contains(note, banned) {
			t.Fatalf("reference block still proposes a route (%q): %s", banned, note)
		}
	}
	if !strings.Contains(note, "attached-images") {
		t.Fatalf("reference block must point at the one note that decides: %s", note)
	}
}

// With a vision model configured the note names it and forbids self-OCR;
// without one it says so instead of promising a delegate that cannot read.
func TestRoutingNoteDependsOnConfiguredVisionModel(t *testing.T) {
	c := New(Options{WorkspaceRoot: testenv.TempDir(t)})
	defer c.Close()
	bare := c.imageRoutingNote(1)
	if !strings.Contains(bare, "no vision model is configured") {
		t.Fatalf("with no vision model the note must say so: %s", bare)
	}
	if strings.Contains(bare, "Do not OCR") {
		t.Fatalf("with no vision model, OCR is the only way left: %s", bare)
	}
}
