package serve

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// removeSessions deletes several conversations in one request. It reuses the
// single-delete safety: every path is validated and guarded before any file is
// erased, a running conversation refuses the whole batch, and a recovery copy
// covered by a parent is swept up with it.
func (h *Hub) removeSessions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || len(body.Paths) == 0 {
		badBody(w)
		return
	}

	// Validate and resolve every requested path before touching anything, so a
	// malformed list never begins a delete that then aborts halfway.
	type picked struct {
		dir  string
		path string
	}
	leads := make([]picked, 0, len(body.Paths))
	seen := make(map[string]bool, len(body.Paths))
	open := h.openSessions()
	excluded := make([]string, 0, len(open))
	for p := range open {
		excluded = append(excluded, p)
	}
	for _, raw := range body.Paths {
		path, err := filepath.Abs(strings.TrimSpace(raw))
		if err != nil || !store.IsSessionTranscriptName(filepath.Base(path)) {
			refuse(w, http.StatusBadRequest, codeSessionBadPath, "a session path could not be resolved", nil)
			return
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		if id := open[sessionstore.CanonicalSessionPath(path)]; id != "" &&
			!h.releaseOrRefuse(w, r, "session.running", "a conversation is running; stop it first",
				h.panesWhere(func(rt *Runtime) bool { return rt.ID == id })) {
			return
		}
		dir := filepath.Dir(path)
		if !h.ownsSessionDir(dir) {
			refuse(w, http.StatusForbidden, "session.outside_workspace", "path outside a known workspace", nil)
			return
		}
		leads = append(leads, picked{dir, path})
	}

	// Expand each lead to include recovery copies it covers, deduplicated across
	// the whole batch, then guard every transcript before erasing any.
	erase := make(map[string]string) // path -> dir
	for _, lead := range leads {
		paths := []string{lead.path}
		sibs, err := sessionstore.RecoveryLineagePaths(lead.path, excluded...)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		for _, p := range sibs {
			if p != lead.path && sessionstore.RecoveryBranchCoveredByParent(p, lead.dir) {
				paths = append(paths, p)
			}
		}
		for _, p := range paths {
			erase[p] = lead.dir
		}
	}
	ordered := make([]picked, 0, len(erase))
	for path, dir := range erase {
		ordered = append(ordered, picked{dir, path})
	}

	guards := make([]*sessionstore.SessionRemovalGuard, 0, len(ordered))
	for _, tgt := range ordered {
		guard, ok := acquireRemovalGuard(w, tgt.path)
		if !ok {
			for _, g := range guards {
				g.Release()
			}
			return
		}
		guards = append(guards, guard)
	}
	for i := range slices.Backward(ordered) {
		if !eraseGuardedSession(w, ordered[i].dir, ordered[i].path, guards[i]) {
			for _, g := range guards[:i] {
				g.Release()
			}
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
