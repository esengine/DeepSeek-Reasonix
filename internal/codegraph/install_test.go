package codegraph

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAssetNameForCurrentPlatform(t *testing.T) {
	got := assetName()
	if !strings.HasPrefix(got, "codegraph-") {
		t.Fatalf("assetName %q lacks codegraph- prefix", got)
	}
	wantExt := ".tar.gz"
	if runtime.GOOS == "windows" {
		wantExt = ".zip"
	}
	if !strings.HasSuffix(got, wantExt) {
		t.Fatalf("assetName %q should end in %s on %s", got, wantExt, runtime.GOOS)
	}
}

func TestPromoteReplacesStalePartialDest(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, ".dl-x", "codegraph-x64")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "codegraph"), []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(parent, "v0.9.7")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := promote(root, dir); err != nil {
		t.Fatalf("promote over a stale dest: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "bin", "codegraph")); err != nil || string(got) != "new" {
		t.Fatalf("dest missing promoted bundle: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.txt")); !os.IsNotExist(err) {
		t.Fatal("stale dest content survived promote")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("root should have been moved into dir")
	}
}

func TestSha256For(t *testing.T) {
	sums := "abc123  codegraph-linux-x64.tar.gz\ndef456  codegraph-darwin-arm64.tar.gz\n"
	got, err := sha256For(sums, "codegraph-darwin-arm64.tar.gz")
	if err != nil || got != "def456" {
		t.Fatalf("sha256For = %q, %v; want def456", got, err)
	}
	if _, err := sha256For(sums, "codegraph-win32-x64.zip"); err == nil {
		t.Fatal("expected error for absent asset")
	}
}

func TestResolveWithinRejectsTraversal(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveWithin(root, "../escape"); err == nil {
		t.Fatal("resolveWithin should reject ../escape")
	}
	if _, err := resolveWithin(root, "bin/codegraph"); err != nil {
		t.Fatalf("resolveWithin rejected a normal path: %v", err)
	}
}

// makeTarGz builds an in-memory .tar.gz from name->(content, mode); a mode with
// the exec bit is preserved through extraction.
func makeTarGz(t *testing.T, files map[string]struct {
	body string
	mode int64
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractTarGzPreservesExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec-bit semantics are POSIX")
	}
	data := makeTarGz(t, map[string]struct {
		body string
		mode int64
	}{
		"bin/codegraph": {"#!/bin/sh\n", 0o755},
		"lib/app.js":    {"console.log(1)", 0o644},
	})
	dir := t.TempDir()
	if err := extractTarGz(data, dir); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, "bin", "codegraph"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Fatal("launcher lost its exec bit on extraction")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "lib", "app.js"))
	if string(b) != "console.log(1)" {
		t.Fatalf("lib/app.js content = %q", b)
	}
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	data := makeTarGz(t, map[string]struct {
		body string
		mode int64
	}{"../evil": {"x", 0o644}})
	if err := extractTarGz(data, t.TempDir()); err == nil {
		t.Fatal("extractTarGz should reject a ../ entry")
	}
}

func TestInstallReturnsCachedWithoutNetwork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX +x launcher")
	}
	base := t.TempDir()
	t.Setenv("REASONIX_CACHE_DIR", base)
	// Seed a fake cached launcher so Install short-circuits before any download.
	launcher := filepath.Join(CacheDir(), "bin", "codegraph")
	if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Install(context.Background(), nil)
	if err != nil || got != launcher {
		t.Fatalf("Install with a populated cache = %q, %v; want %q", got, err, launcher)
	}
	// Resolve should also find it (no override, cache wins).
	if p, ok := Resolve(""); !ok || p != launcher {
		t.Fatalf("Resolve = %q, %v; want %q", p, ok, launcher)
	}
}

func TestDownloadAssetUsesEmbeddedChecksumAndMirrorFallback(t *testing.T) {
	asset := assetName()
	body := []byte("verified codegraph payload")
	sum := sha256.Sum256(body)
	prev, hadPrev := releaseAssetSHA256[asset]
	releaseAssetSHA256[asset] = fmt.Sprintf("%x", sum)
	t.Cleanup(func() {
		if hadPrev {
			releaseAssetSHA256[asset] = prev
		} else {
			delete(releaseAssetSHA256, asset)
		}
	})

	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/"+asset) {
			t.Fatalf("download path = %q, want asset suffix", r.URL.Path)
		}
		w.Write(body)
	}))
	defer good.Close()

	got, err := downloadAssetFromBases(context.Background(), http.DefaultClient, asset, fmt.Sprintf("%x", sum), []string{bad.URL, good.URL}, nil)
	if err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("downloadAsset body = %q, want %q", got, body)
	}
}

func TestDownloadAssetRejectsMirrorChecksumMismatch(t *testing.T) {
	asset := assetName()
	prev, hadPrev := releaseAssetSHA256[asset]
	releaseAssetSHA256[asset] = strings.Repeat("0", 64)
	t.Cleanup(func() {
		if hadPrev {
			releaseAssetSHA256[asset] = prev
		} else {
			delete(releaseAssetSHA256, asset)
		}
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tampered"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := downloadAssetFromBases(ctx, http.DefaultClient, asset, strings.Repeat("0", 64), []string{srv.URL}, nil); err == nil {
		t.Fatal("downloadAsset accepted a checksum mismatch")
	}
}

func TestDownloadBasesUseOfficialSourcesBeforeGitHub(t *testing.T) {
	got := downloadBases()
	want := []string{
		officialMirrorBase + "/" + Version,
		fmt.Sprintf("https://github.com/%s/releases/download/%s", cgRepo, Version),
	}
	if len(got) != len(want) {
		t.Fatalf("downloadBases = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("downloadBases[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestHTTPGetDetachedCtxSurvivesParentCancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-time.After(300 * time.Millisecond):
		case <-r.Context().Done():
		}
		w.Write([]byte("payload"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var got []byte
	var gotErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		got, gotErr = httpGet(context.WithoutCancel(ctx), http.DefaultClient, srv.URL)
	}()

	<-started
	cancel()
	wg.Wait()

	if gotErr != nil {
		t.Fatalf("detached download aborted after parent cancel: %v", gotErr)
	}
	if string(got) != "payload" {
		t.Fatalf("got %q, want payload", got)
	}
}

func TestHTTPGetPlainCtxAbortsOnParentCancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var gotErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, gotErr = httpGet(ctx, http.DefaultClient, srv.URL)
	}()

	<-started
	cancel()
	wg.Wait()

	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("plain ctx should abort on parent cancel, got %v", gotErr)
	}
}
