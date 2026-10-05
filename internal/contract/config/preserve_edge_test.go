package config

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// saveInPlace applies edit through EditConfigFile and fails unless the file
// was edited in place (no backup), still parses, loads as the same edit made
// to its prior load, and keeps every line in keep byte for byte.
func saveInPlace(t *testing.T, seed string, edit func(*Config) error, keep ...string) (*Config, string) {
	t.Helper()
	path := seedUserConfigText(t, seed)
	want := LoadForEdit(path)
	if err := edit(want); err != nil {
		t.Fatal(err)
	}
	if err := EditConfigFile(path, edit); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	saved := string(raw)
	decodeTree(t, []byte(strings.TrimPrefix(saved, "\ufeff")))
	if backups, _ := filepath.Glob(path + ".rewrite-*"); len(backups) > 0 {
		t.Fatalf("saved by full rewrite:\n%s", saved)
	}
	got, err := loadUserConfigBytes(path, saved)
	if err != nil {
		t.Fatal(err)
	}
	if wantLoaded, err := loadUserConfigBytes(path, RenderTOMLForScope(want, RenderScopeUser)); err == nil {
		want = wantLoaded
	}
	if RenderTOMLForScope(got, RenderScopeUser) != RenderTOMLForScope(want, RenderScopeUser) {
		t.Fatalf("saved file does not load as the edit:\n%s", saved)
	}
	for _, line := range keep {
		if !strings.Contains(saved, line) {
			t.Fatalf("lost %q:\n%s", line, saved)
		}
	}
	return got, saved
}

func darkTheme(c *Config) error { return c.SetDesktopAppearance("dark", "") }

const threeProviders = "config_version = 12\n[desktop]\nfuture = 0\n" +
	"[[providers]]\nname = \"a\"\nkind = \"openai\"\nbase_url = \"http://a\"\nmodel = \"m\"\nfuture = 1\n\n" +
	"[[providers]]\nname = \"b\"\nkind = \"openai\"\nbase_url = \"http://b\"\nmodel = \"m\"   # keep\nfuture = 2\n\n" +
	"[[providers]]\nname = \"c\"\nkind = \"openai\"\nbase_url = \"http://c\"\nmodel = \"m\"\nfuture = 3\n"

func renameProvider(from, to string) func(*Config) error {
	return func(c *Config) error {
		for i := range c.Providers {
			if c.Providers[i].Name == from {
				c.Providers[i].Name = to
			}
		}
		return nil
	}
}

