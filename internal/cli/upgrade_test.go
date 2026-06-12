package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"dev", "", false},
		{"", "", false},
		{"  ", "", false},
		{"abc", "", false},
		{"v1.2.3", "v1.2.3", true},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3-rc1", "v1.2.3-rc1", true},
		{"  v0.10.0  ", "v0.10.0", true},
	}
	for _, tt := range tests {
		got, ok := normalizeVersion(tt.in)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("normalizeVersion(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	content := []byte("hello world")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])

	t.Run("match", func(t *testing.T) {
		checksumFile := []byte(fmt.Sprintf("%s  reasonix-linux-amd64.tar.gz\n", hash))
		if err := verifyChecksum(content, "reasonix-linux-amd64.tar.gz", checksumFile); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		checksumFile := []byte(fmt.Sprintf("%s  reasonix-linux-amd64.tar.gz\n", "0000000000000000000000000000000000000000000000000000000000000000"))
		if err := verifyChecksum(content, "reasonix-linux-amd64.tar.gz", checksumFile); err == nil {
			t.Error("expected checksum mismatch error")
		}
	})

	t.Run("not found", func(t *testing.T) {
		checksumFile := []byte(fmt.Sprintf("%s  reasonix-darwin-arm64.tar.gz\n", hash))
		if err := verifyChecksum(content, "reasonix-linux-amd64.tar.gz", checksumFile); err == nil {
			t.Error("expected not-found error")
		}
	})
}

func TestExtractFromTarGz(t *testing.T) {
	// Build a .tar.gz in memory containing a "reasonix" entry.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte("fake binary content")
	if err := tw.WriteHeader(&tar.Header{
		Name: "reasonix",
		Mode: 0o755,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractFromTarGz(buf.Bytes(), "reasonix")
	if err != nil {
		t.Fatalf("extractFromTarGz: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("extracted body = %q, want %q", got, body)
	}
}

func TestExtractFromTarGz_Nested(t *testing.T) {
	// Archives from goreleaser have the binary at the root with its name.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte("nested binary")
	if err := tw.WriteHeader(&tar.Header{
		Name: "reasonix-linux-amd64/reasonix",
		Mode: 0o755,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractFromTarGz(buf.Bytes(), "reasonix")
	if err != nil {
		t.Fatalf("extractFromTarGz: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("extracted body = %q, want %q", got, body)
	}
}

func TestExtractFromTarGz_NotFound(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{
		Name: "other-file.txt",
		Mode: 0o644,
		Size: 3,
	}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte("foo"))
	tw.Close()
	gw.Close()

	_, err := extractFromTarGz(buf.Bytes(), "reasonix")
	if err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestIsCLITag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"v1.6.0", true},
		{"v0.1.0", true},
		{"v2.0.0-rc.1", true},
		{"desktop-v1.5.0", false},
		{"npm-v1.4.0", false},
		{"", false},
		{"v", false},
	}
	for _, tt := range tests {
		if got := isCLITag(tt.tag); got != tt.want {
			t.Errorf("isCLITag(%q) = %v, want %v", tt.tag, got, tt.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{500, "500 B"},
		{2048, "2.0 KiB"},
		{19_000_000, "18.1 MiB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.bytes); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestPickCLIRelease(t *testing.T) {
	pick := func(rels []ghRelease) string {
		if r := pickCLIRelease(rels); r != nil {
			return r.TagName
		}
		return ""
	}

	// Skips foreign namespaces (GitHub's "latest" can be a desktop-v release).
	mixed := []ghRelease{
		{TagName: "desktop-v1.6.0"},
		{TagName: "npm-v1.4.0"},
		{TagName: "v1.6.0"},
	}
	if got := pick(mixed); got != "v1.6.0" {
		t.Errorf("foreign namespaces: got %q, want v1.6.0", got)
	}

	// The 1.x line ships as rc on npm @next, so a newer prerelease must be
	// selected, not skipped — `reasonix upgrade` always moves to the newest 1.x.
	withRC := []ghRelease{
		{TagName: "v1.7.0-rc.1"},
		{TagName: "v1.6.0"},
	}
	if got := pick(withRC); got != "v1.7.0-rc.1" {
		t.Errorf("newest 1.x (incl. rc) must win: got %q, want v1.7.0-rc.1", got)
	}

	if got := pick([]ghRelease{{TagName: "desktop-v1.0.0"}}); got != "" {
		t.Errorf("no CLI release should return nil, got %q", got)
	}
}
