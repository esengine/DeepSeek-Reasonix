package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// witness is one memorybench task's correct answer, as a set of files to write
// into a staged seed. Each answer is derived from the task's own facts, never
// from verify.sh: an expected value read out of the grader accepts whatever the
// grader accepts, which is the loop this table exists to break.
type witness struct {
	// from names the fact the answer comes from, never the grader.
	from  string
	files map[string]string
}

// memorybenchWitness holds the other half of the authoring rule corpus_test.go
// states: "One that can never pass is indistinguishable from a task with no
// solution." These tasks ship no solution/, so the reference-solution test skips
// them and nothing else asserts that their graders accept a correct answer.
var memorybenchWitness = map[string]witness{
	"mb-cjk": {
		from:  "memory/project/migration-naming.md: migrations are `MEMKEY-CJK5T-<description>.sql` at the repo root",
		files: map[string]string{"MEMKEY-CJK5T-add-users.sql": ""},
	},
	"mb-conflict": {
		from:  "memory/project/deploy-region.md: this project deploys to eu-central-1, overriding the global us-east-1",
		files: map[string]string{"answer.txt": "eu-central-1\n"},
	},
	"mb-contradiction": {
		from:  "memory/project/dependency-tooling.md (migrated to pnpm) and the seeded pnpm-lock.yaml",
		files: map[string]string{"answer.txt": "pnpm install\n"},
	},
	"mb-distractor": {
		from:  "memory/project/fast-check-command.md among the distractors",
		files: map[string]string{"answer.txt": "make check-fast --tag=MEMKEY-DIST5Q\n"},
	},
	"mb-exact": {
		from:  "memory/project/fast-check-command.md",
		files: map[string]string{"answer.txt": "make check-fast --tag=MEMKEY-EXACT7Q\n"},
	},
	"mb-generic": {
		from:  "the prompt, plus the seeded util.py that already defines sub",
		files: map[string]string{"util.py": "def sub(a, b):\n    return a - b\n\n\ndef add(a, b):\n    return a + b\n"},
	},
	"mb-history": {
		from:  "the seeded config/server.conf, whose line the prompt says to copy unchanged",
		files: map[string]string{"answer.txt": "timeout_seconds = 47   # MEMKEY-HIST6M tuned for p99\n"},
	},
	"mb-paraphrase": {
		from:  "memory/project/package-manager.md",
		files: map[string]string{"answer.txt": "pnpm install --tag=MEMKEY-PARA3X\n"},
	},
	"mb-pin": {
		from:  "memory/project/file-header-rule.md: a new text file's first line must be the stamp",
		files: map[string]string{"notes.txt": "# MEMKEY-PIN4D\nhello\n"},
	},
	"mb-stale": {
		from:  "the seeded RELEASE file, which supersedes the branch memory still names",
		files: map[string]string{"answer.txt": "release/1.21\n"},
	},
	"mb-symbol": {
		from:  "memory/project/retry-config-location.md: the key lives in policy/retry.toml, never .env",
		files: map[string]string{"policy/retry.toml": "# MEMKEY-SYM9K policy file\nmax_backoff_ms = 250\n"},
	},
	"mb-update": {
		from:  "memory/project/api-base.md: the v2 host replaced the dead v1 host",
		files: map[string]string{"answer.txt": "https://api.v2.example\n"},
	},
	"mb-v1miss-cjk-short": {
		from:  "memory/project/canary-ratio.md",
		files: map[string]string{"answer.txt": "7%\n"},
	},
	"mb-v1miss-crosslang": {
		from:  "memory/project/deploy-approver.md",
		files: map[string]string{"answer.txt": "release-captain-zhao\n"},
	},
	"mb-v1miss-symbol": {
		from:  "memory/project/idle-conn-config.md: the key lives in conf/server.toml, never env",
		files: map[string]string{"conf/server.toml": "# MEMKEY-VM1SYM\nmax_idle_conn_secs = 30\n"},
	},
}

// A grader that rejects a correct answer scores every honest run as a failure,
// so a corpus of them teaches that nothing is ever accepted. Every memorybench
// task ships memory/ facts instead of a solution/, so the witness table above
// is the only reference answer available.
func TestMemorybenchGradersAcceptTheReferenceAnswer(t *testing.T) {
	for _, bin := range []string{"bash", "python3"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s unavailable; the graders need a POSIX shell and python3", bin)
		}
	}
	tasks, err := loadTasks(memorybenchDir)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("no memorybench tasks found")
	}
	for _, task := range tasks {
		w, ok := memorybenchWitness[task.ID]
		if !ok {
			t.Errorf("%s has no witness: give it a correct answer derived from its own facts", task.ID)
			continue
		}
		t.Run(task.ID, func(t *testing.T) {
			t.Parallel()
			work := stageSeed(t, task.dir)
			for rel, content := range w.files {
				path := filepath.Join(work, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir for %s: %v", rel, err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatalf("write %s: %v", rel, err)
				}
			}
			if err := gradeSeed(t, work); err != nil {
				t.Fatalf("the grader rejects the answer its own facts imply (%s): %v", w.from, err)
			}
		})
	}
	for id := range memorybenchWitness {
		if _, err := os.Stat(filepath.Join(memorybenchDir, "tasks", id)); err != nil {
			t.Errorf("witness for %s has no task", id)
		}
	}
}
