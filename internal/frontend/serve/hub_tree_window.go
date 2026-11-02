package serve

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// defaultTreeWindow is how many conversations of a workspace /tree builds when
// the request does not ask for more.
const defaultTreeWindow = 50

// treeWindow bounds each project's sidebar rows, or a global recent page.
// Counts cover the matching history, not only the rows returned.
type treeWindow struct {
	progressive   bool
	retryMetadata bool
	limit         int
	recent        bool
	offset        int
	hideLegacy    bool
	limits        map[string]int
	// scope counts every row, only active rows, or only archived rows.
	// Ordinary windows also include open and pinned active rows; recent
	// pages contain only their matching activity-ordered rows.
	scope treeScope
	// query keeps the rows whose title or name contains it, case-blind; a
	// workspace whose own name contains it keeps all of its rows.
	query string
}

type treeScope string

const (
	scopeEvery    treeScope = ""
	scopeActive   treeScope = "active"
	scopeArchived treeScope = "archived"
)

// maxTreeWindow is the most rows one workspace's window builds; a larger
// request is served at this size, and Remaining says what is left. 0 stays
// "every row" for callers that ask for the whole list.
const maxTreeWindow = 300

// maxTreeQuery bounds the search text the kernel will match against.
const maxTreeQuery = 200

// limitFor is the row bound for one workspace; 0 means every row.
func (w treeWindow) limitFor(root string) int {
	if w.recent {
		if w.limit == 0 {
			return 0
		}
		return w.offset + w.limit
	}
	if n, ok := w.limits[root]; ok {
		return n
	}
	return w.limit
}

// parseTreeWindow reads `limit` (every workspace's bound) and `limits` (a JSON
// object of per-workspace bounds); 0 means unbounded. It answers the request
// itself when a value is unreadable.
func parseTreeWindow(w http.ResponseWriter, r *http.Request) (treeWindow, bool) {
	window := treeWindow{limit: defaultTreeWindow}
	q := r.URL.Query()
	window.progressive = q.Get("progressive") == "1"
	window.retryMetadata = q.Get("retryMetadata") == "1"
	window.hideLegacy = q.Get("hideLegacy") == "1"
	switch q.Get("order") {
	case "":
	case "recent":
		window.recent = true
		window.progressive = false
	default:
		badValue(w, "order", "recent")
		return window, false
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > int(^uint(0)>>1)-maxTreeWindow || !window.recent {
			badValue(w, "offset", "a non-negative recent-page offset")
			return window, false
		}
		window.offset = n
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			badValue(w, "limit", "a non-negative integer")
			return window, false
		}
		window.limit = min(n, maxTreeWindow)
	}
	if raw := q.Get("limits"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &window.limits); err != nil {
			badValue(w, "limits", "a JSON object of workspace to non-negative integer")
			return window, false
		}
		for _, n := range window.limits {
			if n < 0 {
				badValue(w, "limits", "a JSON object of workspace to non-negative integer")
				return window, false
			}
		}
		for root, n := range window.limits {
			window.limits[root] = min(n, maxTreeWindow)
		}
	}
	switch scope := treeScope(q.Get("scope")); scope {
	case scopeEvery, scopeActive, scopeArchived:
		window.scope = scope
	default:
		badValue(w, "scope", "active or archived")
		return window, false
	}
	if raw := strings.TrimSpace(q.Get("q")); raw != "" {
		if len(raw) > maxTreeQuery {
			badValue(w, "q", "at most 200 bytes")
			return window, false
		}
		window.query = strings.ToLower(raw)
	}
	return window, true
}
