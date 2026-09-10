package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/desktop/internal/update"
)

func TestStagedWindowsPayloadMembersBindSignedDigests(t *testing.T) {
	staging := t.TempDir()
	names := []string{"reasonix-desktop.exe", "app/Reasonix.exe", "app/resources/app.asar"}
	hashes := make(map[string]string, len(names))
	for _, name := range names {
		path := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("payload:"+name), 0o700); err != nil {
			t.Fatal(err)
		}
		hashes[name] = update.WindowsPayloadSHA256([]byte("payload:" + name))
	}
	members, err := stagedWindowsPayloadMembers(staging, hashes, names)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != len(names) {
		t.Fatalf("members = %d, want %d", len(members), len(names))
	}
	for i, member := range members {
		if member.Name != names[i] || member.Path != filepath.Join(staging, filepath.FromSlash(names[i])) || member.Mode != 0o700 {
			t.Fatalf("member %d = %+v", i, member)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "app", "Reasonix.exe"), []byte("drift"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := stagedWindowsPayloadMembers(staging, hashes, names); err == nil {
		t.Fatal("changed staged file was bound to the signed digest")
	}
	if _, err := stagedWindowsPayloadMembers(staging, hashes, []string{"app/missing.dll"}); err == nil {
		t.Fatal("missing staged file was bound")
	}
	if _, err := stagedWindowsPayloadMembers(staging, hashes, []string{"app"}); err == nil {
		t.Fatal("directory was bound as a payload member")
	}
}
