package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// redirectCache points config.CacheDir() at a fresh temp dir for the duration
// of the test by overriding the knobs os.UserConfigDir reads: HOME/XDG_CONFIG_HOME
// on unix, APPDATA on Windows (without it the tests write the real user cache).
// Returns the temp dir so a test can also poke into it (e.g. write a corrupted file).
func redirectCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	return dir
}

func sampleSpec() Spec {
	return Spec{
		Name:    "my-server",
		Type:    "stdio",
		Command: "/usr/bin/example",
		Args:    []string{"--flag", "x"},
		Env:     map[string]string{"FOO": "1", "BAR": "2"},
		Headers: map[string]string{"X-Custom": "ok"},
		Dir:     "/work",
	}
}

func sampleCachedSchema(hash string) CachedSchema {
	return CachedSchema{
		SpecHash:     hash,
		Capabilities: map[string]bool{"prompts": true, "resources": false},
		Tools: []CachedTool{{
			Name:        "do_thing",
			Description: "does a thing",
			Schema:      json.RawMessage(`{"type":"object"}`),
			ReadOnly:    true,
		}},
	}
}

func TestCacheRoundTrip(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	hash := SpecFingerprint(spec)
	cs := sampleCachedSchema(hash)

	if err := SaveCachedSchema(spec.Name, cs); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
	got, ok := LoadCachedSchema(spec.Name, hash)
	if !ok {
		t.Fatal("LoadCachedSchema: miss after save")
	}
	if got.SpecHash != hash {
		t.Errorf("SpecHash: got %q want %q", got.SpecHash, hash)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "do_thing" {
		t.Errorf("Tools: %+v", got.Tools)
	}
	if !got.Tools[0].ReadOnly {
		t.Error("ReadOnly: lost across save/load")
	}
	if !got.Capabilities["prompts"] || got.Capabilities["resources"] {
		t.Errorf("Capabilities: %+v", got.Capabilities)
	}
	if got.Version != cacheVersion {
		t.Errorf("Version: got %d want %d", got.Version, cacheVersion)
	}
	if got.LastValidated.IsZero() {
		t.Error("LastValidated: expected non-zero after save")
	}
}

func TestCacheInvalidatesOnSpecHashMismatch(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	hash := SpecFingerprint(spec)
	if err := SaveCachedSchema(spec.Name, sampleCachedSchema(hash)); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
	if _, ok := LoadCachedSchema(spec.Name, "different-hash"); ok {
		t.Fatal("LoadCachedSchema: hit despite mismatching expectedHash")
	}
}

func TestCacheCorruptedFileReturnsFalse(t *testing.T) {
	redirectCache(t)
	p := cachePath("broken")
	if p == "" {
		t.Skip("cachePath unavailable in this environment")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LoadCachedSchema panicked on corrupt file: %v", r)
		}
	}()
	if _, ok := LoadCachedSchema("broken", "any"); ok {
		t.Fatal("LoadCachedSchema: hit on corrupt file")
	}
}

func TestCacheVersionMismatchReturnsFalse(t *testing.T) {
	// Pin the on-disk version to one we don't recognise: a future writer must
	// not poison an older binary's cache reads.
	redirectCache(t)
	spec := sampleSpec()
	hash := SpecFingerprint(spec)
	cs := sampleCachedSchema(hash)
	cs.Version = cacheVersion + 99
	p := cachePath(spec.Name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(cs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCachedSchema(spec.Name, hash); ok {
		t.Fatal("LoadCachedSchema: hit on future-version cache file")
	}
}

func TestSpecFingerprintStable(t *testing.T) {
	spec := sampleSpec()
	h1 := SpecFingerprint(spec)
	h2 := SpecFingerprint(spec)
	if h1 != h2 {
		t.Fatalf("SpecFingerprint not stable: %q vs %q", h1, h2)
	}

	// Reorder the env (build a new map; Go map iteration order is randomised
	// but a fresh map can incidentally iterate the same way, so we run a few
	// iterations to give the runtime a chance to shuffle).
	reordered := spec
	for i := 0; i < 32; i++ {
		reordered.Env = map[string]string{"BAR": "2", "FOO": "1"}
		if got := SpecFingerprint(reordered); got != h1 {
			t.Fatalf("SpecFingerprint changed when env was rebuilt: %q vs %q", got, h1)
		}
	}
}

func TestSpecFingerprintChangesOnCommandEdit(t *testing.T) {
	a := sampleSpec()
	b := a
	b.Command = "/usr/local/bin/other"
	if SpecFingerprint(a) == SpecFingerprint(b) {
		t.Fatal("SpecFingerprint did not change when Command changed")
	}

	c := a
	c.Args = append([]string{}, a.Args...)
	c.Args[0] = "--different"
	if SpecFingerprint(a) == SpecFingerprint(c) {
		t.Fatal("SpecFingerprint did not change when Args changed")
	}

	d := a
	d.Env = map[string]string{"FOO": "1", "BAR": "different"}
	if SpecFingerprint(a) == SpecFingerprint(d) {
		t.Fatal("SpecFingerprint did not change when env value changed")
	}
}

func TestCacheMissForUnknownName(t *testing.T) {
	redirectCache(t)
	if _, ok := LoadCachedSchema("never-saved", "anything"); ok {
		t.Fatal("LoadCachedSchema: hit for a name that was never saved")
	}
}

func TestSlugSafeForFilesystem(t *testing.T) {
	cases := map[string]string{
		"My Server!":           "my-server",
		"weird/name\\with:bad": "weird-name-with-bad",
		"":                     "_",
		"------":               "_",
		"a_b-c":                "a_b-c",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q want %q", in, got, want)
		}
	}
}
