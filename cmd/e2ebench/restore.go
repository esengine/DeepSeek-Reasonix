package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Restoring the source is I/O like any other step, but a failure used to read as
// success: the analysis went on to report a verdict about a tree that was still
// mutated. These helpers make it loud, and are variables so tests can inject one.

var (
	writeFile = os.WriteFile
	readFile  = os.ReadFile
)

// restoreSource writes the original bytes back and proves they are back.
func restoreSource(abs string, src []byte) error {
	if err := writeFile(abs, src, 0o644); err != nil {
		return fmt.Errorf("restore %s: %w", abs, err)
	}
	got, err := readFile(abs)
	if err != nil {
		return fmt.Errorf("verify %s: %w", abs, err)
	}
	if !bytes.Equal(got, src) {
		return fmt.Errorf("verify %s: %d bytes on disk, %d expected", abs, len(got), len(src))
	}
	return nil
}

// treeMatches checks files against a revision. A checkout that exited zero is
// not evidence the tree moved; the tree is.
func treeMatches(repo, rev string, files []string) error {
	args := append([]string{"-C", repo, "diff", "--exit-code", rev, "--"}, files...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s does not match %s after restore: %v: %s", repo, rev, err, strings.TrimSpace(string(out)))
	}
	return nil
}
