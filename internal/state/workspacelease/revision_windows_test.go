//go:build windows

package workspacelease

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"reasonix/internal/base/testenv"
)

func TestWindowsMissingAliasFilesystemEffectsSerialize(t *testing.T) {
	for _, alias := range []string{"file.", "file ", "dir./file"} {
		t.Run(alias, func(t *testing.T) {
			root, locks := pathLeaseRoot(t), testenv.TempDir(t)
			a, _ := New(root, locks, nil)
			b, _ := New(root, locks, nil)
			a.BeginRun()
			b.BeginRun()
			defer a.EndRun()
			defer b.EndRun()
			if err := a.AcquirePaths(context.Background(), []string{alias}); err != nil {
				t.Fatal(err)
			}
			canonical := strings.ReplaceAll(strings.ReplaceAll(alias, "dir.", "dir"), "dir ", "dir")
			canonical = strings.TrimRight(canonical, ". ")
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
			defer cancel()
			if err := b.AcquirePaths(ctx, []string{canonical}); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("missing alias acquired independently: %v", err)
			}
			aliasPath := filepath.Join(root, alias)
			if err := os.MkdirAll(filepath.Dir(aliasPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(aliasPath, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(filepath.Join(root, canonical)); err != nil || string(data) != "fixture" {
				t.Fatalf("alias did not reach the same file: %q, %v", data, err)
			}
			a.EndRun()
			if err := b.AcquirePaths(context.Background(), []string{canonical}); err != nil {
				t.Fatalf("release lost admission: %v", err)
			}
		})
	}
}

func TestWindowsShortAncestorClaimsMissingLeafByLongIdentity(t *testing.T) {
	root, locks := pathLeaseRoot(t), testenv.TempDir(t)
	ancestor := filepath.Join(root, "A Long Directory Name")
	if err := os.Mkdir(ancestor, 0o700); err != nil {
		t.Fatal(err)
	}
	ptr, err := windows.UTF16PtrFromString(ancestor)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 512)
	n, err := windows.GetShortPathName(ptr, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) >= len(buf) {
		t.Fatalf("short name: %d, %v", n, err)
	}
	short := windows.UTF16ToString(buf[:n])
	if strings.EqualFold(short, ancestor) {
		t.Skip("volume has no 8.3 alias for this directory")
	}
	a, _ := New(root, locks, nil)
	b, _ := New(root, locks, nil)
	a.BeginRun()
	b.BeginRun()
	defer a.EndRun()
	defer b.EndRun()
	if err := a.AcquirePaths(context.Background(), []string{filepath.Join(ancestor, "missing.txt")}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	if err := b.AcquirePaths(ctx, []string{filepath.Join(short, "missing.txt")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("8.3 ancestor established a separate missing leaf: %v", err)
	}
}
