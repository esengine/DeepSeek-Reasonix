package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/testenv"
)

const allRetiredKeys = `default_model = "deepseek-pro"

[agent]
max_steps = 9
memory_compiler = "compact"
soft_compact_ratio = 0.5
temperature = 0.7

[secrets]
redact_tool_output = true
filter_subprocess_env = true
`

func realPath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

func countingReader(mu *sync.Mutex, reads map[string]int) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		mu.Lock()
		reads[realPath(path)]++
		mu.Unlock()
		return fileencoding.ReadFileUTF8(path)
	}
}

func writeRetiredFixtures(t *testing.T) (root, userPath, projectPath string) {
	t.Helper()
	isolateUserConfigHome(t)
	root = testenv.TempDir(t)
	userPath = UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	projectPath = filepath.Join(root, "reasonix.toml")
	for _, p := range []string{userPath, projectPath} {
		if err := os.WriteFile(p, []byte(allRetiredKeys), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, userPath, projectPath
}

// Four independent migrations over the same user and project files read each
// of them once, and the result is the same text four sequential rewrites left.
func TestRetiredKeyMigrationsReadEachConfigOnce(t *testing.T) {
	root, userPath, projectPath := writeRetiredFixtures(t)
	var mu sync.Mutex
	reads := map[string]int{}
	all := []retiredKey{retiredStepLimits, retiredRedactToolOutput, retiredMemoryCompiler, retiredMultiThreshold}

	res := processRoots().migrateRetiredKeys(root, countingReader(&mu, reads), all)
	for i, r := range res {
		if r.Err != nil || !r.Changed {
			t.Fatalf("migration %d = %+v, want changed without error", i, r)
		}
	}
	for _, p := range []string{userPath, projectPath} {
		if reads[realPath(p)] != 1 {
			t.Fatalf("%s read %d times, want once: %v", p, reads[realPath(p)], reads)
		}
		raw, _ := os.ReadFile(p)
		want := "default_model = \"deepseek-pro\"\n\n[agent]\ntemperature = 0.7\n\n[secrets]\nfilter_subprocess_env = true\n"
		if string(raw) != want {
			t.Fatalf("%s after migration:\n%s\nwant:\n%s", p, raw, want)
		}
	}

	again := processRoots().migrateRetiredKeys(root, countingReader(&mu, reads), all)
	for i, r := range again {
		if r.Changed || r.Err != nil {
			t.Fatalf("second run migration %d = %+v, want one-shot", i, r)
		}
	}
}

// The counter sees what the separate migrations cost: one read of each file per
// migration.
func TestRetiredKeyMigrationsRunSeparatelyReadPerMigration(t *testing.T) {
	root, userPath, _ := writeRetiredFixtures(t)
	var mu sync.Mutex
	reads := map[string]int{}
	for _, m := range []retiredKey{retiredStepLimits, retiredRedactToolOutput, retiredMemoryCompiler, retiredMultiThreshold} {
		processRoots().migrateRetiredKeys(root, countingReader(&mu, reads), []retiredKey{m})
	}
	if got := reads[realPath(userPath)]; got != 4 {
		t.Fatalf("separate migrations read the user config %d times, want 4", got)
	}
}

// A migration that finds nothing to strip leaves the others' result and the
// file untouched by it.
func TestRetiredKeyMigrationsReportChangeIndependently(t *testing.T) {
	isolateUserConfigHome(t)
	root := testenv.TempDir(t)
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("[agent]\nmemory_compiler = \"compact\"\ntemperature = 0.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := processRoots().MigrateRetiredKeysForRoot(root)
	if !got.MemoryCompiler.Changed || got.StepLimits.Changed || got.RedactToolOutput.Changed || got.MultiThreshold.Changed {
		t.Fatalf("results = %+v, want only memory_compiler changed", got)
	}
	raw, _ := os.ReadFile(userPath)
	if strings.Contains(string(raw), "memory_compiler") || !strings.Contains(string(raw), "temperature = 0.7") {
		t.Fatalf("user config after migration:\n%s", raw)
	}
}

// A failing file stops every migration at that file with its own wrapped error.
func TestRetiredKeyMigrationsErrorsNameEachMigration(t *testing.T) {
	isolateUserConfigHome(t)
	root := testenv.TempDir(t)
	userPath := UserConfigPath()
	if err := os.MkdirAll(userPath, 0o755); err != nil {
		t.Fatal(err)
	}
	got := processRoots().MigrateRetiredKeysForRoot(root)
	for name, r := range map[string]RetiredKeyResult{
		"deprecated agent step limits":               got.StepLimits,
		"deprecated redact_tool_output":              got.RedactToolOutput,
		"deprecated memory_compiler":                 got.MemoryCompiler,
		"deprecated multi-threshold compaction keys": got.MultiThreshold,
	} {
		if r.Err == nil || !strings.Contains(r.Err.Error(), "migrate "+name+" in "+userPath) || r.Changed {
			t.Fatalf("%s result = %+v, want its own error naming the file", name, r)
		}
	}
}
