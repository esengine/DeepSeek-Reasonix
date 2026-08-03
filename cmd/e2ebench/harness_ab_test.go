package main

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExactMcNemarP(t *testing.T) {
	tests := []struct {
		baselineOnly, candidateOnly int
		want                        float64
	}{
		{0, 0, 1},
		{0, 8, 0.0078125},
		{8, 0, 0.0078125},
		{1, 5, 0.21875},
	}
	for _, tt := range tests {
		got := exactMcNemarP(tt.baselineOnly, tt.candidateOnly)
		if math.Abs(got-tt.want) > 1e-12 {
			t.Errorf("exactMcNemarP(%d, %d) = %.12f, want %.12f", tt.baselineOnly, tt.candidateOnly, got, tt.want)
		}
	}
}

func TestSanitizeABErrorRemovesLocalPaths(t *testing.T) {
	got := sanitizeABError(errors.New("copy /Users/alice/private/task to /tmp/run/work"),
		"/Users/alice/private/task", "<task>", "/tmp/run/work", "<workdir>")
	if got != "copy <task> to <workdir>" {
		t.Fatalf("sanitized error = %q", got)
	}
}

func TestHashTreeIncludesEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	withEmpty, err := hashTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(empty); err != nil {
		t.Fatal(err)
	}
	withoutEmpty, err := hashTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if withEmpty == withoutEmpty {
		t.Fatal("tree digest ignored an empty directory that copyDir preserves")
	}
}

func TestBuildABProjectionUsesLatestCellAndAllAttemptCosts(t *testing.T) {
	manifest := testABManifest(1, "task")
	records := []abRecord{
		testABRecord("baseline", "task", 1, 1, abOutcomeInfraFailed, false, false, 3),
		testABRecord("baseline", "task", 1, 2, abOutcomePassed, true, true, 5),
		testABRecord("candidate", "task", 1, 1, abOutcomeVerificationFailed, true, false, 7),
	}
	projection := buildABProjection(manifest, records)
	if got, want := len(projection.Cells), 2; got != want {
		t.Fatalf("latest cells = %d, want %d", got, want)
	}
	baseline := projection.Arms[0]
	if baseline.Passed != 1 || baseline.Scored != 1 || baseline.InfraAttempts != 1 {
		t.Fatalf("baseline summary = %+v", baseline)
	}
	if got, want := baseline.PromptTokens, 8; got != want {
		t.Fatalf("baseline prompt tokens = %d, want all-attempt total %d", got, want)
	}
	if projection.Paired.BaselineOnly != 1 || projection.Paired.EligiblePairs != 1 {
		t.Fatalf("paired summary = %+v", projection.Paired)
	}
}

func TestLoadABRecordsRepairsTornTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "attempts.jsonl")
	manifest := testABManifest(1, "task")
	want := testABRecord("baseline", "task", 1, 1, abOutcomePassed, true, true, 1)
	if err := appendABRecord(path, want); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"schema_version":1,"run_id":"run"`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := loadABRecords(path, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []abRecord{want}) {
		t.Fatalf("records = %#v, want %#v", got, []abRecord{want})
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(body), "\n") || strings.Count(string(body), "\n") != 1 {
		t.Fatalf("WAL was not repaired: %q", body)
	}

	candidate := testABRecord("candidate", "task", 1, 1, abOutcomeVerificationFailed, true, false, 2)
	if err := appendABRecord(path, candidate); err != nil {
		t.Fatal(err)
	}
	got, err = loadABRecords(path, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("records after append = %d, want 2", len(got))
	}
}

func TestLoadABRecordsRepairsMissingFinalNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "attempts.jsonl")
	manifest := testABManifest(1, "task")
	want := testABRecord("baseline", "task", 1, 1, abOutcomePassed, true, true, 1)
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadABRecords(path, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []abRecord{want}) {
		t.Fatalf("records = %#v, want %#v", got, []abRecord{want})
	}
	repaired, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(repaired), "\n") {
		t.Fatalf("missing final newline was not repaired: %q", repaired)
	}
}

func TestCloseInterruptedABAdmissionBecomesInfrastructureFailure(t *testing.T) {
	runDir := t.TempDir()
	walPath := filepath.Join(runDir, "attempts.jsonl")
	manifest := testABManifest(1, "task")
	admission := abRecord{
		SchemaVersion: abSchemaVersion, Event: abEventAdmissionStarted, RunID: manifest.RunID,
		ArmID: "baseline", TaskID: "task", Repetition: 1, Attempt: 1,
		StartedAt: "2026-01-01T00:00:00Z",
	}
	if err := appendABRecord(walPath, admission); err != nil {
		t.Fatal(err)
	}
	records := []abRecord{admission}
	latest := latestABRecords(records)
	if err := closeInterruptedABAdmissions(runDir, walPath, manifest, &records, &latest); err != nil {
		t.Fatal(err)
	}
	if got, want := len(records), 2; got != want {
		t.Fatalf("records = %d, want admission + closure (%d)", got, want)
	}
	closed := latest[abCellKey{armID: "baseline", taskID: "task", repetition: 1}]
	if closed.Event != abEventAttemptFinished || closed.Outcome != abOutcomeInfraFailed || closed.Scored {
		t.Fatalf("closed admission = %+v", closed)
	}
	loaded, err := loadABRecords(walPath, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, records) {
		t.Fatalf("loaded records = %#v, want %#v", loaded, records)
	}
}

func TestPrepareABManifestRefusesChangedSuite(t *testing.T) {
	suite, tasks := writeABTestSuite(t)
	runDir := filepath.Join(t.TempDir(), "run")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opts := harnessABOpts{
		suite: suite, runDir: runDir, runID: "fixed-run", baselineBin: binary, candidateBin: binary,
		baselineProfile: benchmarkProfileBaseline, candidateProfile: benchmarkProfileBaseline,
		repetitions: 1, infraRetries: 1,
	}
	first, err := prepareABManifest(opts, tasks)
	if err != nil {
		t.Fatal(err)
	}
	publicManifest, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publicManifest), filepath.Dir(suite)) || strings.Contains(string(publicManifest), binary) {
		t.Fatalf("manifest leaked an absolute local path: %s", publicManifest)
	}
	if first.Arms[0].binaryPath == "" {
		t.Fatal("runtime binary path was not retained in memory")
	}
	second, err := prepareABManifest(opts, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("unchanged manifest did not resume identically")
	}
	verify := filepath.Join(suite, "tasks", "task", "verify.sh")
	if err := os.WriteFile(verify, []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareABManifest(opts, tasks); err == nil || !strings.Contains(err.Error(), "frozen manifest mismatch") {
		t.Fatalf("changed suite error = %v, want frozen manifest mismatch", err)
	}
}

func TestRunHarnessABResumesWithoutDuplicateAttempts(t *testing.T) {
	suite, _ := writeABTestSuite(t)
	root := t.TempDir()
	fake := filepath.Join(root, "fake-reasonix")
	script := `#!/usr/bin/env bash
set -eu
metrics=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--metrics" ]; then
    metrics="$2"
    shift 2
    continue
  fi
  shift
