package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/nilutil"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func (h *Hub) titleRequest(w http.ResponseWriter, r *http.Request) (string, *string, bool) {
	var body struct {
		Path  string  `json:"path"`
		Title *string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return "", nil, false
	}
	requested, err := filepath.Abs(strings.TrimSpace(body.Path))
	name := filepath.Base(requested)
	if err != nil || !filepath.IsLocal(name) || !store.IsSessionTranscriptName(name) {
		refuse(w, http.StatusBadRequest, codeSessionBadPath, "the session path could not be resolved", nil)
		return "", nil, false
	}
	for _, root := range h.roots() {
		dir, err := filepath.Abs(SessionDirFor(root.dir))
		if err != nil || !strings.HasPrefix(requested, dir+string(os.PathSeparator)) || filepath.Dir(requested) != dir {
			continue
		}
		// The request selects a host-owned directory; it is not an I/O path.
		path := filepath.Join(dir, name)
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return "", nil, false
		}
		physicalDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return "", nil, false
		}
		if !strings.HasPrefix(resolved, physicalDir+string(os.PathSeparator)) || filepath.Dir(resolved) != physicalDir {
			refuse(w, http.StatusForbidden, "session.outside_workspace", "path outside a known workspace", nil)
			return "", nil, false
		}
		return path, body.Title, true
	}
	refuse(w, http.StatusForbidden, "session.outside_workspace", "path outside a known workspace", nil)
	return "", nil, false
}

func (h *Hub) renameSession(w http.ResponseWriter, r *http.Request) {
	path, input, ok := h.titleRequest(w, r)
	if !ok {
		return
	}
	if input == nil {
		badBody(w)
		return
	}
	title := strings.TrimSpace(*input)
	if title == "" {
		cache := h.titleCacheFor(filepath.Dir(path))
		if err := cache.clearExplicit(filepath.Base(path)); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		h.resumeAutomaticTitle(path, cache)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.titleCacheFor(filepath.Dir(path)).putExplicit(filepath.Base(path), title); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) autoNameSession(w http.ResponseWriter, r *http.Request) {
	path, _, ok := h.titleRequest(w, r)
	if !ok {
		return
	}
	source, err := sessionstore.UserMessagesTitleSource(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if source == "" {
		refuse(w, http.StatusBadRequest, "session.title_no_message", "the session has no user message", nil)
		return
	}
	generator := h.sessionTitleGenerator(filepath.Dir(path))
	if nilutil.IsNil(generator.titleProv) {
		refuse(w, http.StatusServiceUnavailable, "session.title_unavailable", "no title model is configured", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), titleFillTimeout)
	defer cancel()
	title := generator.generateTitle(ctx, source)
	if title == "" {
		refuse(w, http.StatusBadGateway, "session.title_failed", "the model did not generate a title", nil)
		return
	}
	if err := r.Context().Err(); err != nil {
		return
	}
	writeJSON(w, struct {
		Title string `json:"title"`
	}{Title: title})
}

func (h *Hub) sessionTitleGenerator(dir string) *Server {
	h.mu.RLock()
	for _, rt := range h.runtimes {
		if rt.Server != nil && !nilutil.IsNil(rt.Server.titleProv) && rt.Server.Controller().SessionDir() == dir {
			h.mu.RUnlock()
			return rt.Server
		}
	}
	h.mu.RUnlock()
	generator := &Server{}
	generator.initTitleProvider()
	return generator
}

func (h *Hub) resumeAutomaticTitle(path string, cache *titleCache) {
	first, _ := sessionstore.SessionPreview(path)
	source := titleSource(first)
	name := filepath.Base(path)
	mod := sessionstore.SessionContentModTime(path).UnixNano()
	if _, ok := cache.get(name, source, mod); ok || source == "" {
		return
	}
	generator := h.sessionTitleGenerator(filepath.Dir(path))
	worker := &Server{
		titleProv: generator.titleProv, titlePrice: generator.titlePrice,
		titleModelRef: generator.titleModelRef, titleUsageSink: generator.titleUsageSink,
		titles: cache, fill: newTitleFiller(),
	}
	worker.scheduleTitle(name, source, mod)
}
