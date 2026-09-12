package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func TestPresentedFileDeclaredRequiresExactTrustedMetadata(t *testing.T) {
	result := &control.ToolResultData{Name: "present", PresentedFiles: []provider.PresentedFile{
		{Path: "dist/game.html", Description: "Game"},
		{Path: "/tmp/report.pdf"},
	}}
	for _, test := range []struct {
		path string
		want bool
	}{
		{"dist/game.html", true},
		{"/tmp/report.pdf", true},
		{"game.html", false},
		{"dist/../dist/game.html", false},
		{"/tmp/other.pdf", false},
		{"", false},
	} {
		if got := presentedFileDeclared(result, test.path); got != test.want {
			t.Errorf("presentedFileDeclared(%q) = %v, want %v", test.path, got, test.want)
		}
	}
	if presentedFileDeclared(nil, "dist/game.html") {
		t.Fatal("nil result authorized a presented file")
	}
	result.Name = "plugin.present"
	if presentedFileDeclared(result, "dist/game.html") {
		t.Fatal("same-named plugin metadata authorized a presented file")
	}
}

func TestPresentedReadPolicyRechecksCurrentForbidRead(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	secretDir := filepath.Join(root, "secret")
	if err := os.MkdirAll(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "token.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[sandbox]\nforbid_read = [\"secret\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if presentedReadPolicyAllows(root, secret) {
		t.Fatal("a current forbid_read entry must revoke an older presentation")
	}
	public := filepath.Join(root, "public.txt")
	if err := os.WriteFile(public, []byte("public"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !presentedReadPolicyAllows(root, public) {
		t.Fatal("ordinary readable file was rejected")
	}
}
