package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/historycatalog"
	"reasonix/internal/state/projectiondb"
)

// catalogCacheDir points the disposable projections at a directory this test
// owns, so `doctor catalogs` reads exactly what the test seeded.
func catalogCacheDir(t *testing.T) string {
	t.Helper()
	dir := testenv.TempDir(t)
	t.Setenv("REASONIX_CACHE_HOME", dir)
	return dir
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// reindexHistory runs the release path that writes a real catalog, so the
// fixture carries a failure the product itself recorded rather than one a test
// assigned to a private field.
func reindexHistory(t *testing.T, dirs ...string) {
	t.Helper()
	args := []string{"--json"}
	for _, dir := range dirs {
		args = append(args, "--dir", dir)
	}
	var code int
	_, stderr := captureCLIOutput(t, func() { code = reindexHistoryCatalog(args) })
	if code != 0 {
		t.Fatalf("reindex history exit=%d stderr=%s", code, stderr)
	}
}

// seedCorruptHistoryCatalog leaves one undecodable transcript behind: the
// catalog records it as health='corrupt' with the reason it failed.
func seedCorruptHistoryCatalog(t *testing.T) (catalogPath, badPath string) {
	t.Helper()
	sessions := testenv.TempDir(t)
	badPath = filepath.Join(sessions, "undecodable.jsonl")
	if err := os.WriteFile(badPath, []byte("this is not a session transcript\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reindexHistory(t, sessions)
	return historycatalog.DefaultPath(), badPath
}

// bumpSchemaVersion writes the ledger row a newer build's migration writes.
func bumpSchemaVersion(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES(?,1)`, version); err != nil {
		t.Fatal(err)
	}
}

func runDoctorCatalogs(t *testing.T, args ...string) (stdout string, code int) {
	t.Helper()
	stdout, _ = captureCLIOutput(t, func() { code = doctorCatalogsCommand(args) })
	return stdout, code
}

// doctorCatalogLine is the one line the command printed for a catalog; one line per
// catalog is part of what the command promises.
func doctorCatalogLine(t *testing.T, stdout, name string) string {
	t.Helper()
	var found []string
	for line := range strings.SplitSeq(strings.TrimSuffix(stdout, "\n"), "\n") {
		if strings.HasPrefix(line, "  "+name+" ") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one %q line, got %d:\n%s", name, len(found), stdout)
	}
	return found[0]
}

// A catalog that recorded a failed source is not ok. The old line said it was,
// which is why the person never ran the reindex that would name the source.
func TestDoctorCatalogsNamesARecordedSourceFailure(t *testing.T) {
	catalogCacheDir(t)
	catalogPath, badPath := seedCorruptHistoryCatalog(t)

	stdout, code := runDoctorCatalogs(t)
	if code != 0 {
		t.Fatalf("doctor catalogs exit=%d, want 0: unlike `doctor repair`, this only reports", code)
	}
	line := doctorCatalogLine(t, stdout, "history")
	// The exact line the defect printed for this fixture, kept as the thing
	// that must never come back.
	oldLine := fmt.Sprintf("  history  ok           schema=1 size=%d path=%s",
		fileSize(t, catalogPath), catalogPath)
	if line == oldLine {
		t.Fatalf("a catalog that recorded a failure still prints %q", oldLine)
	}
	prefix := fmt.Sprintf("  history  failed       schema=1 size=%d path=%s failed=1 source=%s: ",
		fileSize(t, catalogPath), catalogPath, badPath)
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("history line =\n  %q\nwant prefix\n  %q", line, prefix)
	}
	if reason := strings.TrimPrefix(line, prefix); strings.TrimSpace(reason) == "" {
		t.Fatalf("history line names the source but not why it failed: %q", line)
	}
	if strings.Contains(line, "  ok ") {
		t.Fatalf("history line still says ok: %q", line)
	}
}

// The runtime opens a newer schema as StateDegraded and serves memory; the
// catalog is not ok, it is read-only until the build catches up.
func TestDoctorCatalogsReportsFutureSchemaAsDegraded(t *testing.T) {
	catalogCacheDir(t)
	reindexHistory(t, testenv.TempDir(t))
	catalogPath := historycatalog.DefaultPath()
	bumpSchemaVersion(t, catalogPath, 2)
	// Prove the fixture is the state the runtime actually reaches.
	opened, err := historycatalog.Open(context.Background(), historycatalog.Options{Path: catalogPath})
	if err != nil {
		t.Fatal(err)
	}
	state := opened.Status().State
	if err := opened.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state != string(projectiondb.StateDegraded) {
		t.Fatalf("fixture: runtime state = %q, want degraded", state)
	}

	stdout, code := runDoctorCatalogs(t)
	if code != 0 {
		t.Fatalf("doctor catalogs exit=%d, want 0", code)
	}
	want := fmt.Sprintf("  history  degraded     schema=2 size=%d path=%s supported=1",
		fileSize(t, catalogPath), catalogPath)
	if line := doctorCatalogLine(t, stdout, "history"); line != want {
		t.Fatalf("history line =\n  %q\nwant\n  %q", line, want)
	}
}

func TestDoctorCatalogsHealthyCatalogStillSaysOK(t *testing.T) {
	catalogCacheDir(t)
	reindexHistory(t, testenv.TempDir(t))
	catalogPath := historycatalog.DefaultPath()

	stdout, code := runDoctorCatalogs(t)
	if code != 0 {
		t.Fatalf("doctor catalogs exit=%d, want 0", code)
	}
	want := fmt.Sprintf("  history  ok           schema=1 size=%d path=%s", fileSize(t, catalogPath), catalogPath)
	if line := doctorCatalogLine(t, stdout, "history"); line != want {
		t.Fatalf("history line =\n  %q\nwant the unchanged healthy line\n  %q", line, want)
	}
}

func TestDoctorCatalogsMissingCatalogStillSaysSo(t *testing.T) {
	catalogCacheDir(t)

	stdout, code := runDoctorCatalogs(t)
	if code != 0 {
		t.Fatalf("doctor catalogs exit=%d, want 0", code)
	}
	for _, command := range catalogCommands {
		want := fmt.Sprintf("  %-8s %-12s schema=0 size=0 path=%s", command.name, "missing", command.path())
		if line := doctorCatalogLine(t, stdout, command.name); line != want {
			t.Fatalf("%s line =\n  %q\nwant\n  %q", command.name, line, want)
		}
	}
}

// A script has to be able to act on the same facts the text says.
func TestDoctorCatalogsJSONCarriesTheRecordedFailure(t *testing.T) {
	catalogCacheDir(t)
	_, badPath := seedCorruptHistoryCatalog(t)

	stdout, code := runDoctorCatalogs(t, "--json")
	if code != 0 {
		t.Fatalf("doctor catalogs --json exit=%d, want 0", code)
	}
	history := decodeCatalogRows(t, stdout)["history"]
	for _, field := range []string{"name", "info"} {
		if _, ok := history[field]; !ok {
			t.Fatalf("healthy-shaped fields were dropped: no %q in %v", field, history)
		}
	}
	if got := jsonString(t, history["state"]); got != "failed" {
		t.Fatalf("state = %q, want failed", got)
	}
	if got := jsonNumber(t, history["failed"]); got != 1 {
		t.Fatalf("failed = %d, want 1", got)
	}
	var failures []struct {
		Path      string `json:"path"`
		Health    string `json:"health"`
		LastError string `json:"lastError"`
	}
	if err := json.Unmarshal(history["failures"], &failures); err != nil {
		t.Fatalf("failures: %v (%s)", err, history["failures"])
	}
	if len(failures) != 1 || failures[0].Path != badPath || failures[0].Health != "corrupt" || failures[0].LastError == "" {
		t.Fatalf("failures = %#v, want the corrupt source at %s with its reason", failures, badPath)
	}
}

func TestDoctorCatalogsJSONHealthyKeepsItsShape(t *testing.T) {
	catalogCacheDir(t)
	reindexHistory(t, testenv.TempDir(t))

	stdout, _ := runDoctorCatalogs(t, "--json")
	history := decodeCatalogRows(t, stdout)["history"]
	for field := range history {
		switch field {
		case "name", "info":
		default:
			t.Fatalf("healthy JSON grew a %q field a script did not see before: %v", field, history)
		}
	}
	var info map[string]json.RawMessage
	if err := json.Unmarshal(history["info"], &info); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"exists", "path", "schema", "size"} {
		if _, ok := info[field]; !ok {
			t.Fatalf("info lost %q: %v", field, info)
		}
	}
}

func decodeCatalogRows(t *testing.T, stdout string) map[string]map[string]json.RawMessage {
	t.Helper()
	var items []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	byName := map[string]map[string]json.RawMessage{}
	for _, item := range items {
		byName[jsonString(t, item["name"])] = item
	}
	return byName
}

func jsonString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s as string: %v", raw, err)
	}
	return value
}

func jsonNumber(t *testing.T, raw json.RawMessage) int64 {
	t.Helper()
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s as number: %v", raw, err)
	}
	return value
}

// The usage catalog records unreadable lines too; the same column must not say
// ok while it has.
func TestDoctorCatalogsUsageCorruptLinesAreNotOK(t *testing.T) {
	catalogCacheDir(t)
	t.Setenv("REASONIX_STATE_HOME", testenv.TempDir(t))
	stats := config.StatsDir()
	if err := os.MkdirAll(stats, 0o700); err != nil {
		t.Fatal(err)
	}
	day := filepath.Join(stats, "2026-08-02.jsonl")
	if err := os.WriteFile(day, []byte("not json at all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var code int
	if _, stderr := captureCLIOutput(t, func() { code = reindexUsageCatalog([]string{"--json"}) }); code != 0 {
		t.Fatalf("reindex usage exit=%d stderr=%s", code, stderr)
	}
	stdout, _ := runDoctorCatalogs(t)
	line := doctorCatalogLine(t, stdout, "usage")
	if !strings.Contains(line, "  failed ") || !strings.Contains(line, " corrupt-lines=1") {
		t.Fatalf("usage line = %q, want the recorded corrupt line reported as a count", line)
	}
}
