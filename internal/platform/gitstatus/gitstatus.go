// Package gitstatus reports what a working tree currently differs by, asking
// git rather than inferring it from what the agent did. The difference is the
// point: a file the agent created and a shell command then removed leaves two
// tool events behind and no change on disk, and only the tree can say so.
package gitstatus

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// Change is one path git reports as differing from HEAD. Status is the
// porcelain XY code with surrounding space trimmed: "M", "A", "D", "R", "??".
type Change struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"`
	// How much the file differs by. Nil is "git did not say" — a binary file,
	// or one nothing counted — and it stays nil rather than becoming a zero,
	// which reads as "changed by nothing" and is a different fact.
	Insertions *int `json:"insertions,omitempty"`
	Deletions  *int `json:"deletions,omitempty"`
}

// Deleted reports whether the path is gone from the tree.
func (c Change) Deleted() bool { return strings.Contains(c.Status, "D") }

// Added reports whether the path is new — staged or still untracked.
func (c Change) Added() bool { return strings.Contains(c.Status, "A") || c.Status == "??" }

// Status lists the changes under repo.Dir, the workspace repo was resolved for
// when it opened. An unresolved repo — a workspace that is not version
// controlled — reports ok=false rather than an error, so a caller falls back.
func Status(ctx context.Context, repo gitcmd.Repo) (changes []Change, ok bool, err error) {
	root := repo.Dir
	if !repo.Valid() || strings.TrimSpace(root) == "" {
		return nil, false, nil
	}
	// Porcelain paths stay repository-relative even when run from a
	// subdirectory, and Windows spells one directory as both an 8.3 and a long
	// path — so take the prefix from git rather than from filepath.Rel.
	prefixRaw, err := repo.Command(ctx, "rev-parse", "--show-prefix").Output()
	if err != nil {
		return nil, false, err
	}
	prefix := strings.TrimSpace(string(prefixRaw))
	raw, err := repo.Command(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".").Output()
	if err != nil {
		return nil, false, err
	}
	out := make([]Change, 0, 16)
	for _, c := range ParsePorcelainZ(raw) {
		c.Path = relFromPrefix(root, prefix, c.Path)
		if c.Path == "" {
			continue
		}
		c.OldPath = relFromPrefix(root, prefix, c.OldPath)
		out = append(out, c)
	}
	countLines(ctx, repo, out)
	return out, true, nil
}

// countLines fills in how much each path differs by. Tracked paths come from
// one numstat; git spells a binary file "-", which stays uncounted. Untracked
// files have nothing to diff against, so their whole length is the addition.
func countLines(ctx context.Context, repo gitcmd.Repo, changes []Change) {
	root := repo.Dir
	raw, err := repo.Command(ctx, "diff", "--numstat", "-z", "HEAD", "--", ".").Output()
	if err == nil {
		byPath := ParseNumstatZ(raw)
		for i := range changes {
			if n, ok := byPath[changes[i].Path]; ok {
				changes[i].Insertions, changes[i].Deletions = n.added, n.removed
			}
		}
	}
	for i := range changes {
		if changes[i].Insertions != nil || changes[i].Deletions != nil || changes[i].Status != "??" {
			continue
		}
		if n, ok := newFileLines(filepath.Join(root, filepath.FromSlash(changes[i].Path))); ok {
			zero := 0
			changes[i].Insertions, changes[i].Deletions = &n, &zero
		}
	}
}

// numstat is one path's counts, either of which git may decline to give.
type numstat struct{ added, removed *int }

// ParseNumstatZ decodes `git diff --numstat -z`. Added and removed come first,
// then the path; a rename spends two more fields on old and new.
func ParseNumstatZ(raw []byte) map[string]numstat {
	out := map[string]numstat{}
	for line := range bytes.SplitSeq(raw, []byte{0}) {
		fields := strings.SplitN(strings.TrimSpace(string(line)), "\t", 3)
		if len(fields) != 3 || fields[2] == "" {
			continue
		}
		out[fields[2]] = numstat{added: countOrNil(fields[0]), removed: countOrNil(fields[1])}
	}
	return out
}

// countOrNil reads a numstat column. "-" is git saying it did not count this
// one, which is not zero.
func countOrNil(field string) *int {
	n, err := strconv.Atoi(strings.TrimSpace(field))
	if err != nil {
		return nil
	}
	return &n
}

