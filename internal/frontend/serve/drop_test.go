package serve

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// A window knows where a dropped file lives, so the turn must reference that
// file — not a copy of it. Copying is what let a session report an edit it had
// made to the copy while the file the user pointed at never changed.
func TestDropAnswersWithAReferenceToTheFileItself(t *testing.T) {
	dir := testenv.TempDir(t)
	inside := filepath.Join(dir, "notes")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(inside, "plan.md")
	if err := os.WriteFile(file, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: dir})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/drop", map[string][]string{"paths": {file}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /drop = %d", resp.StatusCode)
	}
	var got []droppedRef
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Error != "" {
		t.Fatalf("drop answer = %+v, want one reference", got)
	}
	if got[0].Ref != "@notes/plan.md" {
		t.Fatalf("ref = %q, want the workspace path of the file itself", got[0].Ref)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, ".reasonix", "attachments")); err == nil && len(entries) > 0 {
		t.Fatalf("dropping a path copied %d file(s) into the attachment directory", len(entries))
	}
}

// One unreadable path among several must not cost the others their reference:
// a drop is a handful of files at once, and re-dropping the rest is the whole
// interaction again.
func TestDropReportsOnePathWithoutLosingTheRest(t *testing.T) {
	dir := testenv.TempDir(t)
	file := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(file, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: dir})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/drop", map[string][]string{
		"paths": {filepath.Join(dir, "gone.txt"), file},
	})
	var got []droppedRef
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("drop answer = %+v, want one entry per dropped path", got)
	}
	if got[0].Error == "" || got[0].Ref != "" {
		t.Fatalf("missing path answered %+v, want the reason it has no reference", got[0])
	}
	if got[1].Ref != "@kept.txt" {
		t.Fatalf("second ref = %q, want the file that is still there", got[1].Ref)
	}
}

// The clipboard has bytes and no path, and so does a browser tab. Named bytes
// are stored as the kind of file they say they are, so a dropped PDF in a tab
// still reaches the turn as something the parser resolves.
func TestAttachmentStoresNamedBytesThatAreNotAnImage(t *testing.T) {
	dir := testenv.TempDir(t)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: dir})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/attachments", map[string]string{
		"name": "term list.pdf",
		"data": base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 body")),
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /attachments = %d", resp.StatusCode)
	}
	var got struct{ Path, Ref string }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got.Path, ".pdf") {
		t.Fatalf("stored path = %q, want the extension the name declared", got.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(got.Path))); err != nil {
		t.Fatalf("stored attachment is not on disk: %v", err)
	}
}

// The composer picks a thumbnail or a file glyph on this one field, so "not a
// picture" has to arrive as false rather than as nothing: omitempty drops a
// bool at exactly the value that matters, and the page then reads the missing
// field as an image. Decoding into the struct would restore the false and hide
// that, so this reads the raw object.
func TestDropSaysNotAnImageInsteadOfSayingNothing(t *testing.T) {
	dir := testenv.TempDir(t)
	doc := filepath.Join(dir, "design.md")
	if err := os.WriteFile(doc, []byte("# title"), 0o644); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: dir})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/drop", map[string][]string{"paths": {doc}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /drop = %d", resp.StatusCode)
	}
	var raw []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("drop answer = %+v, want one entry", raw)
	}
	image, ok := raw[0]["image"]
	if !ok {
		t.Fatalf("no \"image\" key in %+v: a document reads as a picture when the field is absent", raw[0])
	}
	if image != false {
		t.Fatalf("image = %v for a .md file, want false", image)
	}
}
