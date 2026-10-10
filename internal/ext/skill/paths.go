package skill

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"

	"reasonix/internal/base/fileutil"
)

// ErrPathOutsideWorkspace marks an observed path whose real location is not
// under the workspace root, so it can neither activate a skill nor be recorded.
var ErrPathOutsideWorkspace = errors.New("path is outside the workspace")

// PathGated reports whether the skill declared `paths:`. A declaration whose
// every pattern was rejected still gates, so a typo hides the skill rather than
// freeing it.
func (s Skill) PathGated() bool { return len(s.Paths)+len(s.InvalidPaths) > 0 }

// parsePathsFrontmatter splits a `paths:` value into usable globs and the
// rejected ones, which doctor reports. Commas inside braces stay in one glob.
func parsePathsFrontmatter(raw string) (valid, invalid []string) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = raw[1 : len(raw)-1]
	}
	seen := map[string]bool{}
	budget := maxPathExpansions
	for _, item := range splitOutsideBraces(raw) {
		item = strings.Trim(strings.TrimSpace(item), `"'`)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		n := 0
		if _, ok := effectivePathPattern(item); ok {
			n = braceExpansions(item)
		}
		if n > 0 && n <= budget {
			budget -= n
			valid = append(valid, item)
		} else {
			invalid = append(invalid, item)
		}
	}
	return valid, invalid
}

func splitOutsideBraces(raw string) []string {
	var out []string
	depth, start := 0, 0
	for i := range len(raw) {
		switch raw[i] {
		case '{':
			depth++
		case '}':
			depth = max(depth-1, 0)
		case ',':
			if depth == 0 {
				out = append(out, raw[start:i])
				start = i + 1
			}
		}
	}
	return append(out, raw[start:])
}

// Bounds on one `paths:` declaration: each brace group multiplies the patterns
// a glob stands for, so the whole list shares one expansion budget.
const (
	maxPathExpansions = 1000
	maxPathGlobLen    = 512
)

// effectivePathPattern is the workspace-relative doublestar pattern a declared
// glob stands for. It is anchored at the workspace root, so a glob with no
// slash names root files only; a trailing slash names everything under that
// directory. Negation, parent traversal and over-budget braces are refused.
func effectivePathPattern(p string) (string, bool) {
	p = strings.TrimPrefix(strings.TrimPrefix(p, "./"), "/")
	if p == "" || len(p) > maxPathGlobLen || strings.HasPrefix(p, "!") || !doublestar.ValidatePattern(p) {
		return "", false
	}
	if slices.Contains(strings.Split(p, "/"), "..") || braceExpansions(p) > maxPathExpansions {
		return "", false
	}
	if strings.HasSuffix(p, "/") {
		p += "**"
	}
	return p, true
}

// braceExpansions is how many plain patterns a glob's brace groups expand to,
// saturating just above maxPathExpansions so a hostile glob costs a bounded
// count. A glob without braces is one pattern.
func braceExpansions(p string) int {
	n, _ := countBraces(p, 0, true)
	return n
}

func countBraces(p string, i int, top bool) (n, next int) {
	total := 1
	for i < len(p) {
		switch p[i] {
		case '\\':
			i++
		case '{':
			var inner int
			inner, i = countGroup(p, i+1)
			total = min(total*inner, maxPathExpansions+1)
		case ',', '}':
			if !top {
				return total, i
			}
		}
		i++
	}
	return total, i
}

func countGroup(p string, i int) (n, next int) {
	for i < len(p) {
		var alt int
		alt, i = countBraces(p, i, false)
		n = min(n+alt, maxPathExpansions+1)
		if i >= len(p) || p[i] == '}' {
			return max(n, 1), i
		}
		i++
	}
	return max(n, 1), i
}

// PathHits is the set of workspace files the host has seen the session touch,
// accumulated over the session. Eligibility is a function of this set and a
// skill's globs alone; verdicts are memoised against the set and dropped the
// moment it changes, so a verdict never outlives the state it answers for.
type PathHits struct {
	mu       sync.Mutex
	root     string
	realRoot string
	seen     map[string]struct{}
	verdicts verdictCache
}

type verdictCache struct {
	gen   uint64
	byKey map[string]bool
}

// NewPathHits records paths relative to workspaceRoot.
func NewPathHits(workspaceRoot string) *PathHits {
	root := filepath.Clean(workspaceRoot)
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		real = root
	}
	return &PathHits{root: root, realRoot: real, seen: map[string]struct{}{}, verdicts: verdictCache{byKey: map[string]bool{}}}
}

