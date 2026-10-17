package control

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

type attachmentSecurityReader func([]byte) (int, error)

func (read attachmentSecurityReader) Read(p []byte) (int, error) { return read(p) }

func attachmentStorageDirectoryLink(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("create directory junction: %v: %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatalf("create directory symlink: %v", err)
	}
}

func TestAttachmentStorageRejectsSymlinkedReasonixParent(t *testing.T) {
	attachmentStorageDirectoryLink(t, t.TempDir(), filepath.Join(t.TempDir(), "probe"))
	for _, transport := range []string{"stream", "legacy-bytes"} {
		t.Run(transport, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			attachmentStorageDirectoryLink(t, outside, filepath.Join(root, ".reasonix"))
			var path string
			var err error
			if transport == "stream" {
				path, err = SaveAttachmentStreamInRoot(context.Background(), root, "app.apk", strings.NewReader("apk bytes"))
			} else {
				path, err = SaveAttachmentBytesInRoot(root, "app.apk", []byte("apk bytes"))
			}
			if err == nil || path != "" {
				t.Errorf("symlinked workspace storage accepted: path=%q error=%v", path, err)
			}
			entries, readErr := os.ReadDir(outside)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Errorf("attachment creation escaped the workspace: outside entries=%v", entries)
			}
		})
	}
}

func TestAttachmentStorageCleanupKeepsOriginalDirectoryOwner(t *testing.T) {
	attachmentStorageDirectoryLink(t, t.TempDir(), filepath.Join(t.TempDir(), "probe"))
	for _, failure := range []string{"read-error", "cancel", "stream-limit"} {
		t.Run(failure, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			directory := filepath.Join(root, ".reasonix", "attachments")
			retained := filepath.Join(root, ".reasonix", "retained-attachments")
			cleanupDirectory := retained
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			readFailure := errors.New("attachment source failed")
			wantErr := readFailure
			if failure == "cancel" {
				wantErr = context.Canceled
			}
			const victimContent = "outside file must survive upload cleanup"
			var basename string
			reads := 0
			reader := attachmentSecurityReader(func(p []byte) (int, error) {
				reads++
				if reads == 1 {
					return copy(p, "partially uploaded apk"), nil
				}
				if reads > 2 {
					return 0, io.EOF
				}
				entries, err := os.ReadDir(directory)
				if err != nil {
					t.Fatal(err)
				}
				var partial []os.DirEntry
				for _, entry := range entries {
					if !entry.IsDir() && filepath.Ext(entry.Name()) == ".apk" {
						partial = append(partial, entry)
					}
				}
				if len(partial) != 1 {
					t.Fatalf("expected one opened partial APK, got %v", entries)
				}
				basename = partial[0].Name()
				partialBytes, err := os.ReadFile(filepath.Join(directory, basename))
				if err != nil || string(partialBytes) != "partially uploaded apk" {
					t.Fatalf("partial bytes must be saved before replacement: bytes=%q error=%v", partialBytes, err)
				}
				if err := os.WriteFile(filepath.Join(outside, basename), []byte(victimContent), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(directory, retained); err != nil {
					if runtime.GOOS != "windows" || !errors.Is(err, os.ErrPermission) {
						t.Fatal(err)
					}
					// Windows may prevent replacement while the upload handle is open.
					cleanupDirectory = directory
					t.Log("open upload handle refused directory replacement")
				} else {
					attachmentStorageDirectoryLink(t, outside, directory)
				}
				if failure == "cancel" {
					cancel()
				}
				if failure == "stream-limit" {
					return copy(p, "x"), nil
				}
				return 0, wantErr
			})

			limit := (*config.Config)(nil).AttachmentStreamLimit()
			if failure == "stream-limit" {
				limit = config.AttachmentLimit{Bytes: int64(len("partially uploaded apk")), Source: "user"}
			}
			path, err := SaveAttachmentStreamInRoot(ctx, root, "app.apk", reader, limit)
			matches := errors.Is(err, wantErr)
			if failure == "stream-limit" {
				var limitErr *AttachmentStreamLimitError
				matches = errors.As(err, &limitErr) && limitErr.LimitBytes == limit.Bytes && limitErr.LimitSource == "user"
			}
			if path != "" || !matches {
				t.Errorf("failed upload result: path=%q error=%v; want %v", path, err, wantErr)
			}
			if reads != 2 || basename == "" {
				t.Fatalf("replacement did not run after the file opened: reads=%d name=%q", reads, basename)
			}
			victim, err := os.ReadFile(filepath.Join(outside, basename))
			if err != nil || string(victim) != victimContent {
				t.Errorf("cleanup touched an outside file: bytes=%q error=%v", victim, err)
			}
			if _, err := os.Lstat(filepath.Join(cleanupDirectory, basename)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("cleanup did not remove the owned partial file: %v", err)
			}
		})
	}
}
