package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestScanMemoryRecallCountsAndPointOfUse(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "run.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"event":{"kind":"tool_result","tool":{"args":"{\"command\":\"make check-fast --tag=MEMKEY-EARLY\"}"}}}`,
		`{"seq":2,"memory_recall":{"hits":[{"id":"a"},{"id":"b"}],"used_chars":420}}`,
		`{"seq":3,"event":{"kind":"tool_result","tool":{"args":"{\"path\":\"answer.txt\",\"content\":\"make check-fast --tag=MEMKEY-USED\"}"}}}`,
		`{"seq":4,"memory_recall":{"suppressed":"generic user turn"}}`,
		`{"seq":5,"event":{"kind":"text","text":"done, MEMKEY-TEXT too"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stats := scanMemoryRecall(path, []string{"MEMKEY-USED", "MEMKEY-TEXT", "MEMKEY-EARLY", "MEMKEY-NEVER"}, false)
	if stats.RecallEvents != 1 || stats.RecallHits != 2 || stats.RecallChars != 420 || stats.Suppressed != 1 {
		t.Fatalf("stats = %+v, want 1 event / 2 hits / 420 chars / 1 suppressed", stats)
	}
	// MEMKEY-EARLY appears only BEFORE the recall: not point-of-use evidence.
	if stats.MarkersUsed != 2 {
		t.Fatalf("markers used = %d, want 2 (post-recall args + answer text only)", stats.MarkersUsed)
	}
}

func TestMemoryUtilitySectionPairsArms(t *testing.T) {
	dir := testenv.TempDir(t)
	on := []result{
		{task: task{ID: "helped"}, Passed: true, MemoryRecallEvents: 1, MemoryRecallChars: 300},
		{task: task{ID: "hurt"}, Passed: false, MemoryRecallEvents: 1, MemoryRecallChars: 500},
		{task: task{ID: "same"}, Passed: true, MemoryRecallEvents: 1, MemoryRecallChars: 100},
	}
	off := []result{
		{task: task{ID: "helped"}, Passed: false},
		{task: task{ID: "hurt"}, Passed: true},
		{task: task{ID: "same"}, Passed: true},
	}
	onPath, offPath := filepath.Join(dir, "on.json"), filepath.Join(dir, "off.json")
	for path, rows := range map[string][]result{onPath: on, offPath: off} {
		data, _ := json.Marshal(rows)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	section := memoryUtilitySection(offPath, onPath) // order must not matter
	for _, want := range []string{"Memory utility", "3 paired tasks", "helpful** 1", "harmful** 1", "helped", "hurt"} {
		if !strings.Contains(section, want) {
			t.Fatalf("section missing %q:\n%s", want, section)
		}
	}
}

func TestSeedTaskMemoryBuildsIsolatedStateRoot(t *testing.T) {
	taskDir := testenv.TempDir(t)
	work := testenv.TempDir(t)
	for _, seed := range []string{"project/fact.md", "global/pref.md"} {
		p := filepath.Join(taskDir, "memory", seed)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: x\ndescription: y\n---\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stateHome := testenv.TempDir(t)
	if err := seedTaskMemory(taskDir, work, stateHome); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateHome, "memory", "global", "pref.md")); err != nil {
		t.Fatalf("global seed missing: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(stateHome, "projects", "*", "memory", "fact.md"))
	if len(matches) != 1 {
		t.Fatalf("project seed not under the work dir's slug: %v", matches)
	}

	if err := seedTaskMemory(testenv.TempDir(t), work, testenv.TempDir(t)); err != nil {
		t.Fatalf("task without seeds must be a no-op, got %v", err)
	}
}

// Every task gets a state root of its own, seeded or not. Sharing one leaves
// each finished task's whole event stream on disk under a directory named for
// its work dir, and one observed run found exactly that by grepping the store
// for its own task id while looking for an answer it was not meant to have.
func TestTaskExperimentEnvIsolatesEveryRun(t *testing.T) {
	isolateStateBase(t)
	roots := map[string]bool{}
	for _, id := range []string{"nosol-absent-oracle", "fix-add-bug"} {
		env, drop, note := taskExperimentEnv(suiteConfig{}, task{ID: id, dir: testenv.TempDir(t)}, nestedWorkdir(t))
		defer drop()
		if note != "" {
			t.Fatalf("%s: %s", id, note)
		}
		root := ""
		for _, e := range env {
			if after, ok := strings.CutPrefix(e, "REASONIX_STATE_HOME="); ok {
				root = after
			}
		}
		if root == "" {
			t.Fatalf("%s ran against the operator's own store root", id)
		}
		if strings.Contains(root, id) {
			t.Errorf("%s: state root %q carries the task id, which is what a run greps for", id, root)
		}
		roots[root] = true
		tmp := ""
		for _, e := range env {
			if after, ok := strings.CutPrefix(e, "TMPDIR="); ok {
				tmp = after
			}
		}
		if tmp == "" {
			t.Errorf("%s inherited the host temp root, where an earlier run's leftovers are still readable", id)
		}
		roots[tmp] = true
	}
	// Two tasks, two roots each, all distinct: a shared one is what lets a
	// no-solution task find the dependency an earlier run compiled for itself.
	if len(roots) != 4 {
		t.Fatalf("two tasks did not get four distinct roots: %v", roots)
	}
}

// isolateStateBase points state roots at a directory of the test's own, so the
// suite never writes into the operator's user cache.
func isolateStateBase(t *testing.T) string {
	t.Helper()
	base := testenv.TempDir(t)
	prev := benchStateBase
	benchStateBase = func() (string, error) { return base, nil }
	t.Cleanup(func() { benchStateBase = prev })
	return base
}

// nestedWorkdir is a workdir whose parent is private to the test, so a state
// base made by isolateStateBase is not inside it.
func nestedWorkdir(t *testing.T) string {
	t.Helper()
	work := filepath.Join(testenv.TempDir(t), "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	return work
}

func stateHomeOf(env []string) string {
	for _, e := range env {
		if after, ok := strings.CutPrefix(e, "REASONIX_STATE_HOME="); ok {
			return after
		}
	}
	return ""
}

func seededTask(t *testing.T) task {
	t.Helper()
	dir := testenv.TempDir(t)
	for _, seed := range []string{"project/fact.md", "global/pref.md"} {
		p := filepath.Join(dir, "memory", seed)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: x\ndescription: y\n---\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return task{ID: "mb-exact", dir: dir}
}

func memoryFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// The off arm's treatment is that no memory exists, not that the memory tool
// is hidden: a seeded store left on disk is one shell command away.
func TestMemoryOffArmSeedsNoMemory(t *testing.T) {
	isolateStateBase(t)
	tk := seededTask(t)

	env, drop, note := taskExperimentEnv(suiteConfig{policy: "memory-off"}, tk, nestedWorkdir(t))
	defer drop()
	if note != "" {
		t.Fatal(note)
	}
	home := stateHomeOf(env)
	if home == "" {
		t.Fatal("memory-off arm ran against the operator's own store root")
	}
	if files := memoryFilesUnder(t, home); len(files) != 0 {
		t.Fatalf("memory-off state root holds seeded memory: %v", files)
	}

	onEnv, onDrop, onNote := taskExperimentEnv(suiteConfig{}, tk, nestedWorkdir(t))
	defer onDrop()
	if onNote != "" {
		t.Fatal(onNote)
	}
	if files := memoryFilesUnder(t, stateHomeOf(onEnv)); len(files) != 2 {
		t.Fatalf("memory-on arm must carry both seeds, got %v", files)
	}
}

// The state root must not sit beside the workdir: a run that walks up from its
// workspace and searches the temp root would otherwise reach the seeded store.
func TestStateHomeIsOutsideWorkdirParent(t *testing.T) {
	work, err := taskWorkdir(suiteConfig{}, "mb-exact")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	env, drop, note := taskExperimentEnv(suiteConfig{}, seededTask(t), work)
	defer drop()
	if note != "" {
		t.Fatal(note)
	}
	home := stateHomeOf(env)
	if home == "" {
		t.Fatal("no state root")
	}
	if within(resolvedPath(filepath.Dir(work)), resolvedPath(home)) {
		t.Fatalf("state root %s is inside the workdir's parent %s", home, filepath.Dir(work))
	}
	if strings.Contains(filepath.Base(home), "e2ebench") || strings.Contains(home, "mb-exact") {
		t.Errorf("state root %s is named after the workdir or task", home)
	}
}

func TestMakeStateHomeRefusesWorkdirParent(t *testing.T) {
	parent := testenv.TempDir(t)
	prev := benchStateBase
	benchStateBase = func() (string, error) { return filepath.Join(parent, "state"), nil }
	t.Cleanup(func() { benchStateBase = prev })
	if home, err := makeStateHome(filepath.Join(parent, "work")); err == nil {
		t.Fatalf("state root %s accepted beside the workdir", home)
	}
}

func TestAbsTrajectoryDirAnchorsRelativePaths(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := absTrajectoryDir("t-off"), filepath.Join(cwd, "t-off"); got != want {
		t.Fatalf("relative -trajectories resolved to %q, want %q", got, want)
	}
	abs := filepath.Join(testenv.TempDir(t), "t-on")
	if got := absTrajectoryDir(abs); got != abs {
		t.Fatalf("absolute -trajectories changed: %q", got)
	}
	if got := absTrajectoryDir(""); got != "" {
		t.Fatalf("unset -trajectories became %q", got)
	}
}
