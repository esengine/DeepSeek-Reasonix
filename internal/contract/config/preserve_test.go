package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/base/testenv"
)

// seedUserConfigFrom1x installs the file 1.x v1.39.5 writes, with keys this
// build does not decode and values it reads differently, as the user config.
func seedUserConfigFrom1x(t *testing.T) (path string, original []byte) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	original, err := os.ReadFile(filepath.Join("testdata", "user_config_1x_v1.39.5.toml"))
	if err != nil {
		t.Fatal(err)
	}
	path = UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, original
}

func decodeTree(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if _, err := toml.Decode(string(data), &out); err != nil {
		t.Fatalf("saved config does not parse: %v\n%s", err, data)
	}
	return out
}

// assertOnlyChanged fails unless saved equals original with want applied at
// the dotted paths, and every line outside those keys is byte-identical.
func assertOnlyChanged(t *testing.T, original, saved []byte, want map[string]any) {
	t.Helper()
	expected := decodeTree(t, original)
	for dotted, v := range want {
		parts := strings.Split(dotted, ".")
		table := expected
		for _, p := range parts[:len(parts)-1] {
			next, ok := table[p].(map[string]any)
			if !ok {
				next = map[string]any{}
				table[p] = next
			}
			table = next
		}
		table[parts[len(parts)-1]] = v
	}
	got := decodeTree(t, saved)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("save changed more than %v:\n%s", want, saved)
	}
	before := strings.Split(string(original), "\n")
	after := strings.Split(string(saved), "\n")
	if len(before) != len(after) {
		return
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
		}
	}
	if changed > len(want) {
		t.Fatalf("save rewrote %d lines for %d keys:\n%s", changed, len(want), saved)
	}
}

func assert1xKeysKept(t *testing.T, saved []byte) {
	t.Helper()
	tree := decodeTree(t, saved)
	desktop, _ := tree["desktop"].(map[string]any)
	if desktop["default_tool_approval_mode"] != "danger-full-access" {
		t.Fatalf("default_tool_approval_mode = %v, want danger-full-access", desktop["default_tool_approval_mode"])
	}
	if desktop["layout_style"] != "workbench" || desktop["status_bar_style_initialized"] != true {
		t.Fatalf("desktop 1.x keys lost: %v", desktop)
	}
	remote, _ := tree["remote"].(map[string]any)
	hosts, _ := remote["hosts"].([]map[string]any)
	if len(hosts) != 1 || hosts[0]["credential_mode"] != "remote" {
		t.Fatalf("remote.hosts credential_mode lost: %v", remote)
	}
	checkpoints, _ := tree["checkpoints"].(map[string]any)
	if checkpoints["retain_turns"] != int64(7) || checkpoints["blob_quota_bytes"] != int64(536870912) {
		t.Fatalf("[checkpoints] lost: %v", tree["checkpoints"])
	}
	if browser, _ := tree["browser"].(map[string]any); browser["chrome_path"] == nil {
		t.Fatalf("browser.chrome_path lost: %v", browser)
	}
	if agent, _ := tree["agent"].(map[string]any); agent["web_search_model"] != "deepseek-flash" {
		t.Fatalf("agent.web_search_model lost: %v", agent)
	}
	providers, _ := tree["providers"].([]map[string]any)
	if len(providers) != 2 || providers[1]["display_name"] != "My DeepSeek" {
		t.Fatalf("providers display_name lost: %v", providers)
	}
	bot, _ := tree["bot"].(map[string]any)
	qq, _ := bot["qq"].(map[string]any)
	if bot["enabled"] != true || qq["app_id"] != "123" || qq["app_secret_env"] != "QQ_BOT_APP_SECRET" {
		t.Fatalf("[bot] passthrough lost: %v", bot)
	}
}

