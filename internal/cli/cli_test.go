package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestChdirTo(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if rc := chdirTo(""); rc != 0 {
		t.Fatalf(`chdirTo("") = %d, want 0`, rc)
	}
	if cwd, _ := os.Getwd(); cwd != orig {
		t.Fatalf(`chdirTo("") moved cwd to %q`, cwd)
	}

	tmp := t.TempDir()
	// Restore CWD before TempDir's RemoveAll runs (LIFO ordering): Windows can't
	// delete a directory that is still the process working directory.
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if rc := chdirTo(tmp); rc != 0 {
		t.Fatalf("chdirTo(tmp) = %d, want 0", rc)
	}
	got, _ := filepath.EvalSymlinks(mustGetwd(t))
	want, _ := filepath.EvalSymlinks(tmp)
	if got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}

	if rc := chdirTo(filepath.Join(tmp, "does-not-exist")); rc != 2 {
		t.Fatalf("chdirTo(missing) = %d, want 2", rc)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return cwd
}

func TestMetadataCommandsDoNotProbeTerminalTheme(t *testing.T) {
	defer func(prev func() (terminalRGB, bool)) {
		queryTerminalBackgroundForTheme = prev
	}(queryTerminalBackgroundForTheme)
	queryTerminalBackgroundForTheme = func() (terminalRGB, bool) {
		t.Fatal("metadata command should not query terminal background")
		return terminalRGB{}, false
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"version"}, "test-version"); rc != 0 {
			t.Fatalf("version rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "reasonix test-version") {
		t.Fatalf("version output = %q", out)
	}

	out = captureStdout(t, func() {
		if rc := Run([]string{"help"}, "test-version"); rc != 0 {
			t.Fatalf("help rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("help output missing usage:\n%s", out)
	}
}

// TestConfigureKeys verifies that a shared api_key_env (each vendor's SKUs use
// the same env var) is asked only once, and entered keys become env lines.
func TestConfigureKeys(t *testing.T) {
	// Force a clean baseline: any DEEPSEEK_API_KEY / MIMO_API_KEY in the
	// process env (e.g. inherited from the test runner) would be picked up
	// by the new "reuse existing" path and the prompt would be skipped,
	// making the assertion below noisy.
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MIMO_API_KEY", "")

	selected := config.Default().Providers // deepseek-flash, deepseek-pro, mimo-pro, mimo-flash

	// Two distinct keys to enter: DEEPSEEK_API_KEY, then MIMO_API_KEY.
	input := "ds-key\nmi-key\n"
	env := configureKeys(selected, strings.NewReader(input), io.Discard)

	if len(env) != 2 {
		t.Fatalf("env = %v (want 2: DeepSeek asked once + MiMo asked once)", env)
	}
	if env[0] != "DEEPSEEK_API_KEY=ds-key" {
		t.Errorf("env[0] = %q", env[0])
	}
	if env[1] != "MIMO_API_KEY=mi-key" {
		t.Errorf("env[1] = %q", env)
	}
}

// TestConfigureKeysReusesExistingEnv covers the "user already typed the key
// in the URL-fetch flow, don't ask again" path. When the env var is set
// (either from .env or from a prior os.Setenv in the wizard), configureKeys
// must NOT consume from the input stream — otherwise the user's next typed
// line bleeds into the next provider's prompt. It also must include the
// existing value in envLines so the value is re-pinned into .env on
// re-runs of setup.
func TestConfigureKeysReusesExistingEnv(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "preset-ds-key")
	t.Setenv("MIMO_API_KEY", "") // ask for this one

	selected := config.Default().Providers
	var output bytes.Buffer
	env := configureKeys(selected, strings.NewReader("mi-key-from-input\n"), &output)

	if len(env) != 2 {
		t.Fatalf("env = %v (want 2: DeepSeek reused + MiMo entered)", env)
	}
	if env[0] != "DEEPSEEK_API_KEY=preset-ds-key" {
		t.Errorf("env[0] = %q, want re-pinned existing value", env[0])
	}
	if env[1] != "MIMO_API_KEY=mi-key-from-input" {
		t.Errorf("env[1] = %q, want typed value", env[1])
	}
	if !strings.Contains(output.String(), "DEEPSEEK_API_KEY") {
		t.Errorf("expected a 'reusing' confirmation for DEEPSEEK_API_KEY, got:\n%s", output.String())
	}
}

// TestConfigureKeysAllSetSkipsInput ensures that when every env var is
// already populated, configureKeys returns without reading anything from
// the input — critical for the first-time-setup flow, where the URL-fetch
// step has already collected all keys and configureKeys is a no-op.
func TestConfigureKeysAllSetSkipsInput(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "ds")
	t.Setenv("MIMO_API_KEY", "mi")

	selected := config.Default().Providers
	env := configureKeys(selected, strings.NewReader("should-not-be-consumed\n"), io.Discard)
	if len(env) != 2 {
		t.Errorf("env = %v, want 2 (both reused)", env)
	}
}

// TestAppendEnvUpsertReplacesExistingKey covers the bug where re-running the
// wizard with a corrected key would append a second line for the same env
// var. loadDotEnv is first-wins, so without dedupe the stale key kept
// authenticating, and the user saw a 401 with no obvious cause.
func TestAppendEnvUpsertReplacesExistingKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "") // also covers the os.Setenv pin path
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# initial\nDEEPSEEK_API_KEY=stale\nMIMO_API_KEY=keepme\n"), 0o600)

	if err := appendEnv(p, []string{"DEEPSEEK_API_KEY=fresh"}); err != nil {
		t.Fatalf("appendEnv: %v", err)
	}
	got, _ := os.ReadFile(p)
	want := "# initial\nMIMO_API_KEY=keepme\nDEEPSEEK_API_KEY=fresh\n"
	if string(got) != want {
		t.Errorf("after upsert =\n%s\nwant =\n%s", got, want)
	}
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "fresh" {
		t.Errorf("process env DEEPSEEK_API_KEY = %q, want %q (upsert should pin in-process)", got, "fresh")
	}
}

