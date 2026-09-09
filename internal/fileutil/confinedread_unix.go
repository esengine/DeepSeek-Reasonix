//go:build !windows && !plan9

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// OpenFileBeneath opens rel first, validates the identity held by that open
// descriptor against a resolved path beneath root, then returns the same
// descriptor for reading. A concurrent symlink or directory swap either fails
// the identity check or happens after the descriptor has pinned the safe file.
func OpenFileBeneath(root, rel string) (*os.File, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, fmt.Errorf("workspace root is empty")
	}
	rel, err := confinedRelativePath(rel)
	if err != nil {
		return nil, err
	}
	rootFile, err := os.Open(root)
	if err != nil {
		return nil, fmt.Errorf("open workspace root: %w", err)
	}
	defer rootFile.Close()
	openedRoot, err := rootFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat workspace root: %w", err)
	}
	abs := filepath.Join(root, rel)
	// O_NONBLOCK prevents a workspace FIFO from stalling admission before the
	// caller can inspect and reject its descriptor type. It is inert for regular
	// files, which are the only pinned-context targets accepted by the caller.
	file, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fail(fmt.Errorf("resolve workspace root: %w", err))
	}
	currentRoot, err := os.Stat(resolvedRoot)
	if err != nil {
		return fail(fmt.Errorf("stat resolved workspace root: %w", err))
	}
	if !os.SameFile(openedRoot, currentRoot) {
		return fail(fmt.Errorf("workspace root changed while file was being opened"))
	}
	resolvedTarget, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fail(fmt.Errorf("resolve workspace file: %w", err))
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		return fail(fmt.Errorf("file resolves outside workspace root"))
	}
	current, err := os.Stat(resolvedTarget)
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(opened, current) {
		return fail(fmt.Errorf("workspace path changed while it was being opened"))
	}
	return file, nil
}
