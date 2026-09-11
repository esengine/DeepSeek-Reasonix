package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Covers the note a resolved @-reference emits per input kind, and both image
// note variants. The capability is pinned because a zero Controller resolves it
// from the ambient config, which would otherwise make the expected note depend
// on the machine running the test.
func TestResolveRefsAttachmentKinds(t *testing.T) {
	temp := t.TempDir()
	attachmentsDir := filepath.Join(temp, ".reasonix", "attachments")
	if err := os.MkdirAll(attachmentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ymlRef := filepath.ToSlash(".reasonix/attachments/config.yml")
	zipRef := filepath.ToSlash(".reasonix/attachments/archive.zip")
	pngRef := filepath.ToSlash(".reasonix/attachments/shot.png")
	if err := os.WriteFile(filepath.Join(temp, filepath.FromSlash(ymlRef)), []byte("name: reasonix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, filepath.FromSlash(zipRef)), []byte{'P', 'K', 0x03, 0x04, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, filepath.FromSlash(pngRef)), []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(temp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldCwd); err != nil {
			t.Error(err)
		}
	})

	line := "check @" + ymlRef + " @" + zipRef + " @" + pngRef
	for _, tc := range []struct {
		name   string
		vision bool
		note   string
	}{
		{name: "text_only", note: "image bytes are not inlined"},
		{name: "vision", vision: true, note: "attached as visual input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vision := tc.vision
			block, errs := (&Controller{frozenImageInput: &vision}).ResolveRefs(context.Background(), line)
			if len(errs) != 0 {
				t.Fatalf("ResolveRefs errors = %v", errs)
			}
			if !strings.Contains(block, `<file path="`+ymlRef+`">`) || !strings.Contains(block, "name: reasonix") {
				t.Fatalf("expected yml attachment to resolve as file content, got: %s", block)
			}
			if !strings.Contains(block, `<file path="`+zipRef+`">`) || !strings.Contains(block, "[binary file "+zipRef) {
				t.Fatalf("expected zip attachment to resolve as binary file note, got: %s", block)
			}
			if !strings.Contains(block, `<image path="`+pngRef+`">`) {
				t.Fatalf("expected png attachment to resolve as image block, got: %s", block)
			}
			if !strings.Contains(block, tc.note) {
				t.Fatalf("expected image note %q, got: %s", tc.note, block)
			}
		})
	}
}
