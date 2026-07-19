package mcpcatalog

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"aead.dev/minisign"
)

func signedIndex(t *testing.T, idx Index) ([]byte, []byte, string) {
	t.Helper()
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubText, err := pub.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	return data, minisign.Sign(priv, data), string(pubText)
}

func emptyIndex(sequence uint64) Index {
	return Index{SchemaVersion: SchemaVersion, Sequence: sequence}
}

func TestVerifyRejectsTamperingAndWrongKey(t *testing.T) {
	data, sig, key := signedIndex(t, emptyIndex(1))
	idx, err := Verify(data, sig, []string{key})
	if err != nil || idx.Sequence != 1 {
		t.Fatalf("verify = %+v, %v", idx, err)
	}
	data[0] ^= 1
	if _, err := Verify(data, sig, []string{key}); err == nil {
		t.Fatal("tampered catalog verified")
	}
	otherData, _, otherKey := signedIndex(t, emptyIndex(1))
	if _, err := Verify(otherData, sig, []string{otherKey}); err == nil {
		t.Fatal("wrong-key signature verified")
	}
}

func TestValidateRejectsDuplicatesAndBadReaders(t *testing.T) {
	sha := strings.Repeat("a", 64)
	idx := emptyIndex(1)
	idx.Entries = []Entry{{ID: "x", Name: "p", Version: "1", Source: "https://example.test/p", Commit: strings.Repeat("b", 40), PackageSHA256: sha, ManifestSHA256: sha, Servers: []Server{{Name: "s", Transport: "stdio", Readers: []string{"r", "r"}}}}}
	if err := Validate(idx); err == nil {
		t.Fatal("duplicate readers accepted")
	}
	idx.Entries[0].Servers[0].Readers = []string{"r"}
	idx.Entries = append(idx.Entries, idx.Entries[0])
	if err := Validate(idx); err == nil {
		t.Fatal("duplicate entry accepted")
	}
}

func TestRefreshKeepsLastGoodAndRejectsRollback(t *testing.T) {
	cache := t.TempDir()
	data2, sig2, key := signedIndex(t, emptyIndex(2))
	var data, sig = data2, sig2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".minisig") {
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	loader := Loader{CacheDir: cache, CatalogURL: server.URL + "/index.json", SignatureURL: server.URL + "/index.json.minisig", Keys: []string{key}}
	result, err := loader.Refresh(context.Background())
	if err != nil || result.Index.Sequence != 2 || result.Source != SourceRemote {
		t.Fatalf("refresh = %+v, %v", result, err)
	}

	data, sig, _ = signedIndex(t, emptyIndex(1))
	if _, err := loader.Refresh(context.Background()); err == nil {
		t.Fatal("catalog rollback accepted")
	}
	cached, err := loader.Load(context.Background(), false)
	if err != nil || cached.Index.Sequence != 2 || cached.Source != SourceCached {
		t.Fatalf("cached = %+v, %v", cached, err)
	}
}

func TestNewestLocalCatalogUsesHighestSequence(t *testing.T) {
	cached := Result{Index: emptyIndex(1), Source: SourceCached}
	bundled := Result{Index: emptyIndex(2), Source: SourceBundled}
	if got := newestCatalogResult(cached, bundled); got.Source != SourceBundled || got.Index.Sequence != 2 {
		t.Fatalf("newestCatalogResult(cached, bundled) = %+v", got)
	}
	if got := newestCatalogResult(bundled, cached); got.Source != SourceBundled || got.Index.Sequence != 2 {
		t.Fatalf("newestCatalogResult(bundled, cached) = %+v", got)
	}
	// Equal sequences prefer the app-bundled snapshot so an upgraded binary's
	// signed baseline cannot be shadowed by an older on-disk representation.
	equalCached := Result{Index: emptyIndex(2), Source: SourceCached}
	if got := newestCatalogResult(equalCached, bundled); got.Source != SourceBundled {
		t.Fatalf("equal sequence selected %q, want bundled", got.Source)
	}
}

func TestCachedCatalogUsesOneAtomicEnvelope(t *testing.T) {
	cache := t.TempDir()
	data, sig, key := signedIndex(t, emptyIndex(3))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".minisig") {
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	loader := Loader{CacheDir: cache, CatalogURL: server.URL + "/index.json", SignatureURL: server.URL + "/index.json.minisig", Keys: []string{key}}
	if _, err := loader.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(cacheEnvelopePath(cache)); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("atomic LKG envelope = %v, %v", info, err)
	}
	dataPath, sigPath := CachePaths(cache)
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("refresh wrote split data cache: %v", err)
	}
	if _, err := os.Stat(sigPath); !os.IsNotExist(err) {
		t.Fatalf("refresh wrote split signature cache: %v", err)
	}
}

func TestTreeSHA256StableAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := TreeSHA256(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := TreeSHA256(root)
	if err != nil || first != second {
		t.Fatalf("tree digest = %q %q, %v", first, second, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(root, "b.txt"), 0o755); err != nil {
			t.Fatal(err)
		}
		modeChanged, err := TreeSHA256(root)
		if err != nil || modeChanged != first {
			t.Fatalf("platform-specific mode changed portable tree digest: %q != %q, %v", modeChanged, first, err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "b.txt"), filepath.Join(root, "link")); err == nil {
		if _, err := TreeSHA256(root); err == nil {
			t.Fatal("symlink accepted")
		}
	}
}

func TestRuntimeRevocationSnapshotReplacesPreviousState(t *testing.T) {
	rememberRuntimeIndex(Index{Revocations: []Revocation{{EntryID: "entry-a"}}})
	defer rememberRuntimeIndex(Index{})
	if !RuntimeEntryRevoked("entry-a") || RuntimeEntryRevoked("entry-b") {
		t.Fatal("runtime revocation snapshot was not applied")
	}
	rememberRuntimeIndex(Index{Revocations: []Revocation{{EntryID: "entry-b"}}})
	if RuntimeEntryRevoked("entry-a") || !RuntimeEntryRevoked("entry-b") {
		t.Fatal("runtime revocation snapshot was not replaced")
	}
}
