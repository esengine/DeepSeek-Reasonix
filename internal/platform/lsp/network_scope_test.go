package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/fileutil"
)

func TestNetworkPathFileIsRefusedBeforeTheServerSyncs(t *testing.T) {
	prev := fileutil.HostIsWindows
	fileutil.HostIsWindows = true
	t.Cleanup(func() { fileutil.HostIsWindows = prev })

	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	slashed := filepath.ToSlash(file)
	if !strings.HasPrefix(slashed, "/") {
		t.Skip("needs a rooted local path")
	}
	m := NewManager(dir, DefaultSpecs())
	defer m.Close()
	net := "/" + slashed
	if _, err := m.Diagnostics(context.Background(), net); !errors.Is(err, fileutil.ErrNetworkPathOutsideScope) {
		t.Errorf("Diagnostics err = %v, want ErrNetworkPathOutsideScope", err)
	}
	if _, err := m.Definition(context.Background(), net, 1, "a"); !errors.Is(err, fileutil.ErrNetworkPathOutsideScope) {
		t.Errorf("Definition err = %v, want ErrNetworkPathOutsideScope", err)
	}
	if _, err := m.scoped(file); err != nil {
		t.Errorf("local path refused: %v", err)
	}
}