// TestAppendEnvUpsertHandlesExportPrefix proves `export FOO=...` style lines
// also get replaced, since users might hand-edit .env in shell-friendly form.
func TestAppendEnvUpsertHandlesExportPrefix(t *testing.T) {
	t.Setenv("FOO", "")
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("export FOO=old\nKEEP=yes\n"), 0o600)
	if err := appendEnv(p, []string{"FOO=new"}); err != nil {
		t.Fatalf("appendEnv: %v", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "FOO=new") || strings.Contains(string(got), "FOO=old") {
		t.Errorf("export-prefixed line not replaced:\n%s", got)
	}
}

// TestGroupByFamily verifies the wizard groups the default preset into
// "deepseek" (flash + pro) and "mimo" (pro + flash), preserving the order
// each family first appears in.
func TestGroupByFamily(t *testing.T) {
	order, members, info := groupByFamily(config.Default().Providers)

	if got := order; !reflect.DeepEqual(got, []string{"deepseek", "mimo"}) {
		t.Fatalf("family order = %v, want [deepseek mimo]", got)
	}
	if got := members["deepseek"]; !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("deepseek members = %v, want [0 1]", got)
	}
	if got := members["mimo"]; !reflect.DeepEqual(got, []int{2, 3}) {
		t.Errorf("mimo members = %v, want [2 3]", got)
	}
	if info["deepseek"].name != "DeepSeek" || info["mimo"].name != "MiMo (Xiaomi)" {
		t.Errorf("display names = %q / %q", info["deepseek"].name, info["mimo"].name)
	}
}

// TestFetchOrFallbackLiveReturns covers the happy path: a live /models call
// succeeds and its result wins over the preset's static list. We can't run
// the real probe (no key) so the FetchModels call is expected to 401 and the
// fallback path runs; the assertion below is that fallback works (static
// list returned) and that an empty base URL short-circuits to the static
// list with no network call.
func TestFetchOrFallback(t *testing.T) {
	t.Run("empty base URL returns static list", func(t *testing.T) {
		probe := config.ProviderEntry{
			BaseURL: "",
			Models:  []string{"preset-a", "preset-b"},
		}
		got := fetchOrFallback(&probe, "Test")
		if !reflect.DeepEqual(got, []string{"preset-a", "preset-b"}) {
			t.Errorf("got %v, want preset-a/b", got)
		}
	})

	t.Run("no key set returns static list (offline first-run)", func(t *testing.T) {
		t.Setenv("REASONIX_FETCH_TEST_KEY", "")
		probe := config.ProviderEntry{
			BaseURL:   "http://127.0.0.1:1", // unreachable, no listener
			APIKeyEnv: "REASONIX_FETCH_TEST_KEY",
			Models:    []string{"preset-a"},
		}
		got := fetchOrFallback(&probe, "Test")
		if !reflect.DeepEqual(got, []string{"preset-a"}) {
			t.Errorf("got %v, want preset-a", got)
		}
	})
}

