package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/store"
)

type sessionListEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Title      string `json:"title,omitempty"`
	Turns      int    `json:"turns,omitempty"`
	Current    bool   `json:"current,omitempty"`
	Running    bool   `json:"running,omitempty"`
	MtimeMilli int64  `json:"mtimeMilli"`
	// HeldBy reports lease ownership for clients: "me" (this serve
	// runtime holds it), "other" (another runtime, e.g. the desktop app,
	// holds it — write attempts will be rejected), or "" (free).
	HeldBy string `json:"heldBy,omitempty"`
}

// sessions lists saved sessions with event-log-aware titles and turn counts.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	ctrl := s.ctl()
	dir := ctrl.SessionDir()
	if dir == "" {
		writeJSON(w, []any{})
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	current := agent.CanonicalSessionPath(ctrl.SessionPath())
	running := map[string]bool{}
	s.detachedMu.Lock()
	for path, detached := range s.detached {
		running[filepath.Clean(path)] = controllerHasActiveRuntimeWork(detached.ctrl)
	}
	s.detachedMu.Unlock()
	out := make([]sessionListEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) {
			continue
		}
		path := agent.CanonicalSessionPath(filepath.Join(dir, entry.Name()))
		if agent.IsCleanupPending(path) {
			continue
		}
		mtime := agent.SessionContentModTime(path)
		cleanPath := agent.CanonicalSessionPath(path)
		row := sessionListEntry{Name: strings.TrimSuffix(entry.Name(), ".jsonl"), Path: path, Current: cleanPath == current, Running: running[cleanPath], MtimeMilli: mtime.UnixMilli()}
		if row.Current {
			row.Running = controllerHasActiveRuntimeWork(ctrl)
		}
		if agent.SessionLeaseHeldByCurrentRuntime(path) {
			row.HeldBy = "me"
		} else if agent.SessionLeaseHeldByOtherRuntime(path) {
			row.HeldBy = "other"
		}
		first, turns, cached := agent.SessionPreviewCached(path)
		if !cached {
			first, turns = agent.SessionPreview(path)
		}
		if turns > 0 {
			row.Turns = turns
			row.Title = s.sessionTitle(r.Context(), entry.Name(), first, mtime.UnixNano())
		}
		out = append(out, row)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	writeJSON(w, out)
}
