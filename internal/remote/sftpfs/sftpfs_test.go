package sftpfs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/remote/sshtest"
)

func dialFS(t *testing.T, root string) *FS {
	t.Helper()
	// The remote module targets POSIX (Linux/macOS) remotes: SFTP paths are
	// always forward-slash and rooted on the remote host. This harness runs the
	// SFTP server against the LOCAL filesystem, so on Windows it serves Windows
	// drive paths and the POSIX/Windows path translation breaks — a property of
	// the harness, not the product. Linux/macOS CI covers the round-trips.
	if runtime.GOOS == "windows" {
		t.Skip("SFTP-server harness serves the local FS; POSIX-remote paths are only exercised on Linux/macOS")
	}
	srv := sshtest.Start(t, sshtest.Options{SFTPRoot: root})
	cfg := &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	cl, err := ssh.Dial("tcp", srv.Addr, cfg)
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}
	t.Cleanup(func() { cl.Close() })
	fsys, err := New(cl)
	if err != nil {
		t.Fatalf("sftp new: %v", err)
	}
	t.Cleanup(func() { fsys.Close() })
	return fsys
}

func TestSFTPListStatRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hi there"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	fsys := dialFS(t, root)
	ctx := context.Background()

	entries, err := fsys.List(ctx, root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = e.IsDir
	}
	if _, ok := names["hello.txt"]; !ok {
		t.Fatalf("hello.txt missing from listing: %+v", entries)
	}
	if !names["sub"] {
		t.Fatal("sub not reported as dir")
	}

	st, err := fsys.Stat(ctx, filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st.Size != 8 {
		t.Fatalf("size = %d, want 8", st.Size)
	}

	data, truncated, kind, err := fsys.ReadFile(ctx, filepath.Join(root, "hello.txt"), 0)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hi there" || truncated || kind != KindText {
		t.Fatalf("read = %q truncated=%v kind=%v", data, truncated, kind)
	}
}

// TestSFTPDownloadStreamsFullFile pins the fs-get fix: Download must return the
// whole file, not the 4 MiB preview cap ReadFile enforces.
func TestSFTPDownloadStreamsFullFile(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", (DefaultReadCap)+5000) // > preview cap
	if err := os.WriteFile(filepath.Join(root, "big.bin"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys := dialFS(t, root)

	// ReadFile truncates at the cap...
	_, truncated, _, err := fsys.ReadFile(context.Background(), filepath.Join(root, "big.bin"), 0)
	if err != nil || !truncated {
		t.Fatalf("expected ReadFile to report truncation (err=%v truncated=%v)", err, truncated)
	}
	// ...but Download returns every byte.
	var buf bytes.Buffer
	n, err := fsys.Download(context.Background(), filepath.Join(root, "big.bin"), &buf)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if n != int64(len(big)) || buf.Len() != len(big) {
		t.Fatalf("Download got %d bytes, want %d (must not truncate)", n, len(big))
	}
}

func TestSFTPReadCapTruncates(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("a", 100)
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys := dialFS(t, root)
	data, truncated, _, err := fsys.ReadFile(context.Background(), filepath.Join(root, "big.txt"), 10)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !truncated || len(data) != 10 {
		t.Fatalf("cap not honored: len=%d truncated=%v", len(data), truncated)
	}
}

func TestSFTPWriteAtomicAndMkdirRenameRemove(t *testing.T) {
	root := t.TempDir()
	fsys := dialFS(t, root)
	ctx := context.Background()

	target := filepath.Join(root, "out.txt")
	if err := fsys.WriteFileAtomic(ctx, target, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	// No temp file left behind.
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.Contains(e.Name(), "reasonix-tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "content" {
		t.Fatalf("written content = %q err=%v", got, err)
	}
	uploaded := filepath.Join(root, "uploaded.txt")
	n, err := fsys.UploadAtomic(ctx, uploaded, strings.NewReader("streamed"), 0o600)
	if err != nil || n != 8 {
		t.Fatalf("UploadAtomic = %d, %v", n, err)
	}
	if got, err := os.ReadFile(uploaded); err != nil || string(got) != "streamed" {
		t.Fatalf("uploaded content = %q err=%v", got, err)
	}
	info, err := os.Stat(uploaded)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("uploaded mode = %v", info.Mode().Perm())
	}

	// Overwrite existing (exercises rename-over-existing path).
	if err := fsys.WriteFileAtomic(ctx, target, []byte("v2"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, _ = os.ReadFile(target)
	if string(got) != "v2" {
		t.Fatalf("overwrite content = %q", got)
	}

	dir := filepath.Join(root, "a", "b")
	if err := fsys.MkdirAll(ctx, dir); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("mkdir -p failed: %v", err)
	}

	renamed := filepath.Join(root, "renamed.txt")
	if err := fsys.Rename(ctx, target, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("rename target missing: %v", err)
	}

	if err := fsys.Remove(ctx, renamed, false); err != nil {
		t.Fatalf("Remove file: %v", err)
	}
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Fatal("file not removed")
	}
	if err := fsys.Remove(ctx, filepath.Join(root, "a"), true); err != nil {
		t.Fatalf("Remove dir recursive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Fatal("dir not removed")
	}
}
