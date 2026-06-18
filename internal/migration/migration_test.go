package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

const legacyMessageLog = `{"role":"user","content":"hello from v0.x"}
{"role":"assistant","content":"hi there"}
`

func isolateMigrationHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_HOME", filepath.Join(home, "new-reasonix"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Chdir(t.TempDir())
	return home
}

func TestRunLegacyRescueImportsSessionsAndEmitsProgress(t *testing.T) {
	home := isolateMigrationHome(t)
	legacyDir := filepath.Join(home, ".reasonix", "sessions")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "old-chat.jsonl"), []byte(legacyMessageLog), 0o644); err != nil {
		t.Fatal(err)
	}

	var notices []string
	res := RunLegacyRescue(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	}))
	if res.ConfigErr != nil {
		t.Fatalf("config migration error: %v", res.ConfigErr)
	}
	if len(res.SessionErrs) != 0 {
		t.Fatalf("session migration errors: %v", res.SessionErrs)
	}
	if got := totalImported(res.SessionImports); got != 1 {
		t.Fatalf("imported sessions = %d, want 1; imports=%+v", got, res.SessionImports)
	}
	if _, err := os.Stat(filepath.Join(config.SessionDir(), "old-chat.jsonl")); err != nil {
		t.Fatalf("migrated session missing: %v", err)
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{
		"migration rescue: checking legacy config and credentials",
		"migration rescue: scanning legacy sessions",
		"imported 1 past session(s)",
		"migration rescue complete: imported 1 past session(s)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing notice %q in:\n%s", want, joined)
		}
	}
}

func TestRunLegacyRescueImportsMemory(t *testing.T) {
	home := isolateMigrationHome(t)
	legacyRoot := filepath.Join(home, ".reasonix")
	if err := os.MkdirAll(filepath.Join(legacyRoot, "memory", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "REASONIX.md"), []byte("legacy user memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "memory", "global", "user.md"), []byte("---\nname: user\n---\nlegacy fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectMemory := filepath.Join(legacyRoot, "projects", "proj-slug", "memory")
	if err := os.MkdirAll(projectMemory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectMemory, "project.md"), []byte("---\nname: project\n---\nproject fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var notices []string
	res := RunLegacyRescue(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	}))
	if len(res.MemoryErrs) != 0 {
		t.Fatalf("memory migration errors: %v", res.MemoryErrs)
	}
	if got := totalMemoryImported(res.MemoryImports); got != 3 {
		t.Fatalf("imported memory files = %d, want 3; imports=%+v", got, res.MemoryImports)
	}
	for _, path := range []string{
		filepath.Join(config.MemoryUserDir(), "REASONIX.md"),
		filepath.Join(config.MemoryUserDir(), "memory", "global", "user.md"),
		filepath.Join(config.MemoryUserDir(), "projects", "proj-slug", "memory", "project.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("migrated memory missing at %s: %v", path, err)
		}
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{
		"migration rescue: scanning legacy memory",
		"imported 3 memory file(s)",
		"migration rescue complete: imported 3 memory file(s)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing notice %q in:\n%s", want, joined)
		}
	}
}

func TestRunLegacyRescueNoopStillShowsProgress(t *testing.T) {
	isolateMigrationHome(t)

	var notices []string
	res := RunLegacyRescue(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	}))
	if got := totalImported(res.SessionImports); got != 0 {
		t.Fatalf("imported sessions = %d, want 0", got)
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{
		"migration rescue: checking legacy config and credentials",
		"migration rescue: no legacy sessions needed migration",
		"migration rescue complete: no legacy data needed migration",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing notice %q in:\n%s", want, joined)
		}
	}
}

func TestMigrateLegacySessionSourcesSkipsCurrentProjectTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_HOME", "")
	t.Setenv("REASONIX_STATE_HOME", "")
	if !samePath(config.MemoryUserDir(), filepath.Join(home, ".reasonix")) {
		t.Skip("current Reasonix home is not ~/.reasonix on this platform")
	}

	projectSessions := filepath.Join(config.MemoryUserDir(), "projects", "current-project", "sessions")
	subagents := filepath.Join(projectSessions, "subagents")
	if err := os.MkdirAll(subagents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagents, "worker.jsonl"), []byte(legacyMessageLog), 0o644); err != nil {
		t.Fatal(err)
	}

	imports := MigrateLegacySessionSources(event.FuncSink(func(event.Event) {}))
	if got := totalImported(imports); got != 0 {
		t.Fatalf("imported sessions = %d, want 0; imports=%+v", got, imports)
	}
	if _, err := os.Stat(filepath.Join(projectSessions, "worker.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("subagent transcript must not be copied into parent history, stat err=%v", err)
	}
}

func totalImported(imports []SessionImport) int {
	total := 0
	for _, imp := range imports {
		total += imp.Count
	}
	return total
}

func totalMemoryImported(imports []MemoryImport) int {
	total := 0
	for _, imp := range imports {
		total += imp.Count
	}
	return total
}
