package extension

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionExtensionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")

	store, err := LoadSessionExtensions(sessionPath)
	if err != nil {
		t.Fatalf("LoadSessionExtensions: %v", err)
	}
	if _, ok := store.Get("missing"); ok {
		t.Fatal("empty store returned a blob")
	}
	if err := store.Set("alpha", json.RawMessage(`{"count":3,"notes":["a","b"]}`)); err != nil {
		t.Fatalf("Set alpha: %v", err)
	}
	if err := store.Set("beta", json.RawMessage(`{"enabled":true}`)); err != nil {
		t.Fatalf("Set beta: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := LoadSessionExtensions(sessionPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	alpha, ok := reloaded.Get("alpha")
	if !ok || string(alpha) != `{"count":3,"notes":["a","b"]}` {
		t.Fatalf("alpha blob = %q (ok=%v)", alpha, ok)
	}
	beta, ok := reloaded.Get("beta")
	if !ok || string(beta) != `{"enabled":true}` {
		t.Fatalf("beta blob = %q (ok=%v)", beta, ok)
	}
	if got := reloaded.Plugins(); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("Plugins() = %v", got)
	}

	// The on-disk shape is the versioned contract.
	raw, err := os.ReadFile(SessionExtensionsPath(sessionPath))
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	var file struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &file); err != nil || file.Version != SessionExtensionsVersion {
		t.Fatalf("store file version = %d, err = %v", file.Version, err)
	}

	// Delete survives a save/reload cycle.
	reloaded.Delete("alpha")
	if err := reloaded.Save(); err != nil {
		t.Fatalf("Save after delete: %v", err)
	}
	again, err := LoadSessionExtensions(sessionPath)
	if err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if _, ok := again.Get("alpha"); ok {
		t.Fatal("deleted blob survived save/reload")
	}
}

func TestSessionExtensionsBlobCap(t *testing.T) {
	store, err := LoadSessionExtensions(filepath.Join(t.TempDir(), "s.jsonl"))
	if err != nil {
		t.Fatalf("LoadSessionExtensions: %v", err)
	}
	// Exactly at the cap: a JSON string sized to MaxPluginStateBytes.
	atCap := []byte(`"` + strings.Repeat("a", MaxPluginStateBytes-2) + `"`)
	if err := store.Set("alpha", atCap); err != nil {
		t.Fatalf("Set at the cap: %v", err)
	}
	overCap := []byte(`"` + strings.Repeat("a", MaxPluginStateBytes-1) + `"`)
	err = store.Set("alpha", overCap)
	if err == nil {
		t.Fatal("Set beyond the cap succeeded")
	}
	if !errors.Is(err, ErrPluginStateTooLarge) {
		t.Fatalf("error = %v, want ErrPluginStateTooLarge", err)
	}
	// The failed write must not have clobbered the previous blob.
	got, ok := store.Get("alpha")
	if !ok || len(got) != MaxPluginStateBytes {
		t.Fatalf("staged blob after rejected write: %d bytes (ok=%v)", len(got), ok)
	}
	// Invalid JSON is rejected too.
	if err := store.Set("beta", json.RawMessage(`{not json`)); err == nil {
		t.Fatal("Set accepted invalid JSON")
	}
	if err := store.Set("", json.RawMessage(`{}`)); err == nil {
		t.Fatal("Set accepted an empty plugin ID")
	}
}

func TestSessionExtensionsCorruptFileTolerated(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	storePath := SessionExtensionsPath(sessionPath)
	if err := os.WriteFile(storePath, []byte("{corrupt json!!!"), 0o644); err != nil {
		t.Fatalf("write corrupt store: %v", err)
	}
	store, err := LoadSessionExtensions(sessionPath)
	if err != nil {
		t.Fatalf("corrupt store must not fail the load: %v", err)
	}
	if len(store.Plugins()) != 0 {
		t.Fatalf("corrupt store produced plugins %v", store.Plugins())
	}
	// Saving over the corrupt file repairs it.
	if err := store.Set("alpha", json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := LoadSessionExtensions(sessionPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Get("alpha"); !ok {
		t.Fatal("repaired store lost the blob")
	}
}

// TestSessionExtensionsNeverTouchesSessionJSONL pins the byte-stability
// contract: the session file itself is never read or written by the store.
func TestSessionExtensionsNeverTouchesSessionJSONL(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	sessionBytes := []byte("{\"role\":\"system\"}\n{\"role\":\"user\"}\n")
	if err := os.WriteFile(sessionPath, sessionBytes, 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	store, err := LoadSessionExtensions(sessionPath)
	if err != nil {
		t.Fatalf("LoadSessionExtensions: %v", err)
	}
	if err := store.Set("alpha", json.RawMessage(`{"state":1}`)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	after, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !bytes.Equal(after, sessionBytes) {
		t.Fatal("session JSONL bytes changed")
	}
	if _, err := os.Stat(SessionExtensionsPath(sessionPath)); err != nil {
		t.Fatalf("extensions file missing: %v", err)
	}
}

func TestSessionExtensionsEmptySessionPath(t *testing.T) {
	store, err := LoadSessionExtensions("")
	if err != nil {
		t.Fatalf("LoadSessionExtensions: %v", err)
	}
	if err := store.Save(); err == nil {
		t.Fatal("Save without a path succeeded")
	}
}