func TestSavingOneUserSettingLeavesThe1xFileAlone(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"SaveTo": func(t *testing.T, path string) {
			unlock := LockUserConfigEdits()
			defer unlock()
			cfg, err := LoadForEditReadOnlyStrict(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.SetCompactRatio(0.75); err != nil {
				t.Fatal(err)
			}
			if err := cfg.SaveTo(path); err != nil {
				t.Fatal(err)
			}
		},
		"SaveToScope": func(t *testing.T, path string) {
			unlock := LockUserConfigEdits()
			defer unlock()
			cfg := LoadForEdit(path)
			if err := cfg.SetCompactRatio(0.75); err != nil {
				t.Fatal(err)
			}
			if err := cfg.SaveToScope(path, RenderScopeUser); err != nil {
				t.Fatal(err)
			}
		},
		"WriteFile": func(t *testing.T, path string) {
			cfg := LoadForEdit(path)
			if err := cfg.SetCompactRatio(0.75); err != nil {
				t.Fatal(err)
			}
			if err := cfg.WriteFile(path); err != nil {
				t.Fatal(err)
			}
		},
		"EditConfigFile": func(t *testing.T, path string) {
			if err := EditConfigFile(path, func(c *Config) error { return c.SetCompactRatio(0.75) }); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, save := range cases {
		t.Run(name, func(t *testing.T) {
			path, original := seedUserConfigFrom1x(t)
			save(t, path)
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertOnlyChanged(t, original, saved, map[string]any{"agent.compact_ratio": 0.75})
			assert1xKeysKept(t, saved)
		})
	}
}

func TestSavingUserSettingsWritesOnlyWhatChanged(t *testing.T) {
	path, original := seedUserConfigFrom1x(t)
	unlock := LockUserConfigEdits()
	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetDesktopAppearance("dark", ""); err != nil {
		t.Fatal(err)
	}
	cfg.Bot = nil
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	unlock()
	saved, _ := os.ReadFile(path)
	tree := decodeTree(t, saved)
	if _, ok := tree["bot"]; ok {
		t.Fatalf("a cleared [bot] was written back:\n%s", saved)
	}
	want := decodeTree(t, original)
	delete(want, "bot")
	want["desktop"].(map[string]any)["theme"] = "dark"
	if !reflect.DeepEqual(tree, want) {
		t.Fatalf("save changed more than desktop.theme and [bot]:\n%s", saved)
	}
}

func TestSavingANewProviderKeepsExistingEntriesWhole(t *testing.T) {
	path, _ := seedUserConfigFrom1x(t)
	unlock := LockUserConfigEdits()
	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://127.0.0.1:9000/v1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	cfg.Remote.Hosts = append(cfg.Remote.Hosts, RemoteHostEntry{Name: "second", Host: "10.0.0.2"})
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	unlock()
	saved, _ := os.ReadFile(path)
	assert1xKeysKept2(t, saved)
}

func assert1xKeysKept2(t *testing.T, saved []byte) {
	t.Helper()
	tree := decodeTree(t, saved)
	providers, _ := tree["providers"].([]map[string]any)
	if len(providers) != 3 || providers[1]["display_name"] != "My DeepSeek" || providers[2]["name"] != "local" {
		t.Fatalf("providers after adding one: %v\n%s", providers, saved)
	}
	hosts, _ := tree["remote"].(map[string]any)["hosts"].([]map[string]any)
	if len(hosts) != 2 || hosts[0]["credential_mode"] != "remote" || hosts[1]["name"] != "second" {
		t.Fatalf("remote hosts after adding one: %v\n%s", hosts, saved)
	}
}

func TestSavingAFreshUserConfigRendersTheTemplate(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := UserConfigPath()
	cfg := LoadForEdit(path)
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if string(saved) != RenderTOMLForScope(cfg, RenderScopeUser) {
		t.Fatalf("fresh user config is not the rendered template:\n%s", saved)
	}
	// Below 12 the 1.x line rewrites the whole file on its next start.
	if got := decodeTree(t, saved)["config_version"]; got != int64(12) {
		t.Fatalf("fresh config_version = %v, want 12", got)
	}
}

func seedUserConfigText(t *testing.T, text string) string {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := UserConfigPath()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSavingUserSettingsKeepsLineEndingsAndMultilineValues(t *testing.T) {
	seed := "config_version = 12\r\n\r\n[agent]\r\nsystem_prompt = \"\"\"\r\n[desktop]\r\ntheme = \"light\"\r\n\"\"\"\r\ncompact_ratio = 0.8\r\n\r\n[desktop]\r\ntheme = \"auto\"   # kept comment\r\nfuture_key = 1\r\n"
	path := seedUserConfigText(t, seed)
	if err := EditConfigFile(path, func(c *Config) error { return c.SetDesktopAppearance("dark", "") }); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	want := strings.Replace(seed, "theme = \"auto\"   # kept comment", "theme = \"dark\"   # kept comment", 1)
	if string(saved) != want {
		t.Fatalf("saved:\n%q\nwant:\n%q", saved, want)
	}
}

func TestSavingAUserConfigItCannotEditInPlaceKeepsABackup(t *testing.T) {
	seed := "config_version = 12\ndesktop.theme = \"auto\"\nfuture_key = \"kept in the backup\"\n"
	path := seedUserConfigText(t, seed)
	if err := EditConfigFile(path, func(c *Config) error { return c.SetDesktopAppearance("dark", "") }); err != nil {
		t.Fatal(err)
	}
	if got := LoadForEdit(path).Desktop.Theme; got != "dark" {
		t.Fatalf("desktop.theme = %q, want dark", got)
	}
	backups, _ := filepath.Glob(path + ".rewrite-*")
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want one", backups)
	}
	if kept, _ := os.ReadFile(backups[0]); string(kept) != seed {
		t.Fatalf("backup = %q, want the prior bytes", kept)
	}
}
