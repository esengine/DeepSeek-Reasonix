package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/config"
)

func decodeTOMLBytesTest(b []byte, v any) (toml.MetaData, error) {
	return toml.Decode(string(b), v)
}

func writeUserConfig(t *testing.T, body string) string {
	t.Helper()
	path := config.UserConfigPath()
	if path == "" {
		t.Fatal("no user config path in isolated state")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApplyConfigEscapesAutoFixesGlobal(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := writeUserConfig(t, "command = \"D:\\开发\\tool.exe\"\n")

	report, err := InspectConfigEscapes(ConfigEscapesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Global.Exists || len(report.Global.Fixes) != 1 {
		t.Fatalf("global check = %+v", report.Global)
	}
	if report.Global.Applied {
		t.Fatal("inspect must never apply")
	}

	report, err = ApplyConfigEscapes(ConfigEscapesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Global.Applied {
		t.Fatalf("global not applied: %+v", report.Global)
	}
	// The repaired file parses and carries the literal path.
	if err := config.ValidateBytes(mustRead(t, path)); err != nil {
		t.Fatalf("repaired config does not parse: %v", err)
	}
	b, _ := os.ReadFile(path)
	var raw map[string]any
	if _, err := decodeTOMLBytesTest(b, &raw); err != nil {
		t.Fatalf("decode repaired: %v", err)
	}
	if got := raw["command"]; got != `D:\开发\tool.exe` {
		t.Errorf("repaired command = %v", got)
	}
	// Backup exists for undo.
	matches, _ := filepath.Glob(path + ".reasonix-escape-backup-*")
	if len(matches) != 1 {
		t.Fatalf("backup matches = %v", matches)
	}
	// Idempotent: a second apply finds nothing.
	report, err = ApplyConfigEscapes(ConfigEscapesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Global.Fixes) != 0 {
		t.Fatalf("second scan still reports fixes: %+v", report.Global.Fixes)
	}
}

func TestApplyConfigEscapesUndoRestoresOriginal(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	original := "command = \"D:\\开发\\tool.exe\"\n"
	path := writeUserConfig(t, original)

	if _, err := ApplyConfigEscapes(ConfigEscapesOptions{}); err != nil {
		t.Fatal(err)
	}
	tx, err := UndoLastRepair()
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if tx == nil {
		t.Fatal("no repair transaction to undo")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != original {
		t.Errorf("undo restored %q, want original %q", b, original)
	}
}

func TestUndoRepairExactRejectsStaleTransactionID(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	writeUserConfig(t, "command = \"D:\\开发\\tool.exe\"\n")
	if _, err := ApplyConfigEscapes(ConfigEscapesOptions{}); err != nil {
		t.Fatal(err)
	}
	first, err := ReadLastRepair()
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	projectOriginal := "[[plugins]]\ncommand = \"C:\\dev\\bridge.exe\"\n"
	if err := os.WriteFile(project, []byte(projectOriginal), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := InspectConfigEscapes(ConfigEscapesOptions{Root: root, IncludeProject: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyConfigEscapes(ConfigEscapesOptions{
		Root:           root,
		IncludeProject: true,
		ExpectedStates: map[string]string{project: preview.Project.StateID},
	}); err != nil {
		t.Fatal(err)
	}
	second, err := ReadLastRepair()
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("test did not create a newer repair transaction")
	}
	repairedProject := string(mustRead(t, project))
	if _, err := UndoRepairExact(first.ID); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("stale exact undo err = %v, want expired action", err)
	}
	if got := string(mustRead(t, project)); got != repairedProject {
		t.Fatalf("stale exact undo modified the newer repair: %q", got)
	}
	current, err := ReadLastRepair()
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != second.ID || current.Undone {
		t.Fatalf("newer repair changed by stale undo: %+v", current)
	}
}

func TestApplyConfigEscapesProjectRequiresConfirmation(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	original := "[[plugins]]\ncommand = \"C:\\dev\\bridge.exe\"\n"
	if err := os.WriteFile(project, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	// Preview without confirmation must never write the project file.
	report, err := ApplyConfigEscapes(ConfigEscapesOptions{Root: root, IncludeProject: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Project.Applied {
		t.Fatal("project config was auto-applied without confirmation")
	}
	b, _ := os.ReadFile(project)
	if string(b) != original {
		t.Fatalf("project config modified without confirmation: %q", b)
	}
	if len(report.Project.Fixes) != 1 {
		t.Fatalf("project fixes = %+v", report.Project.Fixes)
	}

	// Confirmed apply with the bound state ID writes the fix.
	expected := map[string]string{project: report.Project.StateID}
	report, err = ApplyConfigEscapes(ConfigEscapesOptions{Root: root, IncludeProject: true, ExpectedStates: expected})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Project.Applied {
		t.Fatalf("confirmed project apply did not apply: %+v", report.Project)
	}
	b, _ = os.ReadFile(project)
	if strings.Contains(string(b), `"C:\dev`) {
		t.Errorf("project config still has unescaped backslash: %q", b)
	}
	var raw map[string]any
	if _, err := decodeTOMLBytesTest(b, &raw); err != nil {
		t.Fatalf("repaired project config does not parse: %v", err)
	}
}

func TestApplyConfigEscapesRefusesChangedFile(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := writeUserConfig(t, "command = \"D:\\开发\\tool.exe\"\n")

	report, err := InspectConfigEscapes(ConfigEscapesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Another process modifies the file between preview and apply.
	if err := os.WriteFile(path, []byte("command = \"D:\\开发\\changed.exe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{path: report.Global.StateID}
	_, err = ApplyConfigEscapes(ConfigEscapesOptions{ExpectedStates: expected})
	if err == nil {
		t.Fatal("apply succeeded despite the file changing between preview and apply")
	}
	if !strings.Contains(err.Error(), "changed") {
		t.Errorf("unexpected error: %v", err)
	}
	// The changed file is untouched.
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "changed.exe") {
		t.Errorf("changed file was modified: %q", b)
	}
}

func TestApplyConfigEscapesSkipsValidConfigs(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	writeUserConfig(t, "command = \"D:\\\\开发\\\\tool.exe\"\n") // already escaped
	report, err := ApplyConfigEscapes(ConfigEscapesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Global.Exists && len(report.Global.Fixes) != 0 {
		t.Fatalf("escaped config reported fixes: %+v", report.Global.Fixes)
	}
	if report.Global.Applied {
		t.Fatal("valid config was rewritten")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
