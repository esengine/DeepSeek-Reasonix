package feishu

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/config"
)

func TestReadOutboundFileConfinement(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := &adapter{cfg: config.FeishuBotConfig{OutboundMediaRoots: []string{root}}}

	data, name, err := a.readOutboundFile("ok.txt")
	if err != nil {
		t.Fatalf("file inside root should be readable: %v", err)
	}
	if string(data) != "hello" || name != "ok.txt" {
		t.Fatalf("got %q/%q, want hello/ok.txt", data, name)
	}

	// Paths are never aliases for a staged filename: callers must send a bare
	// filename, even when an absolute path happens to point inside a root.
	if _, _, err := a.readOutboundFile(filepath.Join(root, "ok.txt")); err == nil {
		t.Fatal("absolute paths must be rejected")
	}

	// Outside every root: rejected before any lookup.
	if _, _, err := a.readOutboundFile(outside); err == nil {
		t.Fatal("file outside the roots must be rejected")
	}

	// Traversal that resolves outside the root: rejected.
	if _, _, err := a.readOutboundFile(filepath.Join(root, "..", filepath.Base(outside))); err == nil {
		t.Fatal("traversal out of the root must be rejected")
	}

	// Relative path: rejected.
	if _, _, err := a.readOutboundFile("relative/path"); err == nil {
		t.Fatal("relative path must be rejected")
	}

	// No roots configured: local sending disabled.
	off := &adapter{cfg: config.FeishuBotConfig{}}
	if _, _, err := off.readOutboundFile("ok.txt"); err == nil {
		t.Fatal("local file sending must be disabled when no roots are set")
	}
}

func TestReadOutboundFileRejectsAmbiguousName(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, root := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(root, "report.pdf"), []byte(root), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := &adapter{cfg: config.FeishuBotConfig{OutboundMediaRoots: []string{first, second}}}
	if _, _, err := a.readOutboundFile("report.pdf"); err == nil {
		t.Fatal("the same filename in multiple roots must be rejected as ambiguous")
	}
}

func TestReadOutboundFileRequiresAbsoluteRoots(t *testing.T) {
	a := &adapter{cfg: config.FeishuBotConfig{OutboundMediaRoots: []string{"relative-root"}}}
	if _, _, err := a.readOutboundFile("report.pdf"); err == nil {
		t.Fatal("relative outbound media roots must be rejected")
	}
}

func TestReadOutboundFileEnforcesActualReadLimit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.bin")
	if err := os.WriteFile(path, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxOutboundMediaBytes+1); err != nil {
		t.Fatal(err)
	}
	a := &adapter{cfg: config.FeishuBotConfig{OutboundMediaRoots: []string{root}}}
	if _, _, err := a.readOutboundFile("large.bin"); err == nil {
		t.Fatal("files larger than the actual read limit must be rejected")
	}
}

func TestLoadOutboundMediaEnforcesAggregateLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first.bin", "second.bin"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte{1}, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(path, maxOutboundMediaBytes/2+1); err != nil {
			t.Fatal(err)
		}
	}
	a := &adapter{cfg: config.FeishuBotConfig{OutboundMediaRoots: []string{root}}}
	if _, err := a.loadOutboundMedia([]string{"first.bin", "second.bin"}); err == nil {
		t.Fatal("aggregate outbound media larger than 25 MB must be rejected before sending")
	}
}

func TestReadOutboundFileRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is unreliable on Windows CI")
	}
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A symlink living inside the allowed root but pointing outside it.
	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	a := &adapter{cfg: config.FeishuBotConfig{OutboundMediaRoots: []string{root}}}
	if _, _, err := a.readOutboundFile("escape.txt"); err == nil {
		t.Fatal("a symlink escaping the root must be rejected (symlink resolution)")
	}
}

func TestReadOutboundFileAcceptsSymlinkWithinRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is unreliable on Windows CI")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(filepath.Base(real), link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	a := &adapter{cfg: config.FeishuBotConfig{OutboundMediaRoots: []string{root}}}
	if _, _, err := a.readOutboundFile("link.txt"); err != nil {
		t.Fatalf("a symlink staying within the root should be allowed: %v", err)
	}
}
