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
	MtimeMilli int64  `json:"mtimeMilli"`
}

// sessions lists saved sessions with event-log-aware titles and turn counts.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	dir := s.ctl().SessionDir()
	if dir == "" {
		writeJSON(w, []any{})
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	current := filepath.Clean(s.ctl().SessionPath())
	out := make([]sessionListEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if agent.IsCleanupPending(path) {
			continue
		}
		mtime := agent.SessionContentModTime(path)
		row := sessionListEntry{Name: strings.TrimSuffix(entry.Name(), ".jsonl"), Path: path, Current: filepath.Clean(path) == current, MtimeMilli: mtime.UnixMilli()}
		if first, turns := agent.SessionPreview(path); turns > 0 {
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
