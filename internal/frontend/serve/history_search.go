package serve

import (
	"net/http"
	"strconv"
	"strings"

	"reasonix/internal/state/history"
)

// historySearchHit is one conversation whose words matched the query.
type historySearchHit struct {
	Path         string  `json:"path"`
	ID           string  `json:"id"`
	Title        string  `json:"title,omitempty"`
	Workspace    string  `json:"workspace,omitempty"`
	Snippet      string  `json:"snippet"`
	Role         string  `json:"role"`
	MessageIndex int     `json:"messageIndex"`
	Score        float64 `json:"score"`
}

// historySearchIndex is what the projection could answer for, in the counters
// the catalog already keeps. A source it could not read is left out of the
// hits; without this beside them, that search and a search that genuinely
// matched nothing would reach the rail as the same answer.
type historySearchIndex struct {
	State   string `json:"state"`
	Indexed int64  `json:"indexed"`
	Total   int64  `json:"total"`
	Failed  int64  `json:"failed"`
	Partial bool   `json:"partial"`
	Reason  string `json:"reason,omitempty"`
}

type historySearchResponse struct {
	Hits  []historySearchHit `json:"hits"`
	Index historySearchIndex `json:"index"`
}

// historySearch answers across every workspace this process has indexed. It is
// a hub route: the rail asks it with no session open.
func historySearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		refuse(w, http.StatusBadRequest, "history.query_required", "a search needs words to look for", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := history.SearchSessions(r.Context(), query, limit)
	if err != nil {
		refuse(w, http.StatusInternalServerError, "history.search_failed", "the conversation index could not answer", nil)
		return
	}
	out := make([]historySearchHit, 0, len(found))
	for _, h := range found {
		out = append(out, historySearchHit{
			Path: h.Path, ID: h.ID, Title: h.Title, Workspace: h.Workspace, Snippet: h.Snippet,
			Role: string(h.Role), MessageIndex: h.MessageIndex, Score: h.Score,
		})
	}
	// Read the projection's state after the search: a transcript the query
	// pulled up but could not be read has just been recorded there.
	status := history.SearchIndexStatus()
	partial, reason := history.SearchIndexProgress(status)
	writeJSON(w, historySearchResponse{
		Hits: out,
		Index: historySearchIndex{
			State: status.State, Indexed: status.Indexed, Total: status.Total,
			Failed: status.Failed, Partial: partial, Reason: reason,
		},
	})
}
