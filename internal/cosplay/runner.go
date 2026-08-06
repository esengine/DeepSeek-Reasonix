package cosplay

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ProcessRunner executes candidate+test cells with a local interpreter or
// compiler. It writes a combined source file to a temp dir and runs it,
// deciding pass/fail from the exit code and the test's own GOT/EXPECTED/PASS
// markers (the format emitted by TemplateGenerator; arbitrary candidate tests
// are judged by exit code alone).
//
// Go and Python are supported out of the box; the commands are looked up in
// PATH at run time (override via GoCmd/PythonCmd for sandboxed toolchains).
type ProcessRunner struct {
	GoCmd     string
	PythonCmd string
	Timeout   time.Duration
	// TempDir overrides the base temp directory (default os.TempDir()).
	TempDir string
}

// Run implements Runner.
func (r *ProcessRunner) Run(ctx context.Context, cand Candidate, test TestCase) (bool, string, string, error) {
	lang := strings.ToLower(cand.Language)
	if lang == "" {
		lang = strings.ToLower(test.Language)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	switch lang {
	case "go":
		return r.runGo(ctx, cand, test, timeout)
	case "python", "py":
		return r.runPython(ctx, cand, test, timeout)
	default:
		return false, "", "", fmt.Errorf("cosplay: no runner for language %q", lang)
	}
}

func (r *ProcessRunner) runGo(ctx context.Context, cand Candidate, test TestCase, timeout time.Duration) (bool, string, string, error) {
	cmdName := r.GoCmd
	if cmdName == "" {
		cmdName = "go"
	}
	if _, err := exec.LookPath(cmdName); err != nil {
		return false, "", "", fmt.Errorf("cosplay: go toolchain not found (%v)", err)
	}
	dir, err := os.MkdirTemp(r.TempDir, "cosplay-go-")
	if err != nil {
		return false, "", "", err
	}
	defer os.RemoveAll(dir)
	// Combined source: the test provides package main + main(); the candidate
	// code (function/type definitions) follows with its own package header
	// stripped so a full-file candidate still composes cleanly.
	src := test.Body + "\n" + stripPackageHeader(cand.Code) + "\n"
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		return false, "", "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cmdName, "run", file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, "", string(out), nil // compile/run failure = test failed
	}
	return judge(string(out))
}

func (r *ProcessRunner) runPython(ctx context.Context, cand Candidate, test TestCase, timeout time.Duration) (bool, string, string, error) {
	cmdName := r.PythonCmd
	if cmdName == "" {
		cmdName = "python"
	}
	if _, err := exec.LookPath(cmdName); err != nil {
		return false, "", "", fmt.Errorf("cosplay: python not found (%v)", err)
	}
	dir, err := os.MkdirTemp(r.TempDir, "cosplay-py-")
	if err != nil {
		return false, "", "", err
	}
	defer os.RemoveAll(dir)
	src := cand.Code + "\n" + test.Body + "\n"
	file := filepath.Join(dir, "main.py")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		return false, "", "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cmdName, file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, "", string(out), nil
	}
	return judge(string(out))
}

// judge interprets the GOT/EXPECTED/PASS markers the template tests emit.
// Explicit PASS wins; an EXPECTED marker (got != want) is a failure; plain
// successful runs without markers pass (smoke tests).
func judge(out string) (bool, string, string, error) {
	if strings.Contains(out, "PASS") {
		return true, "", "", nil
	}
	if idx := strings.Index(out, "EXPECTED:"); idx >= 0 {
		got := ""
		if g := strings.Index(out, "GOT:"); g >= 0 && g < idx {
			got = strings.TrimSpace(strings.TrimSuffix(out[g+4:idx], "\n"))
		}
		return false, got, "mismatch: " + strings.TrimSpace(out), nil
	}
	return true, "", "", nil
}

// stripPackageHeader removes a leading package declaration (and any comment
// lines before it) so a full-file candidate can be embedded after the test's
// own package main declaration.
func stripPackageHeader(code string) string {
	lines := strings.Split(code, "\n")
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		if strings.HasPrefix(t, "package ") {
			return strings.Join(append(lines[:i], lines[i+1:]...), "\n")
		}
		break
	}
	return code
}
