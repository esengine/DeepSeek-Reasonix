package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/mcplaunch"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// redirectCache points config.CacheDir() at a fresh temp dir for the duration
// of the test (without it the tests write the real user cache).
// Returns the temp dir so a test can also poke into it (e.g. write a corrupted file).
func redirectCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("REASONIX_CACHE_HOME", dir)
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

func sampleCachedSchema(key string) CachedSchema {
	return CachedSchema{
		CacheKey:     key,
		Capabilities: map[string]bool{"prompts": true, "resources": false},
		Tools: []CachedTool{{
			Name:        "do_thing",
			Description: "does a thing",
			Schema:      json.RawMessage(`{"type":"object"}`),
			ReadOnly:    true,
			Destructive: true,
		}},
	}
}

func TestCacheRoundTrip(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	key := SchemaCacheKey(spec)
	cs := sampleCachedSchema(key)

	if err := SaveCachedSchema(spec.Name, cs); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
	body, err := os.ReadFile(cachePath(spec.Name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"spec_hash"`) || strings.Contains(string(body), `"cache_key"`) {
		t.Fatalf("schema cache JSON compatibility changed: %s", body)
	}
	got, ok := LoadCachedSchema(spec.Name, key)
	if !ok {
		t.Fatal("LoadCachedSchema: miss after save")
	}
	if got.CacheKey != key {
		t.Errorf("CacheKey: got %q want %q", got.CacheKey, key)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "do_thing" {
		t.Errorf("Tools: %+v", got.Tools)
	}
	if !got.Tools[0].ReadOnly {
		t.Error("ReadOnly: lost across save/load")
	}
	if !got.Tools[0].Destructive {
		t.Error("Destructive: lost across save/load")
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

func TestCachePersistsDeclaredReaderIndependentlyOfServerAuthorization(t *testing.T) {
	cached := cacheableToolsOf([]tool.Tool{&remoteTool{
		rawName: "search", schema: json.RawMessage(`{"type":"object"}`),
		declaredReadOnly: true, readOnly: false,
	}})
	if len(cached) != 1 || !cached[0].ReadOnly {
		t.Fatalf("cached tool = %+v, want the server-declared reader snapshot", cached)
	}
}

func TestCachedToolSafetyTracksReadOnlyAndDestructiveHintsOnly(t *testing.T) {
	redirectCache(t)
	spec := Spec{
		Name: "cached-reader", Type: "http", URL: "https://example.com/mcp",
	}
	reader := CachedTool{
		Name: "search", Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`), ReadOnly: true,
	}
	if err := SaveCachedSchema(spec.Name, CachedSchema{CacheKey: SchemaCacheKey(spec), Tools: []CachedTool{reader}}); err != nil {
		t.Fatal(err)
	}
	before, found := CachedToolSafetyForSpec(spec, "search")
	if !found || !before.ReadOnly {
		t.Fatalf("server hint = (%+v,%v), want reader metadata", before, found)
	}
	after, found := CachedToolSafetyForSpec(spec, "search")
	if !found || !after.ReadOnly {
		t.Fatalf("explicit reader = (%+v,%v), want reader metadata", after, found)
	}

	// Input/output schema changes are compatibility facts, not authorization or
	// execution-safety decisions. The live server validates the current call and
	// the refreshed schema cache becomes provider-visible next session.
	reader.Schema = json.RawMessage(`{"type":"object","properties":{"q":{"type":"number"}}}`)
	reader.Destructive = true
	if err := SaveCachedSchema(spec.Name, CachedSchema{CacheKey: SchemaCacheKey(spec), Tools: []CachedTool{reader}}); err != nil {
		t.Fatal(err)
	}
	updated, found := CachedToolSafetyForSpec(spec, "search")
	if !found || !updated.ReadOnly || !updated.Destructive {
		t.Fatalf("safety update = (%+v,%v), want read-only/destructive hints", updated, found)
	}
}

