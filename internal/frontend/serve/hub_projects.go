package serve

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/sessionstore"
)

type workspaceProject struct {
	root       string
	roots      []string
	remembered bool
}

func projectRoot(root string) string {
	return worktree.ProjectRoot(root, config.DeliveryWorktreeDir())
}

func projectIdentity(root string) string {
	return sessionstore.CanonicalSessionPath(projectRoot(root))
}

func groupWorkspaces(refs []rootRef) []workspaceProject {
	out := []workspaceProject{}
	index := map[string]int{}
	aliases := map[string]string{}
	sources := map[string]string{}
	for _, ref := range refs {
		source := projectRoot(ref.dir)
		sources[ref.dir] = source
		if source == ref.dir {
			aliases[sessionstore.CanonicalSessionPath(source)] = ref.dir
		}
	}
	for _, ref := range refs {
		source := sources[ref.dir]
		root := source
		identity := sessionstore.CanonicalSessionPath(source)
		if alias := aliases[identity]; alias != "" {
			root = alias
		}
		at, ok := index[identity]
		if !ok {
			at = len(out)
			index[identity] = at
			out = append(out, workspaceProject{root: root, roots: []string{root}})
		}
		p := &out[at]
		p.remembered = p.remembered || ref.remembered
		for _, dir := range []string{source, ref.dir} {
			if !slices.Contains(p.roots, dir) {
				p.roots = append(p.roots, dir)
			}
		}
	}
	for i := range out {
		if !projectDirectoryExists(out[i].root) {
			for _, root := range out[i].roots {
				if projectDirectoryExists(root) {
					out[i].root = root
					break
				}
			}
		}
	}
	return out
}

func projectDirectoryExists(root string) bool {
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}

func rememberedProjects(paths []string) []workspaceProject {
	refs := make([]rootRef, 0, len(paths))
	for _, path := range paths {
		refs = append(refs, rootRef{dir: path, remembered: true})
	}
	return groupWorkspaces(refs)
}

func workspaceProjectOrder(paths []string) []string {
	out := []string{}
	for _, p := range rememberedProjects(paths) {
		out = append(out, projectIdentity(p.root))
	}
	return out
}

func hasWorkspaceProject(paths []string, dir string) bool {
	return slices.Contains(workspaceProjectOrder(paths), projectIdentity(dir))
}

func orderWorkspaceMembers(paths, order []string) []string {
	members := map[string][]string{}
	for _, path := range paths {
		root := projectIdentity(path)
		members[root] = append(members[root], path)
	}
	out := make([]string, 0, len(paths))
	for _, root := range order {
		out = append(out, members[root]...)
	}
	return out
}

func (h *Hub) sessionRoots() []rootRef {
	out := h.roots()
	for _, p := range groupWorkspaces(out) {
		for _, dir := range p.roots {
			if !slices.ContainsFunc(out, func(ref rootRef) bool { return ref.dir == dir }) {
				out = append(out, rootRef{dir: dir, remembered: p.remembered})
			}
		}
	}
	return out
}

func (h *Hub) listedSessionRoot(path, root string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	dir := sessionstore.CanonicalSessionPath(filepath.Dir(path))
	for _, ref := range h.sessionRoots() {
		if dir == sessionstore.CanonicalSessionPath(SessionDirFor(ref.dir)) &&
			(strings.TrimSpace(root) == "" || projectIdentity(root) == projectIdentity(ref.dir)) {
			return ref.dir
		}
	}
	return ""
}

// resolveRoot decides which folder a new runtime opens against: the one asked
// for, the one that owns the session being opened, or the first runtime's.
func (h *Hub) resolveRoot(req OpenRequest) (string, error) {
	if root := h.listedSessionRoot(req.SessionPath, req.Root); root != "" {
		return resolveWorkspaceDir(root)
	}
	if root := strings.TrimSpace(req.Root); root != "" {
		return resolveWorkspaceDir(root)
	}
	if path := strings.TrimSpace(req.SessionPath); path != "" {
		if root := workspaceRootForSession(path); root != "" {
			return root, nil
		}
	}
	h.mu.RLock()
	first := ""
	for _, id := range h.order {
		if rt := h.runtimes[id]; rt != nil && rt.remote == nil && h.shown(rt) {
			first = rt.Server.Controller().WorkspaceRoot()
			break
		}
	}
	h.mu.RUnlock()
	if first != "" {
		return first, nil
	}
	for _, dir := range LaunchWorkspaces() {
		if projectDirectoryExists(dir) {
			return dir, nil
		}
	}
	if dir := ensureAutoWorkspace(); dir != "" {
		return dir, nil
	}
	return "", refusal(http.StatusConflict, "workspace.none", errors.New("no workspace to open in — add a folder first"), nil)
}
