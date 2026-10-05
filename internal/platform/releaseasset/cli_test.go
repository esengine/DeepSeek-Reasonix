package releaseasset

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownloadCLIFromBaseVerifiesAndExtracts(t *testing.T) {
	binary := []byte("reasonix-binary")
	archive := testCLIArchive(t, binary)
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.2.3/reasonix-linux-arm64.tar.gz":
			_, _ = w.Write(archive)
		case "/v1.2.3/SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%s  reasonix-linux-arm64.tar.gz\n", hex.EncodeToString(digest[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := downloadCLIFromBase(context.Background(), server.Client(), server.URL, "v1.2.3", "linux", "arm64", false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("binary = %q, want %q", got, binary)
	}
}

func TestDownloadCLIFromBaseRejectsChecksumMismatch(t *testing.T) {
	archive := testCLIArchive(t, []byte("reasonix-binary"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.2.3/reasonix-linux-amd64.tar.gz" {
			_, _ = w.Write(archive)
			return
		}
		_, _ = fmt.Fprintf(w, "%064d  reasonix-linux-amd64.tar.gz\n", 0)
	}))
	defer server.Close()

	if _, err := downloadCLIFromBase(context.Background(), server.Client(), server.URL, "v1.2.3", "linux", "amd64", false); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
}

func TestDownloadCLIRejectsDevelopmentAndUnsupportedTargets(t *testing.T) {
	for _, test := range []struct{ version, goos, goarch string }{
		{"dev", "linux", "amd64"},
		{"v1.2.3", "plan9", "amd64"},
		{"v1.2.3", "linux", "riscv64"},
		{"v1.2.3", "windows", "riscv64"},
	} {
		if _, err := DownloadCLI(context.Background(), http.DefaultClient, "", StudioLine, test.version, test.goos, test.goarch); err == nil {
			t.Fatalf("DownloadCLI(%q,%q,%q) unexpectedly succeeded", test.version, test.goos, test.goarch)
		}
	}
}

func testCLIArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "reasonix", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Windows ships a zip holding reasonix.exe, and every other platform a tar.gz
// holding reasonix. Reading one with the other's assumptions finds nothing,
// which is how a Windows remote had no install path at all.
func TestDownloadCLIReadsTheWindowsZip(t *testing.T) {
	binary := []byte("reasonix-windows-binary")
	archive := testCLIZip(t, "reasonix.exe", binary)
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.2.3/reasonix-windows-amd64.zip":
			_, _ = w.Write(archive)
		case "/v1.2.3/SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%s  reasonix-windows-amd64.zip\n", hex.EncodeToString(digest[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := downloadCLIFromBase(context.Background(), server.Client(), server.URL, "v1.2.3", "windows", "amd64", false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("binary = %q, want %q", got, binary)
	}
}

// A zip that carries no executable must be reported, not silently returned
// empty: an empty binary uploaded to a remote is a serve that never starts.
func TestDownloadCLIRefusesAZipWithoutTheExecutable(t *testing.T) {
	archive := testCLIZip(t, "README.txt", []byte("nothing to run here"))
	if _, err := extractCLI(archive, "reasonix-windows-amd64.zip", "reasonix.exe"); err == nil {
		t.Fatal("a zip with no executable was accepted")
	}
}

func testCLIZip(t *testing.T, name string, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A version alone cannot say which release holds an asset: the CLI line tags a
// release v1.2.3 and the Studio line studio-v1.2.3. Resolving the second under
// the first's spelling is a 404 on a tag that was never published.
func TestLineNamesTheReleaseTag(t *testing.T) {
	binary := []byte("studio-line-binary")
	archive := testCLIArchive(t, binary)
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/studio-v2.11.0/reasonix-linux-arm64.tar.gz":
			_, _ = w.Write(archive)
		case "/studio-v2.11.0/SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%s  reasonix-linux-arm64.tar.gz\n", hex.EncodeToString(digest[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	got, err := downloadCLIFromBase(context.Background(), server.Client(), server.URL,
		StudioLine.tag("v2.11.0"), "linux", "arm64", false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("binary = %q, want %q", got, binary)
	}
	if tag := CLILine.tag("v1.2.3"); tag != "v1.2.3" {
		t.Errorf("CLI line tag = %q, want the bare version", tag)
	}
}
