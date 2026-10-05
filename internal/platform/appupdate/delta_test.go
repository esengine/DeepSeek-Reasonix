package appupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
