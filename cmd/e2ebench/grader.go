package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// graderNoteLimit bounds the grader excerpt kept on a result: enough for the
// "want X, got Y" line a grader leads with, never a whole test log.
const graderNoteLimit = 300

func appendNote(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}

func utf8Prefix(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

func grade(work, taskDir string) bool {
	passed, _ := gradeVerbose(work, taskDir)
	return passed
}

// gradeVerbose also returns what the grader printed. A failed task is only
// diagnosable if the record says what the agent actually produced, not merely
// that it was wrong — "answer.txt normalized to 'x', want 'y'" is the whole
// finding for an exploration task.
func gradeVerbose(work, taskDir string) (bool, string) {
	verify := filepath.Join(taskDir, "verify.sh")
	if !fileExists(verify) {
		return false, ""
	}
	dst := filepath.Join(work, "verify.sh")
	if err := copyFile(verify, dst); err != nil {
		return false, ""
	}
	cmd := exec.Command("bash", "verify.sh")
	cmd.Dir = work
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stderr, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	return cmd.Run() == nil, strings.TrimSpace(buf.String())
}
