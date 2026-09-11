package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"reasonix/desktop/internal/update"
)

func writeWindowsPayloadDir(t *testing.T, treeNames []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range update.WindowsPayloadFileNames() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("payload:"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range treeNames {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("payload:"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGenWindowsPayloadManifestWalksShellTree(t *testing.T) {
	treeNames := []string{"app/Reasonix.exe", "app/resources/app.asar", "app/locales/en-US.pak", "app/d3dcompiler_47.dll"}
	dir := writeWindowsPayloadDir(t, treeNames)
	if err := genWindowsPayloadManifest(dir, "v2.3.4"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, update.WindowsPayloadManifestName))
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := update.DecodeWindowsPayloadManifest(b, "v2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != len(update.WindowsPayloadFileNames())+len(treeNames) {
		t.Fatalf("manifest members = %d, want %d", len(hashes), len(update.WindowsPayloadFileNames())+len(treeNames))
	}
	for _, name := range append(update.WindowsPayloadFileNames(), treeNames...) {
		if want := update.WindowsPayloadSHA256([]byte("payload:" + name)); hashes[name] != want {
			t.Fatalf("manifest hash for %s = %q, want %q", name, hashes[name], want)
		}
	}
	want := append([]string{"reasonix-cli.exe", "reasonix-desktop.exe", "reasonix-update-helper.exe"}, treeNames...)
	slices.Sort(want)
	if got := update.WindowsPayloadVersionMembers(hashes); !slices.Equal(got, want) {
		t.Fatalf("version members = %v, want %v", got, want)
	}
}

func TestGenWindowsPayloadManifestRejectsSymlinkInShellTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege varies on Windows CI")
	}
	dir := writeWindowsPayloadDir(t, []string{"app/Reasonix.exe"})
	if err := os.Symlink(filepath.Join(dir, "reasonix-cli.exe"), filepath.Join(dir, "app", "linked.dll")); err != nil {
		t.Fatal(err)
	}
	if err := genWindowsPayloadManifest(dir, "v2.3.4"); err == nil {
		t.Fatal("symlink inside app/ was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, update.WindowsPayloadManifestName)); !os.IsNotExist(err) {
		t.Fatalf("manifest was written despite the rejected tree: %v", err)
	}
}
