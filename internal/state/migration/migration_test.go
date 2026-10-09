package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/sessionv4/v4fixture"
)

const legacyMessageLog = `{"role":"user","content":"hello from v0.x"}
{"role":"assistant","content":"hi there"}
`

func migrationRescueHome(t *testing.T) string {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_HOME", "")
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(home, "new-state"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Chdir(testenv.TempDir(t))
	return home
}

func isolateMigrationHome(t *testing.T) string {
	t.Helper()
	home := migrationRescueHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "new-reasonix"))
	t.Setenv("REASONIX_STATE_HOME", "")
	return home
}

func TestRunLegacyRescueImportsSessionsAndEmitsProgress(t *testing.T) {
	home := migrationRescueHome(t)
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
	home := migrationRescueHome(t)
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
	migrationRescueHome(t)

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

func TestRunLegacyRescueSkipsImplicitSourcesWhenIsolated(t *testing.T) {
	home := isolateMigrationHome(t)
	legacyRoot := filepath.Join(home, ".reasonix")
	if err := os.MkdirAll(filepath.Join(legacyRoot, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "sessions", "old-chat.jsonl"), []byte(legacyMessageLog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "REASONIX.md"), []byte("legacy user memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var notices []string
	res := RunLegacyRescue(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	}))
	if got := totalImported(res.SessionImports); got != 0 {
		t.Fatalf("imported sessions = %d, want 0; imports=%+v", got, res.SessionImports)
	}
	if got := totalMemoryImported(res.MemoryImports); got != 0 {
		t.Fatalf("imported memory files = %d, want 0; imports=%+v", got, res.MemoryImports)
	}
	if _, err := os.Stat(filepath.Join(config.SessionDir(), "old-chat.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("isolated rescue imported legacy session, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(config.MemoryUserDir(), "REASONIX.md")); !os.IsNotExist(err) {
		t.Fatalf("isolated rescue imported legacy memory, stat err=%v", err)
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "REASONIX_HOME is set; implicit legacy migration is skipped") {
		t.Fatalf("missing isolated skip notice in:\n%s", joined)
	}
}

func TestRunLegacyRescueCommandImportsFromExplicitInstallDir(t *testing.T) {
	home := isolateMigrationHome(t)
	installRoot := filepath.Join(home, "Custom Reasonix")
	legacySessions := filepath.Join(installRoot, "sessions")
	if err := os.MkdirAll(legacySessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacySessions, "custom-chat.jsonl"), []byte(legacyMessageLog), 0o644); err != nil {
		t.Fatal(err)
	}
	currentSessions := config.SessionDir()
	if err := os.MkdirAll(currentSessions, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{".legacy-imported.v2-routed", ".legacy-imported.v3-jsonl"} {
		if err := os.WriteFile(filepath.Join(currentSessions, marker), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var notices []string
	res := RunLegacyRescueCommand(`--from "`+installRoot+`"`, event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	}))
	if len(res.SessionErrs) != 0 {
		t.Fatalf("session migration errors: %v", res.SessionErrs)
	}
	if got := totalImported(res.SessionImports); got != 1 {
		t.Fatalf("imported sessions = %d, want 1; imports=%+v", got, res.SessionImports)
	}
	if _, err := os.Stat(filepath.Join(currentSessions, "custom-chat.jsonl")); err != nil {
		t.Fatalf("explicit imported session missing: %v", err)
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{
		"migration rescue: scanning explicit legacy sessions from " + installRoot,
		"imported 1 past session(s) from " + legacySessions,
		"migration rescue complete: imported 1 past session(s)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing notice %q in:\n%s", want, joined)
		}
	}
}

func TestMigrateLegacySessionSourcesSkipsCurrentProjectTree(t *testing.T) {
	home := testenv.TempDir(t)
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

// A redirected state root owns sessions and projects, so the automatic
// importers must leave the production install alone. Before this, pointing
// REASONIX_STATE_HOME at a scratch directory copied every project's history
// into it — 460 sessions on the machine that found this, once per process.
func TestAutomaticImportSkipsProductionWhenStateRootRedirected(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_HOME", "")
	t.Setenv("REASONIX_STATE_HOME", "")

	production := filepath.Join(home, ".reasonix", "projects", "some-project", "sessions")
	if err := os.MkdirAll(production, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(production, "old.jsonl"), []byte(legacyMessageLog), 0o644); err != nil {
		t.Fatal(err)
	}

	isolated := filepath.Join(home, "isolated-state")
	t.Setenv("REASONIX_STATE_HOME", isolated)

	if got := totalImported(MigrateLegacySessionSources(event.FuncSink(func(event.Event) {}))); got != 0 {
		t.Fatalf("imported %d session(s) into a redirected state root, want 0", got)
	}
	if totalMemoryImported(MigrateLegacyMemorySources(event.FuncSink(func(event.Event) {}))) != 0 {
		t.Fatal("memory import ran against a redirected state root")
	}
	if entries, err := os.ReadDir(filepath.Join(isolated, "projects")); err == nil && len(entries) > 0 {
		t.Fatalf("production projects were copied into the isolated root: %d entries", len(entries))
	}
}

func seedV4Conversation(t *testing.T, root, id string) {
	t.Helper()
	store := v4fixture.New(t)
	dir := store.Session(id, 3)
	store.Batch(dir, v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "user", "hello from 1.x")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m2", "assistant", "hi")})
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, filepath.Join(root, id)); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitImportFindsEveryLayoutA1xInstallKeepsSessionsIn(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	layouts := map[string]func(root string){
		"sessions-v4": func(r string) { seedV4Conversation(t, filepath.Join(r, "sessions-v4"), id) },
		"desktop v5":  func(r string) { seedV4Conversation(t, filepath.Join(r, "desktop-sessions-v5", "by-id"), id) },
		"project v4":  func(r string) { seedV4Conversation(t, filepath.Join(r, "projects", "p", "sessions-v4"), id) },
		"project jsonl": func(r string) {
			dir := filepath.Join(r, "projects", "p", "sessions")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(legacyMessageLog), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, seed := range layouts {
		t.Run(name, func(t *testing.T) {
			home := isolateMigrationHome(t)
			root := filepath.Join(home, "Roaming", "reasonix")
			seed(root)
			dest := filepath.Join(config.SessionDir(), "ws")

			res := RunLegacySessionImportInto(root, dest, event.Discard)
			if got := totalImported(res.SessionImports); got != 1 || len(res.SessionErrs) != 0 {
				t.Fatalf("imported %d, errs %v; want 1", got, res.SessionErrs)
			}
			if infos, _ := sessionstore.ListSessions(dest); len(infos) != 1 {
				t.Fatalf("history lists %d sessions, want 1", len(infos))
			}
			again := RunLegacySessionImportInto(root, dest, event.Discard)
			if got := totalImported(again.SessionImports); got != 0 {
				t.Fatalf("second import copied %d, want 0", got)
			}
		})
	}
}

func TestExplicitImportAcceptsTheV4RootItself(t *testing.T) {
	home := isolateMigrationHome(t)
	root := filepath.Join(home, "Roaming", "reasonix", "sessions-v4")
	seedV4Conversation(t, root, "0123456789abcdef0123456789abcdef")
	res := RunLegacySessionImportInto(root, filepath.Join(config.SessionDir(), "ws"), event.Discard)
	if got := totalImported(res.SessionImports); got != 1 {
		t.Fatalf("imported %d, want 1", got)
	}
}

func TestExplicitImportHandlesGlobCharactersInThePath(t *testing.T) {
	home := isolateMigrationHome(t)
	root := filepath.Join(home, "x[1]", "reasonix")
	seedV4Conversation(t, filepath.Join(root, "projects", "p", "sessions-v4"), "0123456789abcdef0123456789abcdef")
	res := RunLegacySessionImportInto(root, filepath.Join(config.SessionDir(), "ws"), event.Discard)
	if got := totalImported(res.SessionImports); got != 1 {
		t.Fatalf("imported %d, want 1", got)
	}
}

func TestExplicitImportSkipsACorruptManifestAndKeepsTheRest(t *testing.T) {
	home := isolateMigrationHome(t)
	root := filepath.Join(home, "Roaming", "reasonix", "sessions-v4")
	seedV4Conversation(t, root, "0123456789abcdef0123456789abcdef")
	bad := filepath.Join(root, "ffffffffffffffffffffffffffffffff")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "manifest.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := RunLegacySessionImportInto(filepath.Dir(root), filepath.Join(config.SessionDir(), "ws"), event.Discard)
	if got := totalImported(res.SessionImports); got != 1 {
		t.Fatalf("imported %d, want 1", got)
	}
	if len(res.SessionErrs) != 1 {
		t.Fatalf("a corrupt manifest must be counted as one warning, got %v", res.SessionErrs)
	}
}

func TestExplicitImportSaysWhenTheFolderIsNotRecognised(t *testing.T) {
	home := isolateMigrationHome(t)
	empty := filepath.Join(home, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(config.SessionDir(), "ws")
	if res := RunLegacySessionImportInto(empty, dest, event.Discard); !res.Unrecognised {
		t.Fatal("an empty folder must be reported as unrecognised")
	}
	seedV4Conversation(t, filepath.Join(home, "old", "sessions-v4"), "0123456789abcdef0123456789abcdef")
	RunLegacySessionImportInto(filepath.Join(home, "old"), dest, event.Discard)
	if res := RunLegacySessionImportInto(filepath.Join(home, "old"), dest, event.Discard); res.Unrecognised {
		t.Fatal("an already imported folder is recognised")
	}
}

func TestExplicitImportDoesNotFollowASymlinkOutOfThePickedFolder(t *testing.T) {
	home := isolateMigrationHome(t)
	outside := filepath.Join(home, "elsewhere", "sessions-v4")
	seedV4Conversation(t, outside, "0123456789abcdef0123456789abcdef")
	root := filepath.Join(home, "Roaming", "reasonix")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "sessions-v4")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	res := RunLegacySessionImportInto(root, filepath.Join(config.SessionDir(), "ws"), event.Discard)
	if got := totalImported(res.SessionImports); got != 0 || !res.Unrecognised {
		t.Fatalf("imported %d through a symlink out of the folder (unrecognised=%v)", got, res.Unrecognised)
	}
}

const legacyEventLog = `{"type":"user.message","text":"hello from v0"}
{"type":"model.final","content":"hi"}
`

func writeLegacyFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitImportCountsEverySessionItCannotBringOver(t *testing.T) {
	cases := map[string]struct {
		seed     func(dir string)
		imported int
		warnings int
	}{
		"v0 event log":          {func(d string) { writeLegacyFile(t, d, "a.events.jsonl", legacyEventLog) }, 1, 0},
		"message jsonl":         {func(d string) { writeLegacyFile(t, d, "a.jsonl", legacyMessageLog) }, 1, 0},
		"jsonl.bak only":        {func(d string) { writeLegacyFile(t, d, "a.jsonl.bak", legacyMessageLog) }, 1, 0},
		"unknown jsonl format":  {func(d string) { writeLegacyFile(t, d, "a.jsonl", `{"schema":2,"kind":"header"}`+"\n") }, 0, 1},
		"unreadable event log":  {func(d string) { writeLegacyFile(t, d, "a.events.jsonl", "not json at all\n") }, 0, 1},
		"unknown format in sub": {func(d string) { writeLegacyFile(t, filepath.Join(d, "slug"), "a.jsonl", `{"kind":"x"}`+"\n") }, 0, 1},
		"workspace is gone": {func(d string) {
			writeLegacyFile(t, d, "a.jsonl", legacyMessageLog)
			writeLegacyFile(t, d, "a.meta.json", `{"workspace":"/no/such/workspace","summary":"s"}`)
		}, 1, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			home := isolateMigrationHome(t)
			picked := filepath.Join(home, "D", "项目", "网络")
			tc.seed(filepath.Join(picked, "sessions"))
			res := RunLegacySessionImportInto(picked, filepath.Join(config.SessionDir(), "ws"), event.Discard)
			if got := totalImported(res.SessionImports); got != tc.imported || len(res.SessionErrs) != tc.warnings {
				t.Fatalf("imported %d, warnings %d (%v); want %d and %d", got, len(res.SessionErrs), res.SessionErrs, tc.imported, tc.warnings)
			}
		})
	}
}

func TestExplicitImportFindsSessionsAddedAfterAnEarlierRun(t *testing.T) {
	home := isolateMigrationHome(t)
	picked := filepath.Join(home, "网络")
	sessions := filepath.Join(picked, ".reasonix", "sessions")
	dest := filepath.Join(config.SessionDir(), "ws")
	writeLegacyFile(t, sessions, "a.jsonl", legacyMessageLog)
	if got := totalImported(RunLegacySessionImportInto(picked, dest, event.Discard).SessionImports); got != 1 {
		t.Fatalf("first run imported %d, want 1", got)
	}
	writeLegacyFile(t, sessions, "b.jsonl", legacyMessageLog)
	if got := totalImported(RunLegacySessionImportInto(picked, dest, event.Discard).SessionImports); got != 1 {
		t.Fatalf("second run imported %d, want the new session only", got)
	}
}
