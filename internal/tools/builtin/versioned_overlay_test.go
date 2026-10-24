package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type casOverlay struct {
	fakeOverlay
	version string
	raced   bool
}

func (o *casOverlay) ReadTextFileVersion(_ context.Context, path string) (string, string, bool, error) {
	content, ok := o.files[path]
	return content, o.version, ok, nil
}

func (o *casOverlay) CompareWriteTextFile(_ context.Context, path, version, content string) (bool, error) {
	if o.raced || version != o.version {
		return true, ErrFileChangedSinceSeen
	}
	o.files[path] = content
	return true, nil
}

func TestVersionedOverlayRejectsRaceWithoutDiskFallback(t *testing.T) {
	for _, raced := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "conflict"}[raced], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "file.txt")
			if err := os.WriteFile(path, []byte("disk"), 0600); err != nil {
				t.Fatal(err)
			}
			overlay := &casOverlay{fakeOverlay: fakeOverlay{files: map[string]string{path: "buffer"}}, version: "v1", raced: raced}
			views := NewStrictFileViews()
			views.saw(path, "buffer")
			writer := writeFile{workDir: root, overlay: overlay, views: views}
			_, err := writer.Execute(WithVersionedWrites(t.Context()), json.RawMessage(`{"path":"file.txt","content":"edited"}`))
			if raced && !errors.Is(err, ErrFileChangedSinceSeen) || !raced && err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "disk" {
				t.Fatalf("buffer route wrote disk: %q %v", got, err)
			}
			if !raced && overlay.files[path] != "edited" {
				t.Fatal("CAS result missing")
			}
		})
	}
}

func TestStrictOverlayMissingBufferKeepsDiskRoute(t *testing.T) {
	root := t.TempDir()
	writer := writeFile{workDir: root, overlay: &fakeOverlay{}, views: NewStrictFileViews()}
	_, err := writer.Execute(WithVersionedWrites(t.Context()), json.RawMessage(`{"path":"new.txt","content":"created"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "new.txt")); err != nil || string(got) != "created" {
		t.Fatalf("file %q: %v", got, err)
	}
}

func TestStrictUnversionedUnsavedBufferIsRefused(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "unsaved.txt")
	overlay := &fakeOverlay{files: map[string]string{path: "unsaved"}, writes: map[string]string{}}
	views := NewStrictFileViews()
	views.saw(path, "unsaved")
	writer := writeFile{workDir: root, overlay: overlay, views: views}
	_, err := writer.Execute(WithVersionedWrites(t.Context()), json.RawMessage(`{"path":"unsaved.txt","content":"replacement"}`))
	if !errors.Is(err, ErrOverlayVersionUnavailable) {
		t.Fatalf("write: %v", err)
	}
	if len(overlay.writes) != 0 {
		t.Fatal("unversioned write executed")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsaved buffer fell back to disk: %v", err)
	}
}

func TestStrictUnsavedBufferProbeRequiresReadAuthorization(t *testing.T) {
	root := t.TempDir()
	probe := &writeSourceProbeOverlay{}
	writer := writeFile{workDir: root, forbidRoots: []string{root}, overlay: probe, views: NewStrictFileViews()}
	_, err := writer.Execute(WithVersionedWrites(t.Context()), json.RawMessage(`{"path":"new.txt","content":"replacement"}`))
	if err == nil || probe.reads != 0 || probe.writes != 0 {
		t.Fatalf("protected buffer reached: %+v, %v", probe, err)
	}
}
