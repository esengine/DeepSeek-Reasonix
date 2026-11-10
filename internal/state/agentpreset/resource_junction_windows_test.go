package agentpreset

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
)

func makeResourceJunction(t *testing.T, target, link string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("create directory junction: %v: %s", err, out)
	}
}

func TestWindowsCompositionJunctions(t *testing.T) {
	t.Run("capture-and-restore", func(t *testing.T) {
		source := testenv.TempDir(t)
		data := filepath.Join(source, "data")
		if err := os.Mkdir(data, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, "entry.ps1"), []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
		makeResourceJunction(t, data, filepath.Join(source, "alias"))
		link := filepath.Join(testenv.TempDir(t), "source")
		makeResourceJunction(t, source, link)
		asset, err := CaptureDirectory(link)
		if err != nil || len(asset.Files) != 2 {
			t.Fatalf("junction capture: %+v %v", asset, err)
		}
		entry := filepath.Join(link, "alias", "entry.ps1")
		hookAsset, mapping, err := CaptureHookResources([]string{entry})
		if err != nil || mapping[entry] == "" {
			t.Fatalf("junction hook capture: %v", err)
		}
		if err := os.RemoveAll(source); err != nil {
			t.Fatal(err)
		}
		store := New(testenv.TempDir(t), testenv.TempDir(t))
		for _, snapshot := range []DirectorySnapshot{asset, hookAsset} {
			root, err := store.Materialize(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Materialize(snapshot); err != nil {
				t.Fatalf("reused junction snapshot: %v", err)
			}
			for _, file := range snapshot.Files {
				path := filepath.Join(root, filepath.FromSlash(file.Path))
				body, err := os.ReadFile(path)
				if err != nil || string(body) != "original" {
					t.Fatalf("junction resource lost: %q %v", body, err)
				}
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() || file.Link != "" {
					t.Fatalf("junction was not flattened: %v", err)
				}
			}
		}
	})
	t.Run("escape", func(t *testing.T) {
		root, outside := testenv.TempDir(t), testenv.TempDir(t)
		makeResourceJunction(t, outside, filepath.Join(root, "alias"))
		if _, err := CaptureDirectory(root); !errors.Is(err, agentcomposition.ErrUnavailable) {
			t.Fatalf("outside junction accepted: %v", err)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		root := testenv.TempDir(t)
		makeResourceJunction(t, root, filepath.Join(root, "alias"))
		if _, err := CaptureDirectory(root); !errors.Is(err, agentcomposition.ErrUnavailable) {
			t.Fatalf("cyclic junction accepted: %v", err)
		}
	})
	t.Run("retained-root", func(t *testing.T) {
		store := New(testenv.TempDir(t), testenv.TempDir(t))
		asset := DirectorySnapshot{Files: []AssetFile{{Path: "entry", Data: []byte("same"), Mode: 0o600}}}
		root, err := store.Materialize(asset)
		if err != nil {
			t.Fatal(err)
		}
		moved := filepath.Join(testenv.TempDir(t), "moved")
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		makeResourceJunction(t, moved, root)
		if _, err := store.Materialize(asset); !errors.Is(err, agentcomposition.ErrUnavailable) {
			t.Fatalf("retained junction accepted: %v", err)
		}
	})
}
