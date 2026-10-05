package releaseasset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// cacheServer serves one release and counts what was asked of it, which is the
// only way to tell a cache hit from a very fast download.
func cacheServer(t *testing.T, archive []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	digest := sha256.Sum256(archive)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/studio-v2.11.0/reasonix-linux-arm64.tar.gz":
			hits.Add(1)
			_, _ = w.Write(archive)
		case "/studio-v2.11.0/SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%s  reasonix-linux-arm64.tar.gz\n", hex.EncodeToString(digest[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// The point of the cache: the second machine on a platform costs no download.
func TestCachedDownloadServesTheSecondCallFromDisk(t *testing.T) {
	binary := []byte("reasonix-binary")
	server, hits := cacheServer(t, testCLIArchive(t, binary))
	dir := t.TempDir()

	for i := range 3 {
		got, err := cachedDownloadFromBase(context.Background(), server.Client(), server.URL, dir,
			StudioLine, "v2.11.0", "linux", "arm64", false)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if string(got) != string(binary) {
			t.Fatalf("call %d returned %q", i, got)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("archive was downloaded %d times, want 1", n)
	}
}

// A cached file that does not match its digest is a miss, not an answer: a
// truncated write must cost a download, never a binary that will not run.
func TestCachedDownloadRefusesAMismatchedCopy(t *testing.T) {
	binary := []byte("reasonix-binary")
	server, hits := cacheServer(t, testCLIArchive(t, binary))
	dir := t.TempDir()

	if _, err := cachedDownloadFromBase(context.Background(), server.Client(), server.URL, dir,
		StudioLine, "v2.11.0", "linux", "arm64", false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cli", "studio-v2.11.0", "linux-arm64")
	if err := os.WriteFile(path, []byte("truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := cachedDownloadFromBase(context.Background(), server.Client(), server.URL, dir,
		StudioLine, "v2.11.0", "linux", "arm64", false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Fatalf("a corrupted cache was returned as the binary: %q", got)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("downloads = %d, want 2: the corrupted copy had to be replaced", n)
	}
}

// Each line and each target keeps its own entry: one cache holding "the CLI"
// would hand a Studio kernel to a caller that asked the other line for one.
func TestCachedDownloadKeepsLinesAndTargetsApart(t *testing.T) {
	server, _ := cacheServer(t, testCLIArchive(t, []byte("reasonix-binary")))
	dir := t.TempDir()
	if _, err := cachedDownloadFromBase(context.Background(), server.Client(), server.URL, dir,
		StudioLine, "v2.11.0", "linux", "arm64", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cli", "studio-v2.11.0", "linux-arm64")); err != nil {
		t.Errorf("the studio line's entry is not under its own tag: %v", err)
	}
	// The CLI line's tag for the same version is a different directory.
	if _, err := os.Stat(filepath.Join(dir, "cli", "v2.11.0", "linux-arm64")); err == nil {
		t.Error("the entry was written under the bare version, which is another line's tag")
	}
}

// A version with no release names no directory: the refusal comes before dir is
// joined with anything the caller supplied.
func TestCachedDownloadRefusesAnUnreleasableVersionWithoutTouchingTheCache(t *testing.T) {
	dir := t.TempDir()
	if _, err := DownloadCLI(context.Background(), http.DefaultClient, dir,
		StudioLine, "dev", "linux", "arm64"); err == nil {
		t.Fatal("a source build was accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the cache directory was written to: %v", entries)
	}
}