func TestCacheLoadsLegacyToolWithoutDestructiveField(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	hash := SchemaCacheKey(spec)
	p := cachePath(spec.Name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":2,"spec_hash":"` + hash + `","capabilities":{},"tools":[{"name":"read","description":"legacy","schema":{"type":"object"},"read_only":true}],"last_validated":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(p, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := LoadCachedSchema(spec.Name, hash)
	if !ok || len(got.Tools) != 1 {
		t.Fatalf("legacy cache = (%+v,%v), want one tool", got, ok)
	}
	if got.Tools[0].Destructive {
		t.Fatal("legacy cache without destructive field must default to false")
	}
}

func TestCacheLoadQuarantinesMalformedToolSchema(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	hash := SchemaCacheKey(spec)
	cs := sampleCachedSchema(hash)
	cs.Tools = append(cs.Tools, CachedTool{
		Name: "generate_yso_bytes",
		Schema: json.RawMessage(`{
			"type":"object",
			"properties":{"options":{"type":"array","items":{"key":{"type":"string"},"type":{"type":"string"},"value":{"type":"string"}}}}
		}`),
	})

	if err := SaveCachedSchema(spec.Name, cs); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
	got, ok := LoadCachedSchema(spec.Name, hash)
	if !ok {
		t.Fatal("LoadCachedSchema: miss after save")
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "do_thing" {
		t.Fatalf("cached tools = %+v, want only valid do_thing", got.Tools)
	}
	if schema := string(got.Tools[0].Schema); schema != `{"properties":{},"type":"object"}` {
		t.Fatalf("valid cached schema = %s", schema)
	}
}

func TestCacheInvalidatesOnSchemaCacheKeyMismatch(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	key := SchemaCacheKey(spec)
	if err := SaveCachedSchema(spec.Name, sampleCachedSchema(key)); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
	if _, ok := LoadCachedSchema(spec.Name, "different-cache-key"); ok {
		t.Fatal("LoadCachedSchema: hit despite mismatching expectedKey")
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
	hash := SchemaCacheKey(spec)
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

func TestSchemaCacheKeyStable(t *testing.T) {
	spec := sampleSpec()
	h1 := SchemaCacheKey(spec)
	h2 := SchemaCacheKey(spec)
	if h1 != h2 {
		t.Fatalf("SchemaCacheKey not stable: %q vs %q", h1, h2)
	}

	// Reorder the env (build a new map; Go map iteration order is randomised
	// but a fresh map can incidentally iterate the same way, so we run a few
	// iterations to give the runtime a chance to shuffle).
	reordered := spec
	for i := 0; i < 32; i++ {
		reordered.Env = map[string]string{"BAR": "2", "FOO": "1"}
		if got := SchemaCacheKey(reordered); got != h1 {
			t.Fatalf("SchemaCacheKey changed when env was rebuilt: %q vs %q", got, h1)
		}
	}
}

func TestSchemaCacheKeyIgnoresHostLocalAuthorizationAndIsolation(t *testing.T) {
	base := sampleSpec()
	changed := base
	changed.LaunchManager = mcplaunch.NewManager(filepath.Join(t.TempDir(), mcplaunch.StateFilename), "/workspace")
	changed.ConfigSource = "project:.mcp.json"
	changed.Package = "figma"
	changed.Sandbox = sandbox.Spec{Mode: "enforce", Network: true, WriteRoots: []string{"/workspace"}, MinimalWrites: true}
	changed.StateDir = "/host/state"
	if got, want := SchemaCacheKey(changed), SchemaCacheKey(base); got != want {
		t.Fatalf("host-local security state changed schema cache key: %q != %q", got, want)
	}
}

func TestSchemaCacheKeyTracksNonSecretSpecIdentityOnly(t *testing.T) {
	a := sampleSpec()
	renamed := a
	renamed.Name = "other-server"
	if SchemaCacheKey(a) == SchemaCacheKey(renamed) {
		t.Fatal("SchemaCacheKey did not change when server name changed")
	}

	b := a
	b.Command = "/usr/local/bin/other"
	if SchemaCacheKey(a) == SchemaCacheKey(b) {
		t.Fatal("SchemaCacheKey did not change when Command changed")
	}

	c := a
	c.Args = append([]string{}, a.Args...)
	c.Args[0] = "--different"
	if SchemaCacheKey(a) == SchemaCacheKey(c) {
		t.Fatal("SchemaCacheKey did not change when Args changed")
	}

	d := a
	d.Env = map[string]string{"FOO": "1", "BAR": "different"}
	if SchemaCacheKey(a) != SchemaCacheKey(d) {
		t.Fatal("SchemaCacheKey changed when an environment credential value rotated")
	}

	e := a
	e.Env = map[string]string{"FOO": "1", "NEW_KEY": "2"}
	if SchemaCacheKey(a) == SchemaCacheKey(e) {
		t.Fatal("SchemaCacheKey did not change when environment key names changed")
	}

	f := a
	f.Headers = map[string]string{"X-Custom": "rotated-secret"}
	if SchemaCacheKey(a) != SchemaCacheKey(f) {
		t.Fatal("SchemaCacheKey changed when a header credential value rotated")
	}

	g := a
	g.Headers = map[string]string{"Authorization": "secret"}
	if SchemaCacheKey(a) == SchemaCacheKey(g) {
		t.Fatal("SchemaCacheKey did not change when header key names changed")
	}

	h := a
	h.Type = "http"
	h.URL = "https://user:secret@example.com/mcp?access_token=first&workspace=one"
	i := h
	i.URL = "https://other:rotated@example.com/mcp?access_token=second&workspace=two"
	if SchemaCacheKey(h) == SchemaCacheKey(i) {
		t.Fatal("SchemaCacheKey did not bind URL credential/query values")
	}
}

func TestCacheMissForUnknownName(t *testing.T) {
	redirectCache(t)
	if _, ok := LoadCachedSchema("never-saved", "anything"); ok {
		t.Fatal("LoadCachedSchema: hit for a name that was never saved")
	}
}

func TestSlugSafeForFilesystem(t *testing.T) {
	if got := slug("a_b-c"); got != "a_b-c" {
		t.Fatalf("safe slug changed: %q", got)
	}
	inputs := []string{"My Server!", "my-server", "weird/name\\with:bad", "weird-name-with-bad", "", "------", "Foo", "foo"}
	seen := map[string]string{}
	for _, in := range inputs {
		got := slug(in)
		if got == "" || strings.ContainsAny(got, `/\\:`) {
			t.Fatalf("slug(%q) is not filesystem-safe: %q", in, got)
		}
		if previous, exists := seen[got]; exists {
			t.Fatalf("slug collision: %q and %q both became %q", previous, in, got)
		}
		seen[got] = in
	}
}

func TestMCPStateDirSeparatesConfusableServerNames(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	names := []string{"foo", "Foo", "foo bar", "foo-bar", "foo/bar", "foo\\bar"}
	seen := map[string]string{}
	for _, name := range names {
		dir := MCPStateDir(home, workspace, name)
		if previous, exists := seen[dir]; exists {
			t.Fatalf("state-directory collision: %q and %q both use %q", previous, name, dir)
		}
		seen[dir] = name
	}
}

func TestSlugAppendsHashForWindowsReservedDeviceNames(t *testing.T) {
	for _, name := range []string{"con", "CON", "prn", "aux", "nul", "com1", "COM9", "lpt1", "LPT9"} {
		got := slug(name)
		lowered := strings.ToLower(name)
		if got == lowered {
			t.Errorf("slug(%q) = %q names a Windows device", name, got)
		}
		if !strings.HasPrefix(got, lowered+"-") {
			t.Errorf("slug(%q) = %q, want %q plus a hash suffix", name, got, lowered)
		}
	}
	// Ordinary safe names must stay byte-identical: existing cache, stats, and
	// state paths depend on it.
	for _, name := range []string{"github", "context7", "a_b-c", "console", "com10", "naux"} {
		if got := slug(name); got != name {
			t.Errorf("slug(%q) = %q, want unchanged", name, got)
		}
	}
}

func TestSchemaCacheKeyRedactsURLCredentialsButKeepsResourceScope(t *testing.T) {
	base := sampleSpec()
	base.Type = "http"
	base.URL = "https://user:first@example.com/mcp?access_token=one&workspace=alpha&tenant=t1"

	rotated := base
	rotated.URL = "https://user:second@example.com/mcp?access_token=two&workspace=alpha&tenant=t1"
	if SchemaCacheKey(base) != SchemaCacheKey(rotated) {
		t.Fatal("credential rotation changed the schema cache key")
	}

	reordered := base
	reordered.URL = "https://user:first@example.com/mcp?tenant=t1&workspace=alpha&access_token=one"
	if SchemaCacheKey(base) != SchemaCacheKey(reordered) {
		t.Fatal("query parameter order changed the schema cache key")
	}

	movedWorkspace := base
	movedWorkspace.URL = "https://user:first@example.com/mcp?access_token=one&workspace=beta&tenant=t1"
	if SchemaCacheKey(base) == SchemaCacheKey(movedWorkspace) {
		t.Fatal("workspace scope change did not change the schema cache key")
	}

	// Case/separator variants of credential keys are still recognized: their
	// values redact, so rotating them never moves the cache key. The key
	// spelling itself remains identity-bearing.
	variant := base
	variant.URL = "https://user:first@example.com/mcp?ACCESS-TOKEN=three&workspace=alpha&tenant=t1"
	variantRotated := base
	variantRotated.URL = "https://user:first@example.com/mcp?ACCESS-TOKEN=four&workspace=alpha&tenant=t1"
	if SchemaCacheKey(variant) != SchemaCacheKey(variantRotated) {
		t.Fatal("variant-spelled credential key leaked its value into the schema cache key")
	}
}

func TestLoadCachedSchemaForSpecMigratesLegacyURLCacheKey(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	spec.Name = "legacy-url"
	spec.Type = "http"
	spec.URL = "https://example.com/mcp?access_token=secret&workspace=alpha"

	legacy, ok := legacySchemaCacheKey(spec)
	if !ok {
		t.Fatal("legacy cache key unavailable for credential-bearing URL")
	}
	if err := SaveCachedSchema(spec.Name, CachedSchema{
		CacheKey:     legacy,
		Capabilities: map[string]bool{"tools": true},
		Tools:        []CachedTool{{Name: "echo", Schema: json.RawMessage(`{"type":"object"}`)}},
	}); err != nil {
		t.Fatal(err)
	}

	cs, ok := LoadCachedSchemaForSpec(spec)
	if !ok || len(cs.Tools) != 1 || cs.Tools[0].Name != "echo" {
		t.Fatalf("legacy cache entry did not load: ok=%v cs=%+v", ok, cs)
	}
	if cs.CacheKey != SchemaCacheKey(spec) {
		t.Fatalf("loaded cache kept legacy key %q", cs.CacheKey)
	}
	// The upgrade persists: a plain current-key load now succeeds.
	if _, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec)); !ok {
		t.Fatal("legacy cache entry was not rewritten in place")
	}
}

func TestNormalizeIdentityURLRedactsCredentialMaterial(t *testing.T) {
	got := normalizeIdentityURL("HTTPS://User:Secret@Example.COM:443/mcp#frag?x=1")
	if strings.Contains(got, "Secret") {
		t.Fatalf("normalized URL leaked a password: %q", got)
	}
	rotatedA := normalizeIdentityURL("https://example.com/mcp?api_key=one&workspace=alpha")
	rotatedB := normalizeIdentityURL("https://example.com/mcp?api_key=two&workspace=alpha")
	if rotatedA != rotatedB {
		t.Fatalf("credential rotation changed normalization: %q != %q", rotatedA, rotatedB)
	}
	if !strings.Contains(rotatedA, "workspace=alpha") {
		t.Fatalf("non-sensitive query value dropped: %q", rotatedA)
	}
	userinfoOnly := normalizeIdentityURL("https://alice@example.com/mcp")
	userinfoPassword := normalizeIdentityURL("https://alice:pw@example.com/mcp")
	if userinfoOnly == userinfoPassword {
		t.Fatal("userinfo structure (password presence) was not preserved")
	}
	if strings.Contains(userinfoOnly, "alice") || strings.Contains(userinfoPassword, "pw") {
		t.Fatalf("userinfo values leaked: %q %q", userinfoOnly, userinfoPassword)
	}
}

func TestCredentialURLQueryKeyMatrix(t *testing.T) {
	credentials := []string{
		"token", "access_token", "auth_token", "refresh_token", "id_token",
		"api_token", "session_token", "bearer_token", "sas_token", "csrf-token",
		"api_key", "x-api-key", "apikey", "API-KEY", "key", "access_key",
		"secret_key", "private_key", "auth_key", "app_key", "client_key",
		"subscription-key", "shared_key",
		"secret", "client_secret", "app_secret", "api_secret",
		"password", "passwd", "user_password",
		"signature", "sas_signature", "sig",
		"auth", "authorization", "bearer", "credential", "credentials",
	}
	for _, key := range credentials {
		if !credentialURLQueryKey(key) {
			t.Errorf("credential key %q was not classified as sensitive", key)
		}
	}
	resources := []string{
		"workspace", "tenant", "region", "resource", "project", "org",
		"scope", "version", "monkey", "keyboard", "market", "environment",
	}
	for _, key := range resources {
		if credentialURLQueryKey(key) {
			t.Errorf("resource key %q was misclassified as a credential", key)
		}
	}
}
