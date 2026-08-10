package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A failed task is only actionable if the record says what the agent actually
// produced. For an exploration task the grader's "want X, got Y" line is the
// entire finding — a bare false would leave every failure indistinguishable.
func TestGradeVerboseKeepsWhatTheGraderSaid(t *testing.T) {
	taskDir := t.TempDir()
	verify := "#!/usr/bin/env bash\necho \"answer.txt normalized to 'askrigg', want 'gorsefen'\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte(verify), 0o755); err != nil {
		t.Fatal(err)
	}

	passed, said := gradeVerbose(t.TempDir(), taskDir)
	if passed {
		t.Fatal("a grader exiting non-zero must not pass")
	}
	if !strings.Contains(said, "want 'gorsefen'") {
		t.Fatalf("grader output lost: %q", said)
	}
}

func TestGradeVerboseStaysQuietWhenTheTaskPasses(t *testing.T) {
	taskDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if passed, said := gradeVerbose(t.TempDir(), taskDir); !passed || said != "" {
		t.Fatalf("clean grade = %v, %q", passed, said)
	}
}

// The excerpt is bounded so a chatty grader cannot crowd out the report, and
// bounded on runes so a multi-byte log cannot be cut mid-character.
func TestUtf8PrefixBoundsOnRunes(t *testing.T) {
	if got := utf8Prefix("short", 10); got != "short" {
		t.Errorf("under the limit was altered: %q", got)
	}
	got := utf8Prefix("这是一个很长的输出", 4)
	if []rune(got)[4] != '…' || len([]rune(got)) != 5 {
		t.Errorf("utf8Prefix = %q, want 4 runes plus an ellipsis", got)
	}
}

func TestAppendNoteKeepsBothCauses(t *testing.T) {
	if got := appendNote("", "grader: x"); got != "grader: x" {
		t.Errorf("first note = %q", got)
	}
	// A run that both errored and failed grading must report each: the run
	// error explains the grade, and dropping it hides why.
	if got := appendNote("run: exit 1", "grader: want y"); !strings.Contains(got, "run: exit 1") || !strings.Contains(got, "grader: want y") {
		t.Errorf("combined note lost a cause: %q", got)
	}
}