// Observe records a path the host saw a tool read or write. A relative path is
// taken against the workspace root. A path whose real location, after
// resolving symlinks, leaves the workspace is refused with
// ErrPathOutsideWorkspace; the name recorded is the one it was reached by.
func (h *PathHits) Observe(p string) error {
	if strings.TrimSpace(p) == "" {
		return nil
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(h.root, abs)
	}
	abs = filepath.Clean(abs)
	if fileutil.NetworkScope(abs, []string{h.root, h.realRoot}) != nil {
		return ErrPathOutsideWorkspace
	}
	resolved := lookupExisting(abs)
	realRel, ok := relUnder(h.realRoot, resolved)
	if !ok {
		return ErrPathOutsideWorkspace
	}
	rel, ok := relUnder(h.root, abs)
	if !ok {
		rel = realRel
	}
	if rel == "." {
		return nil
	}
	if info, err := os.Stat(resolved); err == nil && info.IsDir() {
		return nil
	}
	name := slashFrom(rel, os.PathSeparator)
	h.mu.Lock()
	if _, known := h.seen[name]; !known {
		h.seen[name] = struct{}{}
		h.verdicts.invalidate()
	}
	h.mu.Unlock()
	return nil
}

// Reset forgets every recorded path.
func (h *PathHits) Reset() {
	h.mu.Lock()
	h.seen = map[string]struct{}{}
	h.verdicts.invalidate()
	h.mu.Unlock()
}

// Seen returns the recorded paths, sorted.
func (h *PathHits) Seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sortedSeenLocked()
}

// Eligible reports whether the skill may be listed: it declared no `paths`, or
// a recorded path matches one of its globs. A nil set records nothing, so it
// leaves every skill listed.
func (h *PathHits) Eligible(sk Skill) bool {
	if h == nil || !sk.PathGated() {
		return true
	}
	key := strings.Join(sk.Paths, "\x00")
	h.mu.Lock()
	if v, ok := h.verdicts.byKey[key]; ok {
		h.mu.Unlock()
		return v
	}
	gen := h.verdicts.gen
	seen := h.sortedSeenLocked()
	h.mu.Unlock()

	v := matchesAny(sk.Paths, seen)

	h.mu.Lock()
	if h.verdicts.gen == gen {
		h.verdicts.byKey[key] = v
	}
	h.mu.Unlock()
	return v
}

// Visible keeps the skills the model may be shown now: those that declared no
// `paths`, and those whose globs a recorded path matches.
func (h *PathHits) Visible(skills []Skill) []Skill {
	if h == nil {
		return skills
	}
	out := make([]Skill, 0, len(skills))
	for _, sk := range skills {
		if h.Eligible(sk) {
			out = append(out, sk)
		}
	}
	return out
}

func (v *verdictCache) invalidate() {
	v.gen++
	v.byKey = map[string]bool{}
}

func (h *PathHits) sortedSeenLocked() []string {
	out := make([]string, 0, len(h.seen))
	for p := range h.seen {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func matchesAny(globs, paths []string) bool {
	for _, glob := range globs {
		pattern, ok := effectivePathPattern(glob)
		if !ok {
			continue
		}
		for _, p := range paths {
			if fileutil.MatchSlashGlob(p, pattern) {
				return true
			}
		}
	}
	return false
}

func relUnder(root, abs string) (string, bool) {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// lookupExisting is the filesystem lookup Observe makes; tests count its calls.
var lookupExisting = resolveExisting

// resolveExisting resolves symlinks in the longest existing prefix of p, so a
// file about to be created is judged by where its directory really is. A link
// whose target is missing is followed to where it would land.
func resolveExisting(p string) string {
	for range maxLinkHops {
		var tail []string
		cur := p
		for {
			if real, err := filepath.EvalSymlinks(cur); err == nil {
				return filepath.Join(append([]string{real}, tail...)...)
			}
			if target, err := os.Readlink(cur); err == nil {
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(cur), target)
				}
				p = filepath.Join(append([]string{target}, tail...)...)
				break
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				return p
			}
			tail = append([]string{filepath.Base(cur)}, tail...)
			cur = parent
		}
	}
	return ""
}

const maxLinkHops = 40

func slashFrom(rel string, sep byte) string {
	if sep == '/' {
		return rel
	}
	return strings.ReplaceAll(rel, string(sep), "/")
}
