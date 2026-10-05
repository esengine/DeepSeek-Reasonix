package configbackup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
)

// Named roots a file item is relative to. The snapshot never carries this
// machine's absolute location for them.
const (
	memoryRootDocs  = "docs"
	memoryRootFacts = "facts"
)

// ErrUnsafePath is a file item whose path would land outside its root.
var ErrUnsafePath = errors.New("configbackup: file path escapes its root")

func skillsRoot() string { return filepath.Join(config.ReasonixHomeDir(), skill.SkillsDirname) }

func memoryDocsRoot() string { return config.MemoryUserDir() }

func memoryFactsRoot() string {
	if dir := config.MemoryUserDir(); dir != "" {
		return filepath.Join(dir, "memory", "global")
	}
	return ""
}

func memoryRoot(name string) string {
	switch name {
	case memoryRootDocs:
		return memoryDocsRoot()
	case memoryRootFacts:
		return memoryFactsRoot()
	}
	return ""
}

// validSegment admits one path segment a restore may create. Colons and
// backslashes are refused on every platform: on Windows they name a drive, a
// stream or a separator, and a backup written on Linux may be restored there.
func validSegment(s string) bool {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\:`+"\x00") {
		return false
	}
	return strings.TrimRight(s, ". ") == s
}

// safeJoin resolves a slash-separated relative path under root and refuses
// anything that would leave it, including through a symlink already on disk.
func safeJoin(root, rel string) (string, error) {
	if root == "" || rel == "" || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}
	segments := strings.Split(rel, "/")
	if !slices.ContainsFunc(segments, func(s string) bool { return !validSegment(s) }) {
		cur := root
		for i, seg := range segments {
			cur = filepath.Join(cur, seg)
			info, err := os.Lstat(cur)
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			if err != nil {
				return "", err
			}
			if info.Mode()&fs.ModeSymlink != 0 || (i < len(segments)-1 && !info.IsDir()) {
				return "", fmt.Errorf("%w: %q crosses a link or a file", ErrUnsafePath, rel)
			}
		}
		return filepath.Join(append([]string{root}, segments...)...), nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
}

// PathRef is an absolute path an item names, and whether it exists here.
type PathRef struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// commandPaths lists the absolute paths among a shell command's static words.
// A command the parser cannot read statically reports nothing: this is a hint
// shown next to the command, never the gate, and the command itself is shown.
func commandPaths(command string) []PathRef {
	leaves, ok := shellparse.CompoundLeafCommands(command)
	if !ok {
		return nil
	}
	var words []string
	for _, argv := range leaves {
		words = append(words, argv...)
	}
	return absPaths(words...)
}

// absPaths reports the words that are absolute paths on either family of
// platform, since a backup made on one may be restored on the other.
func absPaths(words ...string) []PathRef {
	var out []PathRef
	seen := map[string]bool{}
	for _, w := range words {
		if seen[w] || !isAbsAnywhere(w) {
			continue
		}
		seen[w] = true
		_, err := os.Stat(w)
		out = append(out, PathRef{Path: w, Exists: err == nil})
	}
	return out
}

func isAbsAnywhere(p string) bool {
	if strings.HasPrefix(p, "/") && len(p) > 1 && !strings.HasPrefix(p, "//") {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z'))
}