// TestFamilyStaticModels proves the offline fallback unions every member of a
// family (the flash + pro SKUs), not just the first — the regression that left
// users with only flash when the live /models probe failed.
func TestFamilyStaticModels(t *testing.T) {
	providers := []config.ProviderEntry{
		{Name: "deepseek-flash", Model: "deepseek-v4-flash"},
		{Name: "deepseek-pro", Model: "deepseek-v4-pro"},
		{Name: "mimo-flash", Model: "mimo-v2.5"},
	}
	got := familyStaticModels(providers, []int{0, 1})
	want := []string{"deepseek-v4-flash", "deepseek-v4-pro"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFamilyStaticModelsDedupes(t *testing.T) {
	providers := []config.ProviderEntry{
		{Name: "a", Models: []string{"x", "y"}},
		{Name: "b", Models: []string{"y", "z"}},
	}
	got := familyStaticModels(providers, []int{0, 1})
	if !reflect.DeepEqual(got, []string{"x", "y", "z"}) {
		t.Errorf("got %v, want x/y/z deduped", got)
	}
}

// TestBuildFamilyEntry covers the three observable behaviors:
//   - The selected models land in the entry's Models field, with Model
//     pointed at the first one so legacy single-model lookups still work.
//   - A preset Default that points to a model the user didn't pick is
//     reset to the first selected model (otherwise resolve-by-default
//     would silently break).
//   - A preset Default that IS in the selection is preserved.
func TestBuildFamilyEntry(t *testing.T) {
	t.Run("default reset when not in selection", func(t *testing.T) {
		probe := config.ProviderEntry{
			Name: "deepseek", Kind: "openai",
			BaseURL: "https://api.deepseek.com",
			Models:  []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			Default: "deepseek-v4-pro",
		}
		got := buildFamilyEntry(probe, []string{"deepseek-v4-flash"})
		if got.Model != "deepseek-v4-flash" {
			t.Errorf("Model = %q, want deepseek-v4-flash", got.Model)
		}
		if got.Default != "deepseek-v4-flash" {
			t.Errorf("Default = %q, want reset to first selected", got.Default)
		}
		if !reflect.DeepEqual(got.Models, []string{"deepseek-v4-flash"}) {
			t.Errorf("Models = %v", got.Models)
		}
		if got.BaseURL != "https://api.deepseek.com" {
			t.Errorf("BaseURL lost: %q", got.BaseURL)
		}
	})

	t.Run("default preserved when in selection", func(t *testing.T) {
		probe := config.ProviderEntry{
			Name: "deepseek", Default: "deepseek-v4-pro",
			BaseURL: "https://api.deepseek.com",
		}
		got := buildFamilyEntry(probe, []string{"deepseek-v4-flash", "deepseek-v4-pro"})
		if got.Default != "deepseek-v4-pro" {
			t.Errorf("Default = %q, want preserved", got.Default)
		}
	})

	t.Run("empty default filled from first selected", func(t *testing.T) {
		probe := config.ProviderEntry{Name: "x", BaseURL: "u"}
		got := buildFamilyEntry(probe, []string{"alpha", "beta"})
		if got.Default != "alpha" {
			t.Errorf("Default = %q, want alpha", got.Default)
		}
	})
}

// TestProviderSlug covers the host-derivation rules and the sha1 fallback
// for unparseable URLs. The exact format isn't load-bearing — what matters
// is that the slug (a) starts with the kind prefix, (b) is stable across
// calls with the same URL, and (c) never produces the bare "custom" /
// "anthropic" magic names that would collide with the wizard menu items.
func TestProviderSlug(t *testing.T) {
	cases := []struct {
		name, kind, url, want string
	}{
		{"standard host with port", "custom", "https://token.sensenova.cn/v1", "custom-token-sensenova-cn"},
		{"api subdomain", "custom", "https://api.openai.com/v1", "custom-api-openai-com"},
		{"www stripped", "custom", "https://www.example.com/v1", "custom-example-com"},
		{"port preserved", "custom", "http://localhost:11434/v1", "custom-localhost-11434"},
		{"anthropic kind", "anthropic", "https://api.anthropic.com", "anthropic-api-anthropic-com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerSlug(tc.kind, tc.url); got != tc.want {
				t.Errorf("providerSlug(%q, %q) = %q, want %q", tc.kind, tc.url, got, tc.want)
			}
		})
	}

	t.Run("stable across calls", func(t *testing.T) {
		a := providerSlug("custom", "https://token.sensenova.cn/v1")
		b := providerSlug("custom", "https://token.sensenova.cn/v1")
		if a != b {
			t.Errorf("not stable: %q vs %q", a, b)
		}
		if a == "custom" {
			t.Error("slug degenerated to bare magic name — collision risk")
		}
	})

	t.Run("sha1 fallback for unparseable URL", func(t *testing.T) {
		got := providerSlug("custom", "://not a url::://")
		if !strings.HasPrefix(got, "custom-") || got == "custom" {
			t.Errorf("fallback slug = %q, want custom-<hex>", got)
		}
		// sha1 is 40 hex chars; we take 4 bytes (8 hex chars).
		if len(got) != len("custom-")+8 {
			t.Errorf("fallback slug = %q, want 8 hex chars after prefix", got)
		}
	})
}

