package control

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestSaveImageDataURL(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := SaveImageDataURL("data:image/png;base64," + tinyPNG)
	if err != nil {
		t.Fatalf("SaveImageDataURL: %v", err)
	}
	if !strings.HasPrefix(got, ".reasonix/attachments/clipboard-") || !strings.HasSuffix(got, ".png") {
		t.Fatalf("path = %q, want attachment png path", got)
	}
}

func TestSaveImageDataURLRejectsSpoofedMime(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := SaveImageDataURL("data:image/png;base64,aGk="); err == nil {
		t.Fatal("spoofed image mime should fail")
	}
}

func TestCreateAttachmentFileSkipsExistingPath(t *testing.T) {
	t.Chdir(t.TempDir())
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
	t.Chdir(t.TempDir())
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

func TestSaveImageFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("source.png", mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SaveImageFile("source.png")
	if err != nil {
		t.Fatalf("SaveImageFile: %v", err)
	}
	if !strings.HasPrefix(got, ".reasonix/attachments/clipboard-") || !strings.HasSuffix(got, ".png") {
		t.Fatalf("path = %q, want attachment png path", got)
	}
}

func TestSaveAttachmentFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("notes.pdf", []byte("%PDF-1.4 body"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SaveAttachmentFile("notes.pdf")
	if err != nil {
		t.Fatalf("SaveAttachmentFile: %v", err)
	}
	if !strings.HasPrefix(got, ".reasonix/attachments/clipboard-") || !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("path = %q, want attachment pdf path", got)
	}
	if data, err := os.ReadFile(got); err != nil || string(data) != "%PDF-1.4 body" {
		t.Fatalf("stored bytes = %q (err %v), want original", data, err)
	}
}

func TestSaveAttachmentFileRejectsEmptyAndDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("empty.txt", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAttachmentFile("empty.txt"); err == nil {
		t.Fatal("empty file should fail")
	}
	if err := os.Mkdir("adir", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAttachmentFile("adir"); err == nil {
		t.Fatal("directory should fail")
	}
}

func TestSaveAttachmentFileSanitizesExtension(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("payload.weird-ext-here", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SaveAttachmentFile("payload.weird-ext-here")
	if err != nil {
		t.Fatalf("SaveAttachmentFile: %v", err)
	}
	if !strings.HasSuffix(got, ".bin") {
		t.Fatalf("path = %q, want .bin fallback for unsafe extension", got)
	}
}

func TestSaveAttachmentFileRejectsSymlink(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("source.bin", []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("source.bin", "link.bin"); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := SaveAttachmentFile("link.bin"); err == nil {
		t.Fatal("symlink attachment path should fail")
	}
}

func TestSaveImageFileRejectsSymlink(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("source.png", mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("source.png", "link.png"); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := SaveImageFile("link.png"); err == nil {
		t.Fatal("symlink image path should fail")
	}
}

func TestImageDataURLRejectsOutsideAttachmentDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("x.png", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImageDataURL("x.png"); err == nil {
		t.Fatal("outside attachment dir should fail")
	}
	if _, err := ImageDataURL("../.reasonix/attachments/x.png"); err == nil {
		t.Fatal("traversal path should fail")
	}
}

func TestImageDataURLInRootIgnoresProcessWorkingDirectory(t *testing.T) {
	workspace := t.TempDir()
	processDir := t.TempDir()
	path, err := SaveImageBytesInRoot(workspace, "image/png", mustBase64(t, tinyPNG))
	if err != nil {
		t.Fatalf("SaveImageBytesInRoot: %v", err)
	}
	t.Chdir(processDir)
	got, err := ImageDataURLInRoot(workspace, path)
	if err != nil {
		t.Fatalf("ImageDataURLInRoot: %v", err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("data URL = %q, want png", got)
	}
	if _, err := os.Stat(filepath.Join(processDir, ".reasonix")); !os.IsNotExist(err) {
		t.Fatalf("read created process-local attachment state: %v", err)
	}
}

func TestAttachmentRootAPIsRejectManagedDirectorySymlinks(t *testing.T) {
	for _, component := range []string{".reasonix", "attachments"} {
		t.Run(component, func(t *testing.T) {
			workspace := t.TempDir()
			outside := t.TempDir()
			var link string
			if component == ".reasonix" {
				if err := os.MkdirAll(filepath.Join(outside, "attachments"), 0o755); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(workspace, ".reasonix")
			} else {
				if err := os.MkdirAll(filepath.Join(workspace, ".reasonix"), 0o755); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(workspace, ".reasonix", "attachments")
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			if _, err := ImageDataURLInRoot(workspace, ".reasonix/attachments/image.png"); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("read through managed directory symlink = %v, want symlink rejection", err)
			}
			if _, err := SaveImageBytesInRoot(workspace, "image/png", mustBase64(t, tinyPNG)); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("write through managed directory symlink = %v, want symlink rejection", err)
			}
		})
	}
}

func TestImageDataURLInRootKeepsSameRelativeNameIsolated(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	rel := filepath.Join(".reasonix", "attachments", "same.png")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	first := mustBase64(t, tinyPNG)
	second := append([]byte(nil), first...)
	second[len(second)-1] ^= 1
	if err := os.WriteFile(filepath.Join(firstRoot, rel), first, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondRoot, rel), second, 0o644); err != nil {
		t.Fatal(err)
	}
	one, err := ImageDataURLInRoot(firstRoot, filepath.ToSlash(rel))
	if err != nil {
		t.Fatal(err)
	}
	two, err := ImageDataURLInRoot(secondRoot, filepath.ToSlash(rel))
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Fatal("same attachment name in two workspaces resolved to the same bytes")
	}
}

func TestImageDataURLInRootDoesNotCreateMissingAttachmentDirectory(t *testing.T) {
	workspace := t.TempDir()
	_, err := ImageDataURLInRoot(workspace, ".reasonix/attachments/missing.png")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".reasonix")); !os.IsNotExist(err) {
		t.Fatalf("read created attachment directory: %v", err)
	}
}

func TestImageDataURLRejectsSymlinkFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := ensureAttachmentRoot(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("secret.png", []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(".reasonix", "attachments", "link.png")
	if err := os.Symlink(filepath.Join("..", "..", "secret.png"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := ImageDataURL(link); err == nil {
		t.Fatal("symlink attachment file should fail")
	}
}

func TestImageDataURLRejectsSymlinkAttachmentDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".reasonix", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("elsewhere", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../elsewhere", filepath.Join(".reasonix", "attachments")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := ImageDataURL(".reasonix/attachments/x.png"); err == nil {
		t.Fatal("symlink attachment directory should fail")
	}
}

func TestImageDataURLRejectsSymlinkSubdirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := ensureAttachmentRoot(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("outside", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("outside", "x.png"), mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(".reasonix", "attachments", "link")
	if err := os.Symlink(filepath.Join("..", "..", "outside"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := ImageDataURL(filepath.Join(link, "x.png")); err == nil {
		t.Fatal("symlink attachment subdirectory should fail")
	}
}

func TestValidateAttachmentInRootRejectsMissingAndSymlink(t *testing.T) {
	root := t.TempDir()
	rel, err := SaveAttachmentBytesInRoot(root, "notes.txt", []byte("saved"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttachmentInRoot(root, rel); err != nil {
		t.Fatalf("valid attachment: %v", err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttachmentInRoot(root, rel); err == nil {
		t.Fatal("missing attachment should fail validation")
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, ".reasonix", "attachments", "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := ValidateAttachmentInRoot(root, filepath.ToSlash(filepath.Join(".reasonix", "attachments", "link.txt"))); err == nil {
		t.Fatal("symlink attachment should fail validation")
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

func stubClipboardTools(t *testing.T, look func(string) (string, error), run func(string, ...string) ([]byte, []byte, error)) {
	t.Helper()
	previousLook := lookClipboardTool
	previousRun := runClipboardTool
	t.Cleanup(func() {
		lookClipboardTool = previousLook
		runClipboardTool = previousRun
	})
	lookClipboardTool = look
	runClipboardTool = run
}

func TestSaveLinuxClipboardImageSeparatesNoImageFromMissingTools(t *testing.T) {
	t.Chdir(t.TempDir())
	stubClipboardTools(t,
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(string, ...string) ([]byte, []byte, error) {
			t.Fatal("missing tools must not run")
			return nil, nil, nil
		},
	)
	_, err := saveLinuxClipboardImage()
	if err == nil || errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("missing tools reported as an empty clipboard: %v", err)
	}
	if !strings.Contains(err.Error(), "needs wl-paste") {
		t.Fatalf("missing tools lost their actionable message: %v", err)
	}

	stubClipboardTools(t,
		func(name string) (string, error) {
			if name == "wl-paste" {
				return name, nil
			}
			return "", exec.ErrNotFound
		},
		func(_ string, args ...string) ([]byte, []byte, error) {
			if len(args) != 1 || args[0] != "--list-types" {
				t.Fatalf("text clipboard unexpectedly read as an image: %v", args)
			}
			return []byte("text/plain\nUTF8_STRING\n"), nil, nil
		},
	)
	if _, err := saveLinuxClipboardImage(); !errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("text-only clipboard reported as a broken setup: %v", err)
	}
}

func TestSaveLinuxClipboardImagePreservesProbeFailure(t *testing.T) {
	stubClipboardTools(t,
		func(name string) (string, error) {
			if name == "wl-paste" {
				return name, nil
			}
			return "", exec.ErrNotFound
		},
		func(string, ...string) ([]byte, []byte, error) {
			return nil, []byte("failed to connect to display"), errors.New("display unavailable")
		},
	)

	_, err := saveLinuxClipboardImage()
	if err == nil || errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("clipboard probe failure reported as no image: %v", err)
	}
	if !strings.Contains(err.Error(), "probe wl-paste clipboard types") {
		t.Fatalf("clipboard probe failure lost its operation: %v", err)
	}
}

func TestSaveLinuxClipboardImagePreservesImageReadFailure(t *testing.T) {
	stubClipboardTools(t,
		func(name string) (string, error) {
			if name == "wl-paste" {
				return name, nil
			}
			return "", exec.ErrNotFound
		},
		func(_ string, args ...string) ([]byte, []byte, error) {
			if len(args) == 1 && args[0] == "--list-types" {
				return []byte("text/plain\nimage/png\n"), nil, nil
			}
			return nil, nil, errors.New("selection changed")
		},
	)

	_, err := saveLinuxClipboardImage()
	if err == nil || errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("clipboard image read failure reported as no image: %v", err)
	}
	if !strings.Contains(err.Error(), "read clipboard image with wl-paste") {
		t.Fatalf("clipboard image read failure lost its operation: %v", err)
	}
}

func TestSaveLinuxClipboardImageTreatsEmptySelectionAsNoImage(t *testing.T) {
	stubClipboardTools(t,
		func(name string) (string, error) {
			if name == "wl-paste" {
				return name, nil
			}
			return "", exec.ErrNotFound
		},
		func(string, ...string) ([]byte, []byte, error) {
			return nil, []byte("Nothing is copied\n"), errors.New("exit status 1")
		},
	)

	if _, err := saveLinuxClipboardImage(); !errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("empty clipboard = %v, want ErrNoClipboardImage", err)
	}
}

func TestSaveDarwinClipboardImagePreservesOperationalFailure(t *testing.T) {
	want := errors.New("attachment directory unavailable")
	_, err := saveDarwinClipboardImageWith(func(string) (string, error) {
		return "", want
	})
	if !errors.Is(err, want) {
		t.Fatalf("darwin clipboard operational failure = %v, want %v", err, want)
	}
}

func TestSaveDarwinClipboardImageReturnsNoImageOnlyAfterBothTypesMiss(t *testing.T) {
	var classes []string
	_, err := saveDarwinClipboardImageWith(func(class string) (string, error) {
		classes = append(classes, class)
		return "", ErrNoClipboardImage
	})
	if !errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("darwin empty clipboard = %v, want ErrNoClipboardImage", err)
	}
	if got, want := strings.Join(classes, ","), "PNGf,JPEG"; got != want {
		t.Fatalf("darwin clipboard classes = %q, want %q", got, want)
	}
}

func TestClassifyDarwinClipboardResultDistinguishesMissingTypeFromFailure(t *testing.T) {
	const marker = "__NO_IMAGE__"
	if err := classifyDarwinClipboardResult([]byte(marker+"\n"), nil, marker); !errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("darwin no-image marker = %v, want ErrNoClipboardImage", err)
	}

	want := errors.New("osascript failed")
	err := classifyDarwinClipboardResult([]byte("clipboard service unavailable\n"), want, marker)
	if !errors.Is(err, want) || errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("darwin operational failure = %v, want wrapped %v", err, want)
	}
}

func TestSaveLinuxClipboardImageNegotiatesSupportedImageType(t *testing.T) {
	t.Chdir(t.TempDir())
	png, err := base64.StdEncoding.DecodeString(tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	var readArgs []string
	stubClipboardTools(t,
		func(name string) (string, error) {
			if name == "wl-paste" {
				return name, nil
			}
			return "", exec.ErrNotFound
		},
		func(_ string, args ...string) ([]byte, []byte, error) {
			if len(args) == 1 && args[0] == "--list-types" {
				return []byte("text/plain\nimage/jpeg\n"), nil, nil
			}
			readArgs = args
			return png, nil, nil
		},
	)
	if _, err := saveLinuxClipboardImage(); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(readArgs, " "), "--type image/jpeg --no-newline"; got != want {
		t.Fatalf("read args = %q, want %q", got, want)
	}
}

func TestSaveLinuxClipboardImageNamesUnsupportedImageTypes(t *testing.T) {
	stubClipboardTools(t,
		func(name string) (string, error) {
			if name == "wl-paste" {
				return name, nil
			}
			return "", exec.ErrNotFound
		},
		func(_ string, args ...string) ([]byte, []byte, error) {
			if len(args) == 1 && args[0] == "--list-types" {
				return []byte("text/plain\nimage/bmp\nimage/\x1b]52;c;owned\a\n"), nil, nil
			}
			t.Fatalf("unsupported image types must not be read: %v", args)
			return nil, nil, nil
		},
	)
	_, err := saveLinuxClipboardImage()
	if !errors.Is(err, ErrNoClipboardImage) {
		t.Fatalf("unsupported image error = %v, want ErrNoClipboardImage fallback", err)
	}
	if !errors.Is(err, ErrUnsupportedClipboardImage) {
		t.Fatalf("unsupported image error lost its diagnostic type: %v", err)
	}
	if !strings.Contains(err.Error(), "image/bmp") || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error should name the unsupported image type: %v", err)
	}
	if strings.ContainsAny(err.Error(), "\x1b\a") {
		t.Fatalf("error contains clipboard-provided terminal controls: %q", err)
	}
}
