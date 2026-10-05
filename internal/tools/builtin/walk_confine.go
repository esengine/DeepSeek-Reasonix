package builtin

import (
	"io/fs"
	"path/filepath"
	"strings"

	"reasonix/internal/base/secrets"
)

// walkConfine answers confineRead for the entries one filepath.WalkDir reaches.
// WalkDir descends only into plain directories, so a plain file or directory
// below the root resolves to the root's real path joined with its relative
// path; only a symlink, junction or other special entry pays a full resolve.
type walkConfine struct {
	forbidRoots []string
	scope       []string
	root        string
	realRoot    string // empty when the root did not resolve: every entry pays
	protect     bool
	active      bool
}

func newWalkConfine(forbidRoots, scope []string, root string) walkConfine {
	w := walkConfine{forbidRoots: forbidRoots, scope: scope, root: root, protect: secrets.ProtectSensitiveFiles()}
	w.active = len(forbidRoots) > 0 || len(scope) > 0 || w.protect || secrets.ProtectCredentialFiles()
	if w.active {
		if real, err := realPath(root); err == nil {
			w.realRoot = real
		}
	}
	return w
}

// blocked reports whether the walked entry at path is confined. d is the entry
// WalkDir handed over for path.
func (w walkConfine) blocked(path string, d fs.DirEntry) bool {
	if !w.active {
		return false
	}
	if w.realRoot == "" || d == nil || d.Type()&^fs.ModeDir != 0 {
		return readOutsideScope(w.scope, path) || confineRead(w.forbidRoots, path)
	}
	rel, err := filepath.Rel(w.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return readOutsideScope(w.scope, path) || confineRead(w.forbidRoots, path)
	}
	return confineResolved(w.forbidRoots, filepath.Join(w.realRoot, rel), w.protect)
}