// TestFilterStaleCustomEntries covers the wizard's auto-cleanup of legacy
// "custom" / "anthropic" magic-name entries that previous versions wrote
// into reasonix.toml. These collide with the wizard's own menu items, so
// they're dropped from the providers list before grouping — but the caller
// still gets them back in the dropped slice to surface a warning.
func TestFilterStaleCustomEntries(t *testing.T) {
	in := []config.ProviderEntry{
		{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com"},
		{Name: "custom", Kind: "openai", BaseURL: "https://old.example/v1"},                // stale
		{Name: "anthropic", Kind: "anthropic", BaseURL: "https://old.example/v1/messages"}, // stale
		{Name: "mimo-tp", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1"},
	}
	kept, dropped := filterStaleCustomEntries(in)
	if len(kept) != 2 {
		t.Errorf("kept = %d entries, want 2: %+v", len(kept), kept)
	}
	if len(dropped) != 2 {
		t.Errorf("dropped = %d entries, want 2: %+v", len(dropped), dropped)
	}
	for _, k := range kept {
		if k.Name == "custom" || k.Name == "anthropic" {
			t.Errorf("magic name leaked through: %q", k.Name)
		}
	}

	t.Run("non-magic names with kind anthropic are kept", func(t *testing.T) {
		// An entry someone deliberately named "claude" (kind=anthropic) must
		// not be touched by the filter — only the bare "anthropic" magic name.
		in := []config.ProviderEntry{
			{Name: "claude", Kind: "anthropic", BaseURL: "https://api.anthropic.com"},
		}
		kept, dropped := filterStaleCustomEntries(in)
		if len(kept) != 1 || len(dropped) != 0 {
			t.Errorf("claude should be kept, got kept=%d dropped=%d", len(kept), len(dropped))
		}
	})

	t.Run("custom kind anthropic is kept", func(t *testing.T) {
		// Name="custom" with kind=anthropic is ambiguous — keep it.
		in := []config.ProviderEntry{
			{Name: "custom", Kind: "anthropic", BaseURL: "https://x"},
		}
		kept, dropped := filterStaleCustomEntries(in)
		if len(kept) != 1 || len(dropped) != 0 {
			t.Errorf("custom+anthropic should be kept (ambiguous), got kept=%d dropped=%d", len(kept), len(dropped))
		}
	})
}

func TestWithBuiltinFamiliesAddsMissingMiMo(t *testing.T) {
	// The user's case: a reasonix.toml that defines only deepseek providers.
	cfg := []config.ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com"},
		{Name: "deepseek-pro", Kind: "openai", BaseURL: "https://api.deepseek.com"},
	}
	order, _, info := groupByFamily(withBuiltinFamilies(cfg))
	seen := map[string]bool{}
	for _, k := range order {
		seen[info[k].name] = true
	}
	if !seen["DeepSeek"] || !seen["MiMo (Xiaomi)"] {
		t.Fatalf("wizard families = %v, want both DeepSeek and MiMo", order)
	}
	// A user's customized deepseek must not be duplicated.
	if n := len(groupByFamilyKeys(withBuiltinFamilies(cfg), "deepseek")); n != 2 {
		t.Fatalf("deepseek members = %d, want the user's 2 (no injected duplicate)", n)
	}
}

func groupByFamilyKeys(ps []config.ProviderEntry, key string) []int {
	_, members, _ := groupByFamily(ps)
	return members[key]
}
