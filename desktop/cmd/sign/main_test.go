package main

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aead.dev/minisign"

	"reasonix/desktop/internal/update"
)

// TestSignFiles signs a file with a throwaway key pair (injected via env, exactly
// as CI passes the real key) and verifies the produced .minisig validates under the
// matching public key.
func TestSignFiles(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := minisign.EncryptKey("pw", priv)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINISIGN_PRIVATE_KEY", string(enc))
	t.Setenv("MINISIGN_PASSWORD", "pw")

	dir := t.TempDir()
	artifact := filepath.Join(dir, "Reasonix-linux-amd64.tar.gz")
	payload := []byte("pretend this is a release tarball")
	if err := os.WriteFile(artifact, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := signFiles([]string{artifact}); err != nil {
		t.Fatalf("signFiles: %v", err)
	}
	sig, err := os.ReadFile(artifact + ".minisig")
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	if !minisign.Verify(pub, payload, sig) {
		t.Fatal("produced signature does not verify under the signing key")
	}
}

// TestGenManifest builds a manifest from a directory of fake artifacts and checks
// every platform is listed with a download URL, a parallel .minisig URL, and a
// non-empty digest. The .minisig and latest.json files must be ignored.
func TestGenManifest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"Reasonix-darwin-arm64.zip",
		"Reasonix-darwin-amd64.zip",
		"Reasonix-darwin-universal.dmg",
		"Reasonix-windows-amd64-installer.exe",
		"Reasonix-windows-amd64.zip", // portable download, not the updater channel
		"Reasonix-windows-arm64-installer.exe",
		"Reasonix-windows-arm64.zip", // portable download, not the updater channel
		"Reasonix-linux-amd64.tar.gz",
		"Reasonix-linux-amd64.deb",            // human download, not the updater channel
		"Reasonix-linux-amd64.tar.gz.minisig", // must be skipped
		"README.txt",                          // unmatched, must be skipped
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GITHUB_REPOSITORY", "esengine/reasonix")

	if err := genManifest(dir, "v1.2.0", "desktop-v1.2.0"); err != nil {
		t.Fatalf("genManifest: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m update.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("latest.json is not valid: %v", err)
	}
	if m.Version != "v1.2.0" {
		t.Fatalf("version = %q, want v1.2.0", m.Version)
	}
	if m.DownloadPage != "https://reasonix.io/?download=desktop#start" {
		t.Fatalf("download_page = %q, want official install page", m.DownloadPage)
	}
	if len(m.Platforms) != 5 {
		t.Fatalf("want 5 platforms, got %d: %v", len(m.Platforms), m.Platforms)
	}
	win, ok := m.Platforms["windows-amd64"]
	if !ok {
		t.Fatal("windows-amd64 missing")
	}
	wantURL := "https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.2.0/Reasonix-windows-amd64-installer.exe"
	if win.URL != wantURL {
		t.Fatalf("windows url = %q, want %q", win.URL, wantURL)
	}
	if win.Sig != wantURL+".minisig" {
		t.Fatalf("windows sig = %q, want %q.minisig", win.Sig, wantURL)
	}
	if win.SHA256 == "" || win.Size == 0 {
		t.Fatalf("windows asset missing digest/size: %+v", win)
	}
	// The Windows updater channel is the per-arch -installer.exe; the portable .zip
	// must not shadow the windows-arm64 key.
	arm, ok := m.Platforms["windows-arm64"]
	if !ok {
		t.Fatal("windows-arm64 missing")
	}
	if !strings.HasSuffix(arm.URL, "/Reasonix-windows-arm64-installer.exe") {
		t.Fatalf("windows-arm64 url = %q, want the installer, not the portable zip", arm.URL)
	}
	// The Linux portable channel stays the .tar.gz; the co-located .deb lands
	// only in native_packages so older clients keep resolving platforms["linux-amd64"].
	lin, ok := m.Platforms["linux-amd64"]
	if !ok {
		t.Fatal("linux-amd64 missing")
	}
	if !strings.HasSuffix(lin.URL, "/Reasonix-linux-amd64.tar.gz") {
		t.Fatalf("linux-amd64 url = %q, want the .tar.gz, not the .deb", lin.URL)
	}
	if lin.Sig == "" || lin.SHA256 == "" || lin.Size == 0 {
		t.Fatalf("linux portable asset incomplete: %+v", lin)
	}
	deb, ok := m.NativePackages["linux-amd64"]
	if !ok {
		t.Fatal("native_packages linux-amd64 missing")
	}
	if !strings.HasSuffix(deb.URL, "/Reasonix-linux-amd64.deb") {
		t.Fatalf("native linux-amd64 url = %q, want the .deb", deb.URL)
	}
	if deb.Sig != deb.URL+".minisig" || deb.SHA256 == "" || deb.Size == 0 {
		t.Fatalf("native linux asset incomplete: %+v", deb)
	}
	if len(m.Downloads) != 2 {
		t.Fatalf("want 2 website downloads, got %d: %+v", len(m.Downloads), m.Downloads)
	}
	for _, name := range []string{"Reasonix-darwin-universal.dmg", "Reasonix-windows-amd64.zip"} {
		asset, ok := m.Downloads[name]
		if !ok {
			t.Fatalf("website download %q missing", name)
		}
		if !strings.HasSuffix(asset.URL, "/"+name) ||
			asset.Sig != asset.URL+".minisig" ||
			asset.SHA256 == "" ||
			asset.Size == 0 {
			t.Fatalf("website download %q incomplete: %+v", name, asset)
		}
	}
}

// TestGenManifestIgnoresUnknownNativePackages ensures a .deb without a known
// platform key is skipped rather than inventing a native_packages entry.
func TestGenManifestIgnoresUnknownNativePackages(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"Reasonix-linux-amd64.tar.gz",
		"Reasonix-mystery.deb",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GITHUB_REPOSITORY", "esengine/DeepSeek-Reasonix")
	if err := genManifest(dir, "v1.2.0", "desktop-v1.2.0"); err != nil {
		t.Fatalf("genManifest: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m update.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.NativePackages) != 0 {
		t.Fatalf("unexpected native_packages: %+v", m.NativePackages)
	}
}

func TestGenWindowsPayloadManifestHashesExactReleaseUnit(t *testing.T) {
	dir := t.TempDir()
	for _, name := range update.WindowsPayloadFileNames() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("payload:"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
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
	for _, name := range update.WindowsPayloadFileNames() {
		want := update.WindowsPayloadSHA256([]byte("payload:" + name))
		if hashes[name] != want {
			t.Fatalf("manifest hash for %s = %q, want %q", name, hashes[name], want)
		}
	}
}
