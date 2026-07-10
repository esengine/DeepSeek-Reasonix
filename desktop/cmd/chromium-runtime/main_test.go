package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckedInManifestIsComplete(t *testing.T) {
	manifest, err := loadManifest(filepath.Join("..", "..", "build", "chromium", "manifest.json"))
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if err := validateManifest(manifest); err != nil {
		t.Fatalf("validateManifest: %v", err)
	}
	if manifest.RuntimeVersion != "149.0.7827.55" {
		t.Fatalf("runtimeVersion = %q", manifest.RuntimeVersion)
	}
}

func TestExtractArchiveRejectsPathTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "malicious.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("runtime/../../escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("nope"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "output")
	if err := extractArchive(archive, output, "runtime"); err == nil {
		t.Fatal("malicious archive was accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(output), "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive wrote outside output: %v", err)
	}
}

func TestExtractArchiveRejectsEscapingSymlink(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "malicious-symlink.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "runtime/link"}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("../../escaped"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive, filepath.Join(t.TempDir(), "output"), "runtime"); err == nil {
		t.Fatal("escaping archive symlink was accepted")
	}
}

func TestVerifyArchiveFileRejectsWrongHash(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "runtime.zip")
	data := []byte("fixed archive")
	if err := os.WriteFile(archive, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("different archive"))
	artifact := runtimeArtifact{ArchiveSize: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
	if err := verifyArchiveFile(archive, artifact); err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("verifyArchiveFile error = %v", err)
	}
}

func TestVerifyExecutableArchitectureRejectsWrongArchitecture(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrongArch := "arm64"
	if runtime.GOARCH == "arm64" {
		wrongArch = "amd64"
	}
	if err := verifyExecutableArchitecture(executable, runtime.GOOS, wrongArch); err == nil || !strings.Contains(err.Error(), "architecture mismatch") {
		t.Fatalf("verifyExecutableArchitecture error = %v", err)
	}
}

func TestVerifyPreparedRuntimeRejectsMissingResources(t *testing.T) {
	artifact := runtimeArtifact{
		Executable:    "chrome",
		RequiredPaths: []string{"chrome", "resources.pak", "locales"},
	}
	err := verifyPreparedRuntime(t.TempDir(), "linux-amd64", "linux", "amd64", "test", artifact, false)
	if err == nil || !strings.Contains(err.Error(), "required Chromium resource") {
		t.Fatalf("verifyPreparedRuntime error = %v", err)
	}
}

func TestSafeRelativePathRejectsEscapes(t *testing.T) {
	for _, value := range []string{"", ".", "..", "../chrome", "/chrome", `C:\\chrome.exe`} {
		if _, err := safeRelativePath(value); err == nil {
			t.Errorf("safeRelativePath(%q) accepted an unsafe path", value)
		}
	}
	if got, err := safeRelativePath("Chromium.app/Contents/MacOS/Chromium"); err != nil || got == "" {
		t.Fatalf("safeRelativePath valid path = %q, %v", got, err)
	}
}