// newFileLines counts an untracked file's lines, up to a size past which the
// count is not worth the read and stays unsaid.
func newFileLines(path string) (int, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Size() > newFileCountLimit {
		return 0, false
	}
	body, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(body, 0) >= 0 {
		return 0, false
	}
	if len(body) == 0 {
		return 0, true
	}
	return bytes.Count(body, []byte{'\n'}) + 1, true
}

const newFileCountLimit = 2 << 20

// ParsePorcelainZ decodes `git status --porcelain=v1 -z`. Rename and copy
// entries spend a second NUL-separated field on the source path.
func ParsePorcelainZ(raw []byte) []Change {
	parts := bytes.Split(raw, []byte{0})
	out := make([]Change, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 4 {
			continue
		}
		status := string(part[:2])
		entry := Change{Path: string(part[3:]), Status: strings.TrimSpace(status)}
		if strings.ContainsAny(status, "RC") && i+1 < len(parts) {
			i++
			entry.OldPath = string(parts[i])
		}
		out = append(out, entry)
	}
	return out
}

// RelPath normalises a status path to a slash-separated path inside base, or ""
// when it escapes base.
func RelPath(base, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(base, path); err == nil {
			path = rel
		}
	}
	path = filepath.Clean(path)
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(path)
}

func relFromPrefix(base, prefix, path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	prefix = filepath.ToSlash(strings.TrimSpace(prefix))
	if path == "" {
		return ""
	}
	if prefix != "" {
		if !strings.HasPrefix(path, prefix) {
			return ""
		}
		path = strings.TrimPrefix(path, prefix)
	}
	return RelPath(base, filepath.FromSlash(path))
}

// MaxDiffBytes caps one path's diff. A generated file can differ by megabytes,
// and a panel holding all of it is not being read — the cap is reported on the
// answer rather than applied behind the reader's back.
const MaxDiffBytes = 512 << 10

// ErrPathOutsideTree rejects a path that does not name something inside the
// tree. It is a sentinel because the caller has to tell it apart from git
// failing: one is a bad request, the other is a broken repository.
var ErrPathOutsideTree = errors.New("gitstatus: path is outside the working tree")

// safeRel constrains a caller-supplied path to the tree. Three things are being
// kept out: an absolute path, a traversal, and a leading dash — git reads that
// last one as an option however the argument list is built, which is why every
// invocation below also puts it after "--".
func safeRel(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.HasPrefix(rel, "-") || filepath.IsAbs(rel) {
		return "", ErrPathOutsideTree
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	inside, err := filepath.Rel(root, filepath.Join(root, clean))
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideTree
	}
	return filepath.ToSlash(clean), nil
}

// Diff returns the unified diff for one path in root's working tree, measured
// against HEAD so a staged change is included with an unstaged one. Untracked
// files are diffed against the null device instead: they have nothing in HEAD,
// and that is the only way git prints them without first writing to the index.
func Diff(ctx context.Context, repo gitcmd.Repo, path string) (text string, truncated bool, err error) {
	rel, err := safeRel(repo.Dir, path)
	if err != nil {
		return "", false, err
	}
	var raw []byte
	if tracked(ctx, repo, rel) {
		raw, err = repo.Command(ctx, "diff", "--no-color", "HEAD", "--", rel).Output()
		if err != nil {
			// A repository with no commits has no HEAD to name; everything in
			// it is either staged or untracked.
			raw, err = repo.Command(ctx, "diff", "--no-color", "--", rel).Output()
			if err != nil {
				return "", false, err
			}
		}
	} else {
		// --no-index exits 1 when the two sides differ, which is the whole
		// point of asking. Only the output matters here.
		raw, _ = repo.Command(ctx, "diff", "--no-color", "--no-index", "--", os.DevNull, rel).Output()
	}
	if len(raw) > MaxDiffBytes {
		return string(raw[:MaxDiffBytes]), true, nil
	}
	return string(raw), false, nil
}

// tracked reports whether git already knows the path. The answer decides which
// of the two diffs above can say anything at all, so it is asked rather than
// inferred from an empty result — an unchanged tracked file and an untracked
// one both diff to nothing against HEAD.
func tracked(ctx context.Context, repo gitcmd.Repo, rel string) bool {
	return repo.Command(ctx, "ls-files", "--error-unmatch", "--", rel).Run() == nil
}
