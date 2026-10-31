//go:build darwin || linux

package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"reasonix/internal/base/testenv"
)

func TestConfigLoadThroughSearchOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory mode checks")
	}
	for _, user := range []bool{false, true} {
		t.Run(fmt.Sprintf("user=%t", user), func(t *testing.T) {
			home, root := testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
			parent := filepath.Join(root, "search-only")
			logical := filepath.Join(root, "reasonix.toml")
			if user {
				parent = filepath.Join(testenv.TempDir(t), "search-only")
				logical = RootsForHome(home).UserConfigPath()
			}
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(parent, 0o700); err != nil {
					t.Error(err)
				}
			})
			target := filepath.Join(parent, "settings.toml")
			if err := os.WriteFile(target, []byte("[memory]\nrecall_limit = 937\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, logical); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := os.Chmod(parent, 0o100); err != nil {
				t.Fatal(err)
			}
			if _, err := os.ReadFile(target); err != nil {
				t.Fatalf("fixture is not a readable config behind a search-only directory: %v", err)
			}
			cfg, err := RootsForHome(home).LoadForRootReadOnly(root)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Memory.RecallLimit != 937 {
				t.Fatalf("search-only directory lost config: recall_limit=%d, want 937", cfg.Memory.RecallLimit)
			}
		})
	}
}

func TestConfigLoadRejectsParentReplacementBetweenReads(t *testing.T) {
	cfg, err := loadAcrossConfigReplacement(t, 0o700, func(parent, _, outside string) error {
		if err := os.Rename(parent, parent+"-original"); err != nil {
			return err
		}
		return os.Symlink(outside, parent)
	})
	if err == nil && cfg.Memory.RecallLimit == 937 {
		t.Fatal("later config reads followed a replacement outside the original project boundary")
	}
}

func TestConfigLoadObservesAtomicFileReplacementBetweenReads(t *testing.T) {
	cfg, err := loadAcrossConfigReplacement(t, 0o700, func(parent, target, _ string) error {
		replacement := filepath.Join(filepath.Dir(parent), "replacement.toml")
		if err := os.WriteFile(replacement, []byte("[memory]\nrecall_limit = 937\n"), 0o600); err != nil {
			return err
		}
		return os.Rename(replacement, target)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Memory.RecallLimit != 937 {
		t.Fatalf("load hid an atomic file replacement: recall_limit=%d, want 937", cfg.Memory.RecallLimit)
	}
}

func TestConfigUncachedFallbackRejectsParentReplacementBetweenReads(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory mode checks")
	}
	cfg, err := loadAcrossConfigReplacement(t, 0o300, func(parent, _, outside string) error {
		if err := os.Rename(parent, parent+"-original"); err != nil {
			return err
		}
		return os.Symlink(outside, parent)
	})
	if err == nil && cfg.Memory.RecallLimit == 937 {
		t.Fatal("guarded fallback followed a replacement outside the project")
	}
}

func loadAcrossConfigReplacement(t *testing.T, rootMode os.FileMode, replace func(parent, target, outside string) error) (*Config, error) {
	t.Helper()
	home, root, outside := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	parent := filepath.Join(root, "config")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "settings.toml")
	if err := unix.Mkfifo(target, 0o600); err != nil {
		t.Skipf("named pipes unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "settings.toml"), []byte("[memory]\nrecall_limit = 937\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "reasonix.toml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Chmod(root, rootMode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(root, 0o700); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), testenv.Budget(t))
	defer cancel()
	done := make(chan error, 1)
	go func() {
		for {
			fd, err := unix.Open(target, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if err == nil {
				file := os.NewFile(uintptr(fd), target)
				if err := replace(parent, target, outside); err != nil {
					_ = file.Close()
					done <- err
					return
				}
				_, err = file.WriteString("[memory]\nrecall_limit = 7\n")
				closeErr := file.Close()
				done <- errors.Join(err, closeErr)
				return
			}
			if !errors.Is(err, unix.ENXIO) {
				done <- err
				return
			}
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	cfg, err := RootsForHome(home).LoadForRootReadOnly(root)
	cancel()
	if writerErr := <-done; writerErr != nil {
		t.Fatal(writerErr)
	}
	return cfg, err
}
