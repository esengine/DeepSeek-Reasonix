package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadServeTokenFileTrimsSingleLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte("  s3cret-token \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := readServeTokenFile(p)
	if err != nil {
		t.Fatalf("readServeTokenFile: %v", err)
	}
	if tok != "s3cret-token" {
		t.Fatalf("token = %q, want s3cret-token", tok)
	}
}

func TestReadServeTokenFileRejectsEmptyAndMultiline(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readServeTokenFile(empty); err == nil {
		t.Fatal("empty token file accepted")
	}
	multi := filepath.Join(dir, "multi")
	if err := os.WriteFile(multi, []byte("a\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readServeTokenFile(multi); err == nil {
		t.Fatal("multi-line token file accepted")
	}
}

func TestReadServeTokenFileRejectsLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission check")
	}
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte("tok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readServeTokenFile(p); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("loose-permission token file accepted (err=%v)", err)
	}
}

func TestWriteServeAddrAndPidFiles(t *testing.T) {
	dir := t.TempDir()
	addrPath := filepath.Join(dir, "port")
	if err := writeServeAddrFile(addrPath, "127.0.0.1:43210"); err != nil {
		t.Fatalf("writeServeAddrFile: %v", err)
	}
	b, err := os.ReadFile(addrPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != "127.0.0.1:43210" {
		t.Fatalf("addr file = %q", got)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(addrPath)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Fatalf("addr file perm = %v, want owner-only", fi.Mode().Perm())
		}
	}

	pidPath := filepath.Join(dir, "pid")
	if err := writeServePidFile(pidPath); err != nil {
		t.Fatalf("writeServePidFile: %v", err)
	}
	b, err = os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) == "" {
		t.Fatal("pid file empty")
	}
}
