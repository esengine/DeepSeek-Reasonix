package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestMoveFileRenameDoesNotRequireContentRead(t *testing.T) {
	withProtectSensitiveFiles(t, true)
	root := testenv.TempDir(t)
	private := filepath.Join(root, "private")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"protected_destination", "protected_source"} {
		t.Run(name, func(t *testing.T) {
			src, dst := filepath.Join(root, "source.txt"), filepath.Join(private, "server.key")
			if name == "protected_source" {
				src, dst = filepath.Join(private, "source.key"), filepath.Join(root, "renamed.txt")
			}
			if err := os.WriteFile(src, []byte("synthetic content\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ws := Workspace{Dir: root, ForbidReadRoots: []string{private}}
			mover := ws.Tools("move_file")[0]
			args := argsJSON(t, map[string]any{"source_path": src, "destination_path": dst})
			if err := ws.TargetAccessCheck()(context.Background(), mover, args); err != nil {
				t.Fatalf("rename incorrectly requires content read: %v", err)
			}
			if _, err := mover.Execute(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(dst); err != nil || string(got) != "synthetic content\n" {
				t.Fatalf("destination=%q error=%v", got, err)
			}
			if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("source remained after rename: %v", err)
			}
		})
	}
}

func TestMoveFileCopyFallbackChecksOnlySourceReads(t *testing.T) {
	for _, forbiddenSource := range []bool{false, true} {
		name := "protected_destination"
		if forbiddenSource {
			name = "protected_source"
		}
		t.Run(name, func(t *testing.T) {
			root := testenv.TempDir(t)
			src, dst := filepath.Join(root, "source.txt"), filepath.Join(root, "moved.txt")
			const original = "COPY_SOURCE_CANARY\n"
			if err := os.WriteFile(src, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			forbidden := dst
			if forbiddenSource {
				forbidden = src
			}
			ws := Workspace{Dir: root, ForbidReadRoots: []string{forbidden}}
			mover := ws.Tools("move_file")[0]
			args := argsJSON(t, map[string]any{"source_path": src, "destination_path": dst})
			if err := ws.TargetAccessCheck()(context.Background(), mover, args); err != nil {
				t.Fatalf("rename admission incorrectly requires content read: %v", err)
			}
			oldRename := renameFile
			renameFile = func(oldpath, newpath string) error {
				return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: crossDeviceErrno}
			}
			t.Cleanup(func() { renameFile = oldRename })
			_, err := mover.Execute(context.Background(), args)
			if forbiddenSource {
				var refusal tool.Refusal
				if !errors.As(err, &refusal) || refusal.Code != CodeReadForbidden {
					t.Fatalf("copy must deny forbidden source: %v", err)
				}
				if got, err := os.ReadFile(src); err != nil || string(got) != original {
					t.Fatalf("source changed: %q %v", got, err)
				}
				if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("denied copy created destination: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(dst); err != nil || string(got) != original {
				t.Fatalf("destination=%q error=%v", got, err)
			}
		})
	}
}
