package appupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/delta"
	"reasonix/internal/platform/update"
)

// A delta that cannot be proven is not applied: the install is left alone, no
// swap is started, and the full package is what the update falls back to.
func TestAnUnverifiableDeltaFallsBackToTheFullPackage(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	raw, err := delta.Index{SchemaVersion: delta.SchemaVersion, Version: "v2.0.0", Platform: update.CurrentPlatform()}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	packed, err := delta.PackIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	var chunkHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json.zst":
			_, _ = w.Write(packed)
		case "/index.json.zst.minisig":
			_, _ = w.Write([]byte("untrusted comment: not a signature\nAAAA\n"))
		default:
			chunkHits++
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	sum := sha256.Sum256(packed)
	m := &update.Manifest{Deltas: map[string]update.Delta{update.CurrentPlatform(): {
		Index:  update.Asset{URL: srv.URL + "/index.json.zst", Sig: srv.URL + "/index.json.zst.minisig", SHA256: hex.EncodeToString(sum[:])},
		Chunks: srv.URL,
	}}}
	root, cache := testenv.TempDir(t), testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "app.exe"), []byte("installed"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Owner: stubOwner{}, Running: "v1.0.0", Application: update.Application{PID: 1}}).(*capability)
	install := update.Install{Version: "v1.0.0", Layout: update.Layout{Root: root, Executable: filepath.Join(root, "app.exe")}}
	_, err = c.tryDelta(t.Context(), install, "v2.0.0", cache, m)
	if err == nil {
		t.Fatal("a delta with a bad signature was applied")
	}
	if update.TreeHandoffSupported() && deltaCode(err) != DeltaMismatch {
		t.Fatalf("abandoned as %q, want %q", deltaCode(err), DeltaMismatch)
	}
	if chunkHits != 0 {
		t.Fatalf("%d chunks were fetched for an index that never verified", chunkHits)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "app.exe")); string(b) != "installed" {
		t.Fatal("the install changed")
	}
	if matches, _ := filepath.Glob(filepath.Join(cache, "delta", "*", "*.handoff.json")); len(matches) != 0 {
		t.Fatalf("a swap was planned: %v", matches)
	}
}

func deltaFixture(t *testing.T, installed map[string]string, files map[string]string) (delta.Index, update.Delta, *int, update.Install, string) {
	t.Helper()
	store := map[string][]byte{}
	x := delta.Index{SchemaVersion: delta.SchemaVersion, Version: "v2.0.0", Platform: update.CurrentPlatform()}
	for name, body := range files {
		h := delta.HashOf([]byte(body))
		store["/"+delta.ObjectName(h)] = delta.Compress([]byte(body))
		sum := sha256.Sum256([]byte(body))
		x.Files = append(x.Files, delta.File{Path: name, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:]), Chunks: []delta.Chunk{{Hash: h, Size: len(body)}}})
	}
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		b, ok := store[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	root := testenv.TempDir(t)
	for name, body := range installed {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	install := update.Install{Version: "v1.0.0", Layout: update.Layout{Root: root}}
	return x, update.Delta{Chunks: srv.URL}, hits, install, testenv.TempDir(t)
}

func stageFixture(t *testing.T, x delta.Index, d update.Delta, install update.Install, cache string) error {
	t.Helper()
	c := New(Options{Owner: stubOwner{}, Running: "v1.0.0", Application: update.Application{PID: 1}}).(*capability)
	tr, err := c.deltaTransport()
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.stageFromIndex(t.Context(), tr, install, "v2.0.0", cache, d, x, filepath.Join(cache, "backup"))
	return err
}

// A release most of whose bytes this install lacks is the full package's job:
// no chunk is requested, and the reason is a typed code.
func TestADeltaLargerThanHalfTheReleaseFetchesNothing(t *testing.T) {
	x, d, hits, install, cache := deltaFixture(t,
		map[string]string{"a.bin": "same-bytes-here"},
		map[string]string{"a.bin": "same-bytes-here", "b.bin": "brand new bytes that outweigh what is kept"})
	err := stageFixture(t, x, d, install, cache)
	if deltaCode(err) != DeltaTooLarge {
		t.Fatalf("abandoned as %q (%v), want %q", deltaCode(err), err, DeltaTooLarge)
	}
	if *hits != 0 {
		t.Fatalf("%d chunks were fetched for a delta bigger than the full package", *hits)
	}
}

// A release the install mostly holds still takes the chunked path.
func TestADeltaUnderHalfTheReleaseStillFetchesItsChunks(t *testing.T) {
	x, d, hits, install, cache := deltaFixture(t,
		map[string]string{"a.bin": "kept bytes that make up most of the release tree"},
		map[string]string{"a.bin": "kept bytes that make up most of the release tree", "b.bin": "new"})
	err := stageFixture(t, x, d, install, cache)
	if deltaCode(err) == DeltaTooLarge {
		t.Fatalf("a small delta was refused: %v", err)
	}
	if *hits != 1 {
		t.Fatalf("%d chunks fetched, want the one missing", *hits)
	}
}

// The delta is looked up by this install's own platform key. A release that
// offers one only for another architecture fetches nothing and falls back to the
// full package; the same offer under this key is where the fetch starts.
func TestTheDeltaIsLookedUpByThisInstallsPlatformKey(t *testing.T) {
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	root, cache := testenv.TempDir(t), testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "app.exe"), []byte("installed"), 0o644); err != nil {
		t.Fatal(err)
	}
	install := update.Install{Version: "v1.0.0", Layout: update.Layout{Root: root, Executable: filepath.Join(root, "app.exe")}}
	try := func(key string) (indexFetches int, err error) {
		t.Setenv("REASONIX_HOME", testenv.TempDir(t))
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/index.json.zst" {
				indexFetches++
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()
		m := &update.Manifest{Deltas: map[string]update.Delta{key: {
			Index:  update.Asset{URL: srv.URL + "/index.json.zst", Sig: srv.URL + "/index.json.zst.minisig"},
			Chunks: srv.URL,
		}}}
		c := New(Options{Owner: stubOwner{}, Running: "v1.0.0", Application: update.Application{PID: 1}}).(*capability)
		_, err = c.tryDelta(t.Context(), install, "v2.0.0", cache, m)
		return indexFetches, err
	}

	if fetches, err := try(update.PlatformKey(runtime.GOOS, other)); !errors.Is(err, errNoDelta) || fetches != 0 {
		t.Fatalf("a %s-only delta: err %v, %d index fetches; want errNoDelta and none", other, err, fetches)
	}
	if !update.TreeHandoffSupported() {
		return
	}
	if fetches, err := try(update.CurrentPlatform()); errors.Is(err, errNoDelta) || fetches == 0 {
		t.Fatalf("this platform's delta: err %v, %d index fetches; want an attempt", err, fetches)
	}
}
