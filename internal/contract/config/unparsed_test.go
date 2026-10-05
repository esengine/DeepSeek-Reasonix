package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// The line a Windows user writes by hand. TOML reads \U as the start of a code
// point and \S as nothing at all, and the file stops parsing at whichever comes
// first — while every reading of what was typed agrees: the backslash is a
// backslash.
const windowsPathConfig = `language = "zh"

[[plugins]]
name = "local"
command = "C:\Users\HUAWEI\AppData\Roaming\Scripts\mcp.exe"
`

const windowsCommand = `C:\Users\HUAWEI\AppData\Roaming\Scripts\mcp.exe`

func TestRepairSaysTheSameBytesInAWayTOMLAccepts(t *testing.T) {
	repaired, ok := repairEscapes(windowsPathConfig)
	if !ok {
		t.Fatal("no repair offered for a Windows path in a basic string")
	}
	cfg := Default()
	if _, err := decodeTOMLBytes([]byte(repaired), cfg); err != nil {
		t.Fatalf("repaired file does not parse: %v", err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Command != windowsCommand {
		t.Fatalf("command after repair = %+v, want %q unchanged", cfg.Plugins, windowsCommand)
	}
}

// A valid escape is not a defect and is not touched: the parser points at what
// it refused, and nothing here reads further than that.
func TestRepairLeavesAnEscapeThatWasMeant(t *testing.T) {
	repaired, ok := repairEscapes("a = \"tab\\there\\Sx\"\n")
	if !ok {
		t.Fatal("no repair offered")
	}
	cfg := map[string]any{}
	if _, err := decodeTOMLBytes([]byte(repaired), &cfg); err != nil {
		t.Fatalf("repaired file does not parse: %v", err)
	}
	if cfg["a"] != "tab\there\\Sx" {
		t.Fatalf("value = %q, want the tab kept and the lone backslash literal", cfg["a"])
	}
}

// A file that needs a decision nobody here can make is left alone. The repair
// exists only where the parser accepts the result, so "nothing offered" is the
// answer for everything else rather than a rewrite that guesses.
func TestNothingIsOfferedForAFileThatIsActuallyBroken(t *testing.T) {
	for _, src := range []string{"broken = [", "[a\nb = 1\n", "x = \n", "a = 1\na = 2\n"} {
		if got, ok := repairEscapes(src); ok {
			t.Fatalf("offered %q for %q", got, src)
		}
	}
}

func brokenUserConfig(t *testing.T) string {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(windowsPathConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The settings surfaces used to show built-in defaults while the user's own
// file sat unread on disk — settings nobody had chosen, presented as theirs.
func TestSettingsSeeTheSnapshotRatherThanBuiltInDefaults(t *testing.T) {
	path := brokenUserConfig(t)
	snapshot := LastKnownGoodConfigPath()
	if err := os.MkdirAll(filepath.Dir(snapshot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot, []byte("default_model = \"from-snapshot\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadForEdit(path)
	if cfg.DefaultModel != "from-snapshot" {
		t.Fatalf("default model = %q, want the last-known-good snapshot", cfg.DefaultModel)
	}
	problem := cfg.Unparsed()
	if problem == nil {
		t.Fatal("an unreadable file reported no problem")
	}
	if problem.Line != 5 || !strings.Contains(problem.Excerpt, "mcp.exe") {
		t.Fatalf("problem = %+v, want line 5 with the offending line in it", problem)
	}
	if !problem.Repairable() || !strings.Contains(problem.Repair, `\\Scripts`) {
		t.Fatalf("repair = %q, want the same line with its backslashes doubled", problem.Repair)
	}
	if problem.Recovered != RecoveredSnapshot {
		t.Fatalf("recovered = %q, want %q", problem.Recovered, RecoveredSnapshot)
	}

	// The guard is unchanged: recovering values to show is not permission to
	// write them over the file they did not come from.
	var unparsed *UnparsedFile
	if err := cfg.SaveTo(path); !errors.As(err, &unparsed) {
		t.Fatalf("save error = %v, want the unparsed identity", err)
	}
}

// Without a snapshot the surface still says which values it is showing, so
// "these are not your settings" is on screen either way.
func TestWithoutASnapshotTheSurfaceSaysItIsShowingDefaults(t *testing.T) {
	cfg := LoadForEdit(brokenUserConfig(t))
	problem := cfg.Unparsed()
	if problem == nil || problem.Recovered != RecoveredDefaults {
		t.Fatalf("problem = %+v, want defaults reported", problem)
	}
}

func TestRepairKeepsTheOriginalBesideTheRepairedFile(t *testing.T) {
	path := brokenUserConfig(t)
	backup, err := RepairUnparsedConfig(path)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	kept, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(kept) != windowsPathConfig {
		t.Fatalf("backup = %q, want the file exactly as it was", kept)
	}
	cfg := LoadForEdit(path)
	if problem := cfg.Unparsed(); problem != nil {
		t.Fatalf("file still does not parse after repair: %+v", problem)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Command != windowsCommand {
		t.Fatalf("command after repair = %+v, want %q", cfg.Plugins, windowsCommand)
	}
}
