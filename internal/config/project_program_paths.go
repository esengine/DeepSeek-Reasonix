package config

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/sandbox"
)

// newSingleProgram digests a single program path with the binary's content, so
// a program swapped at the same path needs approval again at the next load.
func newSingleProgram(kind ProjectProgramKind, key, path, ws string) ProjectProgram {
	p := NewProjectProgram(kind, key, path, ws, ws, path, nil)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && filepath.IsAbs(path) {
		p.Files = []string{filepath.Clean(path)}
		p.Digest = p.currentDigest()
	}
	return p
}

// namesWritablePath reports a program path that is relative, climbs with "..",
// or lies — existing or not — in the workspace, an allow_write root, or a
// temporary or toolchain cache directory the bash jail leaves writable. A bare
// command name is looked up on PATH, which never searches the workspace.
func (c *Config) namesWritablePath(ws, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || !strings.ContainsAny(path, `/\`) {
		return false
	}
	if !filepath.IsAbs(path) || strings.Contains(path, "..") || ws == "" {
		return true
	}
	return inRoots(path, c.writableRoots(ws))
}

// inRoots reports path at or under any of roots, by spelling or by identity.
func inRoots(path string, roots []string) bool {
	resolved, err := evalSymlinksAllowMissing(path)
	if err != nil {
		return true
	}
	for _, root := range roots {
		if real, err := evalSymlinksAllowMissing(root); err == nil {
			root = real
		}
		if pathWithinRoot(root, resolved) || sameFileAncestor(root, resolved) {
			return true
		}
	}
	return false
}

func hostWritableDirs() []string { return sandbox.HostWritableDirs() }

func (c *Config) writableRoots(ws string) []string {
	roots := append([]string{ws, os.TempDir()}, hostWritableDirs()...)
	for _, dir := range c.Sandbox.AllowWrite {
		if dir = ExpandVars(dir); dir != "" {
			roots = append(roots, dir)
		}
	}
	return roots
}

// sameFileAncestor asks the filesystem, not the spelling, whether root is path
// or one of its ancestors: a case-insensitive volume or a junction spells the
// same directory differently.
func sameFileAncestor(root, path string) bool {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false
	}
	for dir := path; ; {
		if info, err := os.Stat(dir); err == nil && os.SameFile(rootInfo, info) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
