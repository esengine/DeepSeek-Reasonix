package config

import (
	"os"
	"strings"
	"testing"
)

func TestSaveToPreservesUnrecognizedSection(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.toml"

	original := `default_model = "deepseek-flash"
[lark]
app_id_env = "LARK_APP_ID"
app_secret_env = "LARK_APP_SECRET"
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadForEdit(path)
	cfg.DefaultModel = "deepseek-pro"
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "[lark]") {
		t.Errorf("[lark] section lost after save:\n%s", content)
	}
	if !strings.Contains(content, "app_id_env") {
		t.Errorf("app_id_env lost after save:\n%s", content)
	}
	if !strings.Contains(content, "deepseek-pro") {
		t.Errorf("modified default_model not preserved:\n%s", content)
	}
}

func TestSaveToNewFileUsesRenderTOML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.toml"

	cfg := Default()
	cfg.DefaultModel = "deepseek-flash"
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "# Reasonix configuration") {
		t.Error("new file should use RenderTOML with comments")
	}
}
