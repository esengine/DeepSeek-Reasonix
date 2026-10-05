package serve

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// sessions lists saved session files from the session directory, enriched with
// LLM-generated titles and turn counts.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	dir := s.ctl().SessionDir()
	if dir == "" {
		writeJSON(w, []any{})
		return
	}
	type sessionEntry struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Title   string `json:"title,omitempty"`
		Turns   int    `json:"turns,omitempty"`
		Current bool   `json:"current,omitempty"`
		// Modified is when the conversation last changed, for a picker to date it.
		Modified time.Time `json:"modified"`
	}
	// ListSessions answers from the sidecars and never decodes a transcript.
	// Counting turns per file here made this endpoint O(sessions x transcript
	// size) on every refresh — the sidebar load the sidecars exist to avoid.
	listed, err := sessionstore.ListSessions(dir)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	current := sessionstore.CanonicalSessionPath(s.ctl().SessionPath())
	out := make([]sessionEntry, 0, len(listed))
	for _, si := range listed {
		base := filepath.Base(si.Path)
		// A subagent transcript only means anything through the session that
		// spawned it, and IsVisibleSession deliberately keeps them visible so
		// GC and redaction still see them. Only flat legacy names reach here.
		if store.IsSubagentTranscriptName(base) {
			continue
		}
		if hiddenRecoveryCopy(si, current) {
			continue
		}
		modified := sessionstore.SessionContentModTime(si.Path)
		// A name the person typed outranks the generated one.
		title := strings.TrimSpace(si.CustomTitle)
		if title == "" {
			title = s.sessionTitle(base, si.Preview, modified.UnixNano())
		}
		out = append(out, sessionEntry{
			Name:     strings.TrimSuffix(base, ".jsonl"),
			Path:     si.Path,
			Turns:    si.Turns,
			Title:    title,
			Current:  sessionstore.CanonicalSessionPath(si.Path) == current,
			Modified: modified,
		})
	}
	writeJSON(w, out)
}
