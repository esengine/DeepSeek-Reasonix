package repair

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnoseFindsProviderPluginAndPermissionProblems(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	configText := `default_model = "custom/missing"

[[providers]]
name = "custom"
kind = "openai"
base_url = "not-a-url"
model = "model-a"
api_key_env = "CUSTOM_KEY"

[[plugins]]
name = "missing-command"
command = "reasonix-command-that-does-not-exist"

[permissions]
allow = ["bash"]
deny = ["bash"]
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Diagnose(context.Background(), DiagnoseOptions{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, finding := range report.Findings {
		codes[finding.Code] = true
	}
	for _, want := range []string{"provider.invalid_url", "provider.missing_key", "model.invalid_default", "plugin.command_missing", "permissions.conflict"} {
		if !codes[want] {
			t.Errorf("missing finding %s: %+v", want, report.Findings)
		}
	}
}

func TestDiagnoseDoesNotRewriteLegacyMCPTierConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	configText := "[[plugins]]\nname = \"srv\"\ncommand = \"echo\"\ntier = \"eager\"\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Diagnose(context.Background(), DiagnoseOptions{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != configText {
		t.Fatalf("read-only diagnose rewrote config:\n%s", got)
	}
}

func TestRebuildDerivedStateQuarantinesWithoutDeleting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "desktop-tabs.json")
	if err := os.WriteFile(path, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	applied, err := RebuildDerivedState("tabs")
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied = %v", applied)
	}
	if _, err := os.Stat(applied[0]); err != nil {
		t.Fatalf("quarantined state missing: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("original derived state remains: %v", err)
	}
}

func TestRebuildDerivedStateCommitsWhenAuditLogFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "desktop-tabs.json")
	original := []byte("bad")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repairLogPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RebuildDerivedState("tabs"); err != nil {
		t.Fatalf("rebuild must commit despite a failing audit log: %v", err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatalf("undo after audit-log failure: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(original) {
		t.Fatalf("undone derived state = %q (%v), want %q", got, err, original)
	}
}