done
printf '{"prompt_tokens":10,"completion_tokens":2,"cache_hit_tokens":8,"cache_miss_tokens":2,"cost":0.01,"currency":"USD"}\n' > "$metrics"
printf 'ok\n' > answer.txt
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, "run")
	opts := harnessABOpts{
		suite: suite, runDir: runDir, runID: "resume-run", baselineBin: fake, candidateBin: fake,
		baselineProfile: benchmarkProfileBaseline, candidateProfile: benchmarkProfileBaseline,
		repetitions: 1, infraRetries: 1,
	}
	if err := runHarnessAB(opts); err != nil {
		t.Fatal(err)
	}
	manifest, ok, err := readABManifest(filepath.Join(runDir, "manifest.json"))
	if err != nil || !ok {
		t.Fatalf("read manifest: ok=%v err=%v", ok, err)
	}
	records, err := loadABRecords(filepath.Join(runDir, "attempts.jsonl"), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(records), 4; got != want {
		t.Fatalf("first run attempts = %d, want %d", got, want)
	}
	for _, record := range records {
		if record.Event == abEventAttemptFinished && record.FinishedAt == "" {
			t.Fatalf("finished event has no finished_at: %+v", record)
		}
	}
	if err := runHarnessAB(opts); err != nil {
		t.Fatal(err)
	}
	records, err = loadABRecords(filepath.Join(runDir, "attempts.jsonl"), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(records), 4; got != want {
		t.Fatalf("resumed attempts = %d, want no duplicates (%d)", got, want)
	}
	for _, name := range []string{"manifest.json", "attempts.jsonl", "results.json", "results.csv", "report.md"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Errorf("artifact %s: %v", name, err)
		}
	}
	report, err := os.ReadFile(filepath.Join(runDir, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "**Status:** complete") || !strings.Contains(string(report), "**Paired cells:** 1/1") {
		t.Fatalf("unexpected report:\n%s", report)
	}
}

func testABManifest(infraRetries int, taskIDs ...string) abManifest {
	tasks := make([]abTaskManifest, 0, len(taskIDs))
	for _, id := range taskIDs {
		tasks = append(tasks, abTaskManifest{ID: id, SHA256: "task-hash", TimeoutSec: 10})
	}
	return abManifest{
		SchemaVersion: abSchemaVersion, HarnessProtocol: abHarnessProtocol, RunID: "run", CreatedAt: "2026-01-01T00:00:00Z",
		Suite: abSuiteManifest{Path: "/suite", SHA256: "suite-hash"}, Repetitions: 1, InfraRetries: infraRetries,
		Arms: []abArmManifest{
			{ID: "baseline", Binary: "/baseline", BinarySHA256: "base-hash", Profile: benchmarkProfileBaseline},
			{ID: "candidate", Binary: "/candidate", BinarySHA256: "candidate-hash", Profile: benchmarkProfileBaseline},
		},
		Tasks: tasks,
	}
}

func testABRecord(armID, taskID string, repetition, attempt int, outcome string, scored, passed bool, promptTokens int) abRecord {
	return abRecord{
		SchemaVersion: abSchemaVersion, Event: abEventAttemptFinished, RunID: "run", ArmID: armID, TaskID: taskID,
		Repetition: repetition, Attempt: attempt, StartedAt: "2026-01-01T00:00:00Z", FinishedAt: "2026-01-01T00:00:01Z",
		DurationMS: 1000, Outcome: outcome, Scored: scored, Passed: passed,
		MetricsAvailable: true,
		Metrics:          runMetrics{PromptTokens: promptTokens, CacheHitTokens: promptTokens, Cost: float64(promptTokens) / 100, Currency: "USD"},
	}
}

func writeABTestSuite(t *testing.T) (string, []task) {
	t.Helper()
	suite := t.TempDir()
	taskDir := filepath.Join(suite, "tasks", "task")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	taskBody := "prompt = \"create answer.txt\"\nmax_steps = 4\ntimeout_sec = 10\n"
	if err := os.WriteFile(filepath.Join(taskDir, "task.toml"), []byte(taskBody), 0o644); err != nil {
		t.Fatal(err)
	}
	verify := "#!/usr/bin/env bash\nset -eu\ntest -f answer.txt\n"
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte(verify), 0o755); err != nil {
		t.Fatal(err)
	}
	tasks, err := loadTasks(suite)
	if err != nil {
		t.Fatal(err)
	}
	return suite, tasks
}