func TestSavingUserConfigEditsTheTextInPlace(t *testing.T) {
	cases := []struct {
		name string
		seed string
		edit func(*Config) error
		keep []string
	}{
		{"multiline literal holding headers", "config_version = 12\n[agent]\nsystem_prompt = '''\n[desktop]\ntheme = \"light\"\n'''\n[desktop]\ntheme = \"auto\"\nfuture = 1\n", darkTheme, []string{"future = 1", "theme = \"light\""}},
		{"multiline basic with escapes", "config_version = 12\n[agent]\nsystem_prompt = \"\"\"a \\\"\"\" [desktop]\ntheme = 'x' \\\n  more\"\"\"\n[desktop]\ntheme = \"auto\"\nfuture = 1\n", darkTheme, []string{"future = 1"}},
		{"multiline arrays and comments", "config_version = 12\n[desktop]\nstatus_bar_items = [\n  \"model\", # c ]\n  \"cache\",\n]\ntheme = \"auto\"\nfuture = [\n 1,\n 2,\n]\n", darkTheme, []string{"# c ]", "future = [\n 1,\n 2,\n]"}},
		{"inline table", "config_version = 12\n[desktop]\nfuture = { a = 1, b = [1, 2], c = { d = \"]\" } }\ntheme = \"auto\"\n", darkTheme, []string{"future = { a = 1, b = [1, 2], c = { d = \"]\" } }"}},
		{"byte order mark", "\ufeffconfig_version = 12\n[desktop]\ntheme = \"auto\"\nfuture = 1\n", darkTheme, []string{"future = 1"}},
		{"mixed line endings", "config_version = 12\r\n[desktop]\ntheme = \"auto\"\r\nfuture = 1\n", darkTheme, []string{"future = 1\n"}},
		{"tabs and trailing comment", "config_version = 12\n[desktop]\n\ttheme\t=\t\"auto\"\t# c\n\tfuture = 1\n", darkTheme, []string{"\tfuture = 1", "# c"}},
		{"no final newline", "config_version = 12\n[desktop]\nfuture = 1", darkTheme, []string{"future = 1\n"}},
		{"spaced header", "config_version = 12\n[ desktop ]   # hdr\ntheme=\"auto\"#c\nfuture = \"a#b\"\n", darkTheme, []string{"[ desktop ]   # hdr", "future = \"a#b\""}},
		{"inline table holding unknown keys", "config_version = 12\ndesktop = { theme = \"auto\", future = 1, more = [\"x\"] }\n", darkTheme, []string{"future = 1", "more = [\"x\"]"}},
		{"sub-table first", "config_version = 12\n[desktop.future]\nx = 1\n", darkTheme, []string{"[desktop.future]\nx = 1"}},
		{"unnamed provider", "config_version = 12\n[desktop]\nfuture = 0\n[[providers]]\nkind = \"openai\"\nfuture = 1\n", darkTheme, []string{"future = 1"}},
		{"delete a middle provider", threeProviders, func(c *Config) error { return c.RemoveProvider("b") }, []string{"future = 1", "future = 3"}},
		{"edit a middle provider", threeProviders, func(c *Config) error {
			for i := range c.Providers {
				if c.Providers[i].Name == "b" {
					c.Providers[i].Model = "m2"
				}
			}
			return nil
		}, []string{"future = 1", "future = 2", "future = 3", "# keep"}},
		{"reorder providers", threeProviders, func(c *Config) error {
			c.Providers[0], c.Providers[2] = c.Providers[2], c.Providers[0]
			return nil
		}, []string{"future = 0", "future = 1", "future = 2", "future = 3"}},
		{"rename a provider", threeProviders, renameProvider("b", "bb"), []string{"future = 1", "future = 3", "future = 0"}},
		{"crlf file gains a multiline prompt", "config_version = 12\r\n[agent]\r\ncompact_ratio = 0.8\r\n[desktop]\r\nfuture = 1\r\n", func(c *Config) error {
			c.Agent.SystemPrompt = "line1\nline2"
			return nil
		}, []string{"future = 1\r\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { saveInPlace(t, tc.seed, tc.edit, tc.keep...) })
	}
}

func TestRemovingTheLastProviderLeavesItsSubTablesWithIt(t *testing.T) {
	seed := "config_version = 12\n[[providers]]\nname = \"a\"\nkind = \"openai\"\nbase_url = \"http://a\"\nmodel = \"m\"\n\n" +
		"[[providers]]\nname = \"b\"\nkind = \"openai\"\nbase_url = \"http://b\"\nmodel = \"m\"\n[providers.headers]\nX-A = \"1\"\n"
	got, _ := saveInPlace(t, seed, func(c *Config) error { return c.RemoveProvider("b") })
	if p, _ := got.Provider("a"); len(got.Providers) != 1 || len(p.Headers) != 0 {
		t.Fatalf("providers = %+v, want a alone without b's headers", got.Providers)
	}
}

func TestAddingTheFirstProviderKeepsTheBuiltInOnes(t *testing.T) {
	got, _ := saveInPlace(t, "config_version = 12\n[desktop]\nfuture = 1\n", func(c *Config) error {
		return c.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://127.0.0.1:9000/v1", Model: "m"})
	}, "future = 1")
	if _, ok := got.Provider("deepseek"); !ok {
		t.Fatalf("built-in deepseek provider lost: %+v", got.Providers)
	}
}

func TestSystemPromptSurvivesASave(t *testing.T) {
	for _, prompt := range []string{`use C:\Users\me`, "a \"\"\" b", "ends with a quote\"", "ends with two\"\"", "tab\tand \\n literal", "\nleading newline", "bell\a and \r return", "中文提示"} {
		got, _ := saveInPlace(t, "config_version = 12\n[desktop]\nfuture = 1\n", func(c *Config) error {
			c.Agent.SystemPrompt = prompt
			return nil
		}, "future = 1")
		if got.Agent.SystemPrompt != prompt {
			t.Fatalf("system_prompt = %q, want %q", got.Agent.SystemPrompt, prompt)
		}
		got, _ = saveInPlace(t, "config_version = 12\n[agent]\nsystem_prompt = "+tomlMultilineBasicString(prompt)+"\n", darkTheme)
		if got.Agent.SystemPrompt != prompt {
			t.Fatalf("system_prompt after an unrelated save = %q, want %q", got.Agent.SystemPrompt, prompt)
		}
	}
}

func TestMultilineBasicStringDecodesToItsInput(t *testing.T) {
	for _, s := range []string{"", `\`, `"`, `""`, `"""`, `""""`, "a\"\"\"b", "x\\\ny", "\x00\x01\x1f\x7f", "\r\n", "\n\n", "é中"} {
		var out map[string]any
		if _, err := toml.Decode("v = "+tomlMultilineBasicString(s), &out); err != nil || out["v"] != s {
			t.Fatalf("%q: decoded %q, err %v", s, out["v"], err)
		}
	}
}

func TestChoosingAnApprovalModeReplacesOneThisBuildDoesNotKnow(t *testing.T) {
	seed := "config_version = 12\n[desktop]\ndefault_tool_approval_mode = \"future-mode\"\nfuture = 1\n"
	if got := LoadForEdit(seedUserConfigText(t, seed)).UnrecognizedDesktopToolApprovalMode(); got != "future-mode" {
		t.Fatalf("unrecognized mode = %q, want future-mode", got)
	}
	_, saved := saveInPlace(t, seed, darkTheme, "default_tool_approval_mode = \"future-mode\"")
	if strings.Contains(saved, "\"ask\"") {
		t.Fatalf("an unrelated save chose a mode for the user:\n%s", saved)
	}
	got, _ := saveInPlace(t, seed, func(c *Config) error { return c.SetDesktopDefaultToolApprovalMode("ask") }, "future = 1")
	if got.Desktop.DefaultToolApprovalMode != "ask" || got.UnrecognizedDesktopToolApprovalMode() != "" {
		t.Fatalf("chosen mode = %q, want ask written to the file", got.Desktop.DefaultToolApprovalMode)
	}
	for _, known := range []string{"", "ask", "Auto", "yolo", "full-access", "workspace-write", "danger-full-access", "read-only"} {
		c := Default()
		c.Desktop.DefaultToolApprovalMode = known
		if got := c.UnrecognizedDesktopToolApprovalMode(); got != "" {
			t.Fatalf("%q reported unrecognized as %q", known, got)
		}
	}
}

func TestSavingNeverWritesAFileThatDoesNotLoad(t *testing.T) {
	seed := "config_version = 12\n[agent]\ntemperature = nan\n[desktop]\nfuture = 1\n"
	if full := RenderTOMLForScope(LoadForEdit(seedUserConfigText(t, seed)), RenderScopeUser); !strings.Contains(full, "NaN") {
		t.Skip("the renderer now spells NaN as TOML does; this case needs another unloadable render")
	}
	_, saved := saveInPlace(t, seed, darkTheme, "temperature = nan", "future = 1")
	if !strings.Contains(saved, "theme") {
		t.Fatalf("theme not written:\n%s", saved)
	}

	path := seedUserConfigText(t, "config_version = 12\n[desktop]\nfuture = 1\n")
	before, _ := os.ReadFile(path)
	err := EditConfigFile(path, func(c *Config) error {
		c.Agent.Temperature = math.NaN()
		return nil
	})
	after, _ := os.ReadFile(path)
	if err == nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("err = %v, file changed = %v; want a refusal that leaves the file alone", err, !reflect.DeepEqual(before, after))
	}
}

func TestEditingAProviderThe1xFileSplitsReplacesOnlyTheProviders(t *testing.T) {
	path, original := seedUserConfigFrom1x(t)
	if err := EditConfigFile(path, func(c *Config) error { return c.SetProviderEffort(c.Providers[0].Name, "max") }); err != nil {
		t.Fatal(err)
	}
	if backups, _ := filepath.Glob(path + ".rewrite-*"); len(backups) > 0 {
		t.Fatal("saved by full rewrite")
	}
	saved, _ := os.ReadFile(path)
	want, got := decodeTree(t, original), decodeTree(t, saved)
	delete(want, "providers")
	delete(got, "providers")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("save changed more than [[providers]]:\n%s", saved)
	}
	head := string(original[:strings.Index(string(original), "[[providers]]")])
	if !strings.HasPrefix(string(saved), head) {
		t.Fatalf("text before [[providers]] changed:\n%s", saved)
	}
}

func TestEditingThe1xFileKeepsTheProvidersDisplayName(t *testing.T) {
	path, _ := seedUserConfigFrom1x(t)
	for _, edit := range []func(*Config) error{
		func(c *Config) error { return c.SetProviderEffort(c.Providers[0].Name, "max") },
		func(c *Config) error { c.Providers[0].ContextWindow = 500000; return nil },
		func(c *Config) error { c.Providers[0], c.Providers[1] = c.Providers[1], c.Providers[0]; return nil },
	} {
		if err := EditConfigFile(path, edit); err != nil {
			t.Fatal(err)
		}
		saved, _ := os.ReadFile(path)
		if !strings.Contains(string(saved), `display_name = "My DeepSeek"`) {
			t.Fatalf("display_name lost:\n%s", saved)
		}
	}
	if backups, _ := filepath.Glob(path + ".rewrite-*"); len(backups) > 0 {
		t.Fatalf("backups = %v, want none", backups)
	}
}

// Two elements sharing a name cannot be matched to the rendered ones, so the
// entries only they held go, and the prior file is kept beside the new one.
func TestAnEntryASaveCannotCarryLeavesABackup(t *testing.T) {
	seed := "config_version = 12\n[desktop]\nfuture = 0\n" +
		"[[providers]]\nname = \"a\"\nkind = \"openai\"\nbase_url = \"http://a\"\nmodel = \"m\"\nfuture = 1\n" +
		"[[providers]]\nname = \"a\"\nkind = \"openai\"\nbase_url = \"http://b\"\nmodel = \"m\"\nfuture = 2\n"
	path := seedUserConfigText(t, seed)
	if err := EditConfigFile(path, func(c *Config) error {
		return c.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://127.0.0.1:9000/v1", Model: "m"})
	}); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	decodeTree(t, saved)
	backups, _ := filepath.Glob(path + ".rewrite-*")
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want one:\n%s", backups, saved)
	}
	if kept, _ := os.ReadFile(backups[0]); string(kept) != seed {
		t.Fatalf("backup = %q, want the prior bytes", kept)
	}
	if !strings.Contains(string(saved), "future = 0") {
		t.Fatalf("lost what the save could keep:\n%s", saved)
	}
}
