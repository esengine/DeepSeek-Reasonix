package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentRemoteCLISelection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "remote-cli", "linux-amd64", "reasonix")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("linux artifact"), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := readDevelopmentRemoteCLI(dir, "linux", "amd64")
	if err != nil || string(data) != "linux artifact" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if _, err := readDevelopmentRemoteCLI(dir, "linux", "arm64"); err == nil || !strings.Contains(err.Error(), "build-remote-cli.mjs linux/arm64") {
		t.Fatalf("missing target must give build instructions: %v", err)
	}
	if _, err := readDevelopmentRemoteCLI(dir, "../linux", "amd64"); err == nil {
		t.Fatal("invalid target accepted")
	}
	if err := os.WriteFile(path, nil, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := readDevelopmentRemoteCLI(dir, "linux", "amd64"); err == nil {
		t.Fatal("empty artifact accepted")
	}
}
