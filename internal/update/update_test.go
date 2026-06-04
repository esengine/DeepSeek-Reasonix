package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		// Clean semver.
		{"v1.2.3", "v1.2.3", true},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.0", "v1.2.0", true},
		// Prerelease.
		{" v2.0.0-rc1 ", "v2.0.0-rc1", true},
		// git-describe output.
		{"desktop-v1.0.0-105-gf3894d6f", "v1.0.0", true},
		{"v1.1.0-3-gabcdef0", "v1.1.0", true},
		{"v1.2.0-rc1-5-g1234567", "v1.2.0-rc1", true},
		// Invalid / empty.
		{"dev", "", false},
		{"", "", false},
		{"  ", "", false},
		{"not-a-version", "", false},
	}
	for _, tt := range tests {
		got, ok := normalizeVersion(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("normalizeVersion(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseSHA256SUMS(t *testing.T) {
	sums := []byte("abc123  reasonix-linux-amd64.tar.gz\ndef456  reasonix-windows-amd64.zip\n")
	got, err := parseSHA256SUMS(sums, "reasonix-linux-amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Errorf("got %q, want %q", got, "abc123")
	}

	_, err = parseSHA256SUMS(sums, "nonexistent.tar.gz")
	if err == nil {
		t.Error("expected error for missing entry")
	}
}

func TestCheckSHA256(t *testing.T) {
	data := []byte("hello world")
	// sha256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	err := checkSHA256(data, "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	err = checkSHA256(data, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Error("expected mismatch error")
	}
}

func TestExtractFromTarGz(t *testing.T) {
	// Build a .tar.gz with one file.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	content := []byte("fake-binary-data")
	hdr := &tar.Header{
		Name: "reasonix-linux-amd64/reasonix",
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	got, err := extractFromTarGz(buf.Bytes(), "reasonix")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content mismatch")
	}
}

func TestSumsAssetURL(t *testing.T) {
	assets := []Asset{
		{Name: "reasonix-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/a.tar.gz"},
		{Name: "SHA256SUMS", BrowserDownloadURL: "https://example.com/SHA256SUMS"},
	}
	got := sumsAssetURL(assets)
	if got != "https://example.com/SHA256SUMS" {
		t.Errorf("got %q", got)
	}
	if sumsAssetURL(nil) != "" {
		t.Error("expected empty for nil assets")
	}
}

func TestAssetName(t *testing.T) {
	name := assetName()
	if name == "" {
		t.Error("assetName returned empty")
	}
	// Should contain "reasonix" and the OS.
	if !bytes.Contains([]byte(name), []byte("reasonix")) {
		t.Errorf("assetName %q doesn't contain 'reasonix'", name)
	}
}
