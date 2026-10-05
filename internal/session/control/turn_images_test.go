package control

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/model/visionimage"
)

// A pasted screenshot that reaches a text-only model must not vanish. The data
// is already carried as a sub-agent candidate; what was missing is any sign of
// it — so the model answered as though nothing had been attached.
func TestUnreadableImagesRideTheTurnTail(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	got := c.imageRoutingPrefix(&turnImages{candidates: []string{"data:image/png;base64,AAA", "data:image/png;base64,BBB"}}) + "look at this"
	if !strings.HasSuffix(got, "look at this") {
		t.Fatalf("the user's text must stay last in the turn: %q", got)
	}
	if !strings.Contains(got, "<"+ImageRoutingTag+">") || !strings.Contains(got, "2 image(s)") {
		t.Fatalf("note = %q, want an attached-images block naming the count", got)
	}
	// No vision model is configured here, so promising a delegate that also
	// cannot read would send the model down a path with nothing at the end.
	if !strings.Contains(got, "no vision model is configured") {
		t.Fatalf("note = %q, want it to say no reader is configured", got)
	}
}

// With a reader configured the note names it and closes the door on self-OCR —
// the two together are what stopped the model writing its own OCR script while
// the vision model sat unused.
func TestConfiguredVisionModelIsNamedInTheNote(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[agent]\nvision_model = \"looker/eyes\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Sink: event.Discard, WorkspaceRoot: testenv.TempDir(t)})
	got := c.imageRoutingNote(1)
	if !strings.Contains(got, "read_only_task") {
		t.Fatalf("note = %q, want it to name the delegated read", got)
	}
	if !strings.Contains(got, "looker/eyes") {
		t.Fatalf("note = %q, want it to name the model that reads", got)
	}
	if !strings.Contains(got, "Do not OCR") {
		t.Fatalf("note = %q, want self-OCR ruled out when a reader exists", got)
	}
}

// The reference block defers to the attached-images note in every case, so a
// model that reads images needs the note too. Without it the deferral reads as
// the picture never having arrived, and a model reasoning at length says so.
func TestReadableImagesAreAnnouncedAsPresent(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	imgs := []string{"data:image/png;base64,AAA"}
	got := c.imageRoutingPrefix(&turnImages{userImages: imgs, candidates: imgs, modelReads: true}) + "look"
	if !strings.Contains(got, "<"+ImageRoutingTag+">") || !strings.Contains(got, "1 image(s)") || !strings.Contains(got, "in this message as images") {
		t.Fatalf("note = %q, want the images announced as present in the message", got)
	}
	if strings.Contains(got, "cannot read images") {
		t.Fatalf("a model that reads images was told it cannot: %q", got)
	}
}

// A Goal continuation carries no pixels of its own: a vision parent already has
// them in its history. It is still a model that reads images, and must not be
// told otherwise just because this message is empty of them.
func TestAContinuationOfAVisionModelIsNotToldItCannotRead(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	got := c.imageRoutingPrefix(&turnImages{candidates: []string{"data:image/png;base64,AAA"}, modelReads: true}) + "continue"
	if got != "continue" {
		t.Fatalf("got %q, want no note for a continuation whose model reads the images already in history", got)
	}
}

func TestNoAttachmentsChangeNothing(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	if got := c.imageRoutingPrefix(&turnImages{}) + "plain"; got != "plain" {
		t.Fatalf("got %q, want the input untouched with no attachments", got)
	}
}

// An attachment no host would accept must not leave silently. The rejection
// comes back as a complaint about the image *format*, and the message that
// carried it then fails every later turn in the session — so the reason is said
// here, to the user and to the model, and the bytes never leave the machine.
func TestUnfitImageIsNamedToUserAndModel(t *testing.T) {
	workspace := testenv.TempDir(t)
	writeVisionTestConfig(t, workspace)
	broken := filepath.Join(workspace, "docs", "diagram.png")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	// A PNG signature over bytes that do not decode: the shape a truncated or
	// half-synced attachment arrives in.
	if err := os.WriteFile(broken, append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{controllerDeps: controllerDeps{workspaceRoot: workspace, modelRef: "custom/vision-pro"}}
	images := c.resolveTurnImages("look at @docs/diagram.png")
	if len(images.userImages) != 0 || len(images.candidates) != 0 {
		t.Fatalf("an unfit image must not be carried: %v / %v", images.userImages, images.candidates)
	}
	if len(images.skipped) != 1 {
		t.Fatalf("skipped = %v, want the unfit attachment reported", images.skipped)
	}
	if !errors.Is(images.skipped[0], visionimage.ErrUnfit) {
		t.Fatalf("skipped[0] = %v, want it to carry the ErrUnfit identity", images.skipped[0])
	}
	if !strings.Contains(images.skipped[0].Error(), "docs/diagram.png") {
		t.Fatalf("reason = %q, want it to name which attachment", images.skipped[0])
	}

	note := (&Controller{controllerDeps: controllerDeps{workspaceRoot: workspace, sink: event.Discard}}).unfitImagesNote(images)
	if !strings.Contains(note, "<"+ImageRoutingTag+">") || !strings.Contains(note, "1 image(s)") {
		t.Fatalf("note = %q, want an attached-images block naming the count", note)
	}
	if !strings.Contains(note, "never guess what the image shows") {
		t.Fatalf("note = %q, want guessing ruled out", note)
	}
}

// A reference that was never an image stays silent: detectRefs returns every
// @ref, and reporting the non-images would bury the one that matters.
func TestNonImageRefIsNotReportedAsSkipped(t *testing.T) {
	workspace := testenv.TempDir(t)
	writeVisionTestConfig(t, workspace)
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Controller{controllerDeps: controllerDeps{workspaceRoot: workspace, modelRef: "custom/vision-pro"}}
	if images := c.resolveTurnImages("read @notes.txt"); len(images.skipped) != 0 {
		t.Fatalf("skipped = %v, want silence for a reference that is not an image", images.skipped)
	}
}
