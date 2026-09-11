package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanMemoryRecallCountsAndPointOfUse(t *testing.T) {
	dir := t.TempDir()
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
	dir := t.TempDir()
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
	taskDir := t.TempDir()
	work := t.TempDir()
	for _, seed := range []string{"project/fact.md", "global/pref.md"} {
		p := filepath.Join(taskDir, "memory", seed)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: x\ndescription: y\n---\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env, err := seedTaskMemory(taskDir, work)
	if err != nil || len(env) != 1 || !strings.HasPrefix(env[0], "REASONIX_STATE_HOME=") {
		t.Fatalf("env = %v err = %v", env, err)
	}
	stateHome := strings.TrimPrefix(env[0], "REASONIX_STATE_HOME=")
	if _, err := os.Stat(filepath.Join(stateHome, "memory", "global", "pref.md")); err != nil {
		t.Fatalf("global seed missing: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(stateHome, "projects", "*", "memory", "fact.md"))
	if len(matches) != 1 {
		t.Fatalf("project seed not under the work dir's slug: %v", matches)
	}

	if env, err := seedTaskMemory(t.TempDir(), work); err != nil || env != nil {
		t.Fatalf("task without seeds must be a no-op, got %v %v", env, err)
	}
}
