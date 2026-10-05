package control

import (
	"encoding/base64"
	"os"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestCreateAttachmentFileSkipsExistingPath(t *testing.T) {
	t.Chdir(testenv.TempDir(t))
	if err := ensureAttachmentRoot(); err != nil {
		t.Fatal(err)
	}

	first := attachmentPath(".png")
	if err := os.WriteFile(first, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	rel, f, err := createAttachmentFile(".png")
	if err != nil {
		t.Fatalf("createAttachmentFile: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if rel == first {
		t.Fatalf("createAttachmentFile reused existing path %q", rel)
	}
	if got, err := os.ReadFile(first); err != nil {
		t.Fatal(err)
	} else if string(got) != "keep" {
		t.Fatalf("existing attachment was overwritten: %q", got)
	}
}

func TestSaveImageBytesUsesUniquePathsWithinSameTimestamp(t *testing.T) {
	t.Chdir(testenv.TempDir(t))
	oldNow := attachmentNow
	attachmentNow = func() time.Time {
		return time.Date(2026, 6, 1, 10, 20, 30, 123456000, time.UTC)
	}
	defer func() {
		attachmentNow = oldNow
	}()

	raw := mustBase64(t, tinyPNG)
	first, err := SaveImageBytes("image/png", raw)
	if err != nil {
		t.Fatalf("first SaveImageBytes: %v", err)
	}
	second, err := SaveImageBytes("image/png", raw)
	if err != nil {
		t.Fatalf("second SaveImageBytes: %v", err)
	}
	if first == second {
		t.Fatalf("paths collided: %q", first)
	}
	for _, path := range []string{first, second} {
		if got, err := os.ReadFile(path); err != nil {
			t.Fatalf("read %s: %v", path, err)
		} else if string(got) != string(raw) {
			t.Fatalf("content for %s changed", path)
		}
	}
}

func mustBase64(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
