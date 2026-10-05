package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// What 1.x writes for a renamed connection: display_name beside the name it
// labels, with the name still the identity every ref points at.
const displayNameConfig = `default_model = "relay/gpt-5"

[[providers]]
name = "relay"
display_name = "工作账号"
kind = "openai"
base_url = "https://relay.example/v1"
models = ["gpt-5", "gpt-5-mini"]
default = "gpt-5"
api_key_env = "RELAY_API_KEY"

[[providers]]
name = "relay-anthropic"
kind = "anthropic"
base_url = "https://relay.example/anthropic"
models = ["claude-x"]
api_key_env = "RELAY_API_KEY"
`

func writeDisplayNameConfig(t *testing.T) string {
	t.Helper()
	dir := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", dir)
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(displayNameConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDisplayNameWrittenBy1xSurvivesAnUnrelatedSave(t *testing.T) {
	path := writeDisplayNameConfig(t)
	edit := LoadForEdit(path)
	relay, ok := edit.Provider("relay")
	if !ok || relay.DisplayName != "工作账号" {
		t.Fatalf("relay display name = %+v, want 工作账号", relay)
	}
	if err := edit.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `display_name = "工作账号"`) {
		t.Fatalf("saved config lost display_name:\n%s", raw)
	}
	back, ok := LoadForEdit(path).Provider("relay")
	if !ok || back.DisplayName != "工作账号" || back.Label() != "工作账号" {
		t.Fatalf("reloaded relay = %+v", back)
	}
	if other, _ := LoadForEdit(path).Provider("relay-anthropic"); other.DisplayName != "" || other.Label() != "relay-anthropic" {
		t.Fatalf("an unlabelled entry gained a label: %+v", other)
	}
}

func TestRenamingKeepsEveryIdentity(t *testing.T) {
	path := writeDisplayNameConfig(t)
	edit := LoadForEdit(path)
	if err := edit.SetProviderDisplayName([]string{"relay", "relay-anthropic"}, "  个人  "); err != nil {
		t.Fatal(err)
	}
	if err := edit.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	back := LoadForEdit(path)
	for _, name := range []string{"relay", "relay-anthropic"} {
		p, ok := back.Provider(name)
		if !ok || p.DisplayName != "个人" {
			t.Fatalf("%s after rename = %+v, want display name 个人", name, p)
		}
	}
	if back.DefaultModel != "relay/gpt-5" {
		t.Fatalf("default_model = %q, want relay/gpt-5", back.DefaultModel)
	}
	resolved, ok := back.ResolveModel("relay/gpt-5-mini")
	if !ok || resolved.Name != "relay" || resolved.DisplayName != "个人" {
		t.Fatalf("relay/gpt-5-mini resolved to %+v", resolved)
	}
	if _, ok := back.ResolveModel("个人/gpt-5"); ok {
		t.Fatal("a display name resolved as a ref; it must never be an identity")
	}

	if err := back.SetProviderDisplayName([]string{"relay", "relay-anthropic"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := back.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "display_name") {
		t.Fatalf("a cleared display name was still written:\n%s", raw)
	}
}

func TestSetProviderDisplayNameRefusesWithoutChangingAnything(t *testing.T) {
	path := writeDisplayNameConfig(t)
	cases := []struct {
		names []string
		value string
		want  error
	}{
		{[]string{"relay", "ghost"}, "x", ErrProviderNotFound},
		{[]string{"relay"}, strings.Repeat("名", MaxProviderDisplayNameRunes+1), ErrProviderDisplayNameTooLong},
		{[]string{"relay"}, "two\nlines", ErrProviderDisplayNameInvalid},
	}
	for _, c := range cases {
		edit := LoadForEdit(path)
		if err := edit.SetProviderDisplayName(c.names, c.value); !errors.Is(err, c.want) {
			t.Fatalf("SetProviderDisplayName(%v, %q) = %v, want %v", c.names, c.value, err, c.want)
		}
		if p, _ := edit.Provider("relay"); p.DisplayName != "工作账号" {
			t.Fatalf("a refused rename still changed relay to %q", p.DisplayName)
		}
	}
	if err := LoadForEdit(path).SetProviderDisplayName([]string{"relay"}, strings.Repeat("名", MaxProviderDisplayNameRunes)); err != nil {
		t.Fatalf("a name at the limit was refused: %v", err)
	}
}
