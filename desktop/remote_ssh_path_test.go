package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRemoteSSHExecutableKeepsConfiguredOverride(t *testing.T) {
	called := false
	got, err := resolveRemoteSSHExecutableForOS("windows", ` C:\Tools\OpenSSH\ssh.exe `, nil, func(string) (string, error) {
		called = true
		return "", errors.New("unexpected lookup")
	}, os.Stat)
	if err != nil {
		t.Fatal(err)
	}
	if got != `C:\Tools\OpenSSH\ssh.exe` || called {
		t.Fatalf("configured result = %q, lookup called=%v", got, called)
	}
}

func TestResolveRemoteSSHExecutableWindowsPrefersSystemOpenSSHWithoutPATH(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "System32", "OpenSSH", "ssh.exe")
	if err := os.MkdirAll(filepath.Dir(candidate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("test OpenSSH"), 0o755); err != nil {
		t.Fatal(err)
	}
	lookedUp := false
	got, err := resolveRemoteSSHExecutableForOS("windows", "", []string{"", root, root}, func(string) (string, error) {
		lookedUp = true
		return "", execErrNotFound()
	}, os.Stat)
	if err != nil {
		t.Fatal(err)
	}
	if got != candidate || lookedUp {
		t.Fatalf("resolved = %q, want %q; PATH lookup called=%v", got, candidate, lookedUp)
	}
}

func TestResolveRemoteSSHExecutableWindowsFallsBackToAbsolutePATHResult(t *testing.T) {
	pathResult := filepath.Join(t.TempDir(), "OpenSSH", "ssh.exe")
	got, err := resolveRemoteSSHExecutableForOS("windows", "", []string{"relative", ""}, func(name string) (string, error) {
		if name != "ssh.exe" {
			t.Fatalf("LookPath name = %q", name)
		}
		return pathResult, nil
	}, os.Stat)
	if err != nil {
		t.Fatal(err)
	}
	if got != pathResult {
		t.Fatalf("resolved = %q, want %q", got, pathResult)
	}
}

func TestResolveRemoteSSHExecutableWindowsRejectsRelativePATHResult(t *testing.T) {
	got, err := resolveRemoteSSHExecutableForOS("windows", "", nil, func(string) (string, error) {
		return filepath.Join("relative", "ssh.exe"), nil
	}, os.Stat)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ssh.exe" {
		t.Fatalf("resolved = %q, want controlled missing-command fallback", got)
	}
}

func TestResolveRemoteSSHExecutableOtherPlatformsKeepPATHCommand(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		got, err := resolveRemoteSSHExecutableForOS(goos, "", nil, nil, nil)
		if err != nil || got != "ssh" {
			t.Errorf("%s result = %q, %v", goos, got, err)
		}
	}
}

func TestResolveRemoteSSHExecutableRejectsNUL(t *testing.T) {
	if _, err := resolveRemoteSSHExecutableForOS("windows", "ssh\x00.exe", nil, nil, nil); err == nil {
		t.Fatal("NUL OpenSSH path accepted")
	}
}

func execErrNotFound() error { return errors.New("executable file not found") }
