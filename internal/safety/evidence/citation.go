package evidence

import (
	"context"
	"path/filepath"
	"strings"
)

type workspaceRootKey struct{}

// WithWorkspaceRoot attaches the workspace a call's relative paths resolve
// against, so a cited path can be matched to the receipt for the same file.
func WithWorkspaceRoot(ctx context.Context, root string) context.Context {
	return context.WithValue(ctx, workspaceRootKey{}, strings.TrimSpace(root))
}

// WorkspaceRootFromContext returns the root WithWorkspaceRoot attached, or "".
func WorkspaceRootFromContext(ctx context.Context) string {
	root, _ := ctx.Value(workspaceRootKey{}).(string)
	return root
}

// CitationForms returns the ledger identities of the one file a citation
// names. File tools resolve a relative path against the workspace, so a
// relative citation and the absolute path under root are the same file; a
// path outside root has only its own form.
func CitationForms(root, cited string) []string {
	own := normalizePath(cited)
	if own == "" {
		return nil
	}
	forms := []string{own}
	if root == "" {
		return forms
	}
	cited = filepath.Clean(filepath.FromSlash(strings.ReplaceAll(strings.TrimSpace(cited), `\`, `/`)))
	root = filepath.Clean(root)
	if !filepath.IsAbs(cited) {
		if inside(cited) {
			forms = append(forms, normalizePath(filepath.Join(root, cited)))
		}
		return forms
	}
	if rel, err := filepath.Rel(root, cited); err == nil && inside(rel) {
		forms = append(forms, normalizePath(rel))
	}
	return forms
}

// inside reports whether a relative path stays below the directory it is
// relative to.
func inside(rel string) bool {
	return rel != "." && rel != ".." && !filepath.IsAbs(rel) &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
