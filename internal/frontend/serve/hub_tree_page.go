package serve

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// sessionPage is the newest rows of one folder plus the counts of the whole.
type sessionPage struct {
	Status                     *sessionstore.ListingStatus
	Sessions                   []treeSession
	Total, Archived, Remaining int
	// Said is whether any conversation here has had a turn, or a first turn is
	// in flight: what separates a folder with history from one only opened.
	Said bool
}

// workspacePage lists one folder's conversations, hiding what the pickers
// hide: subagent traces and redundant recovery copies. A copy that a pane has
// open stays, because something is driving it. limit bounds the rows built (0
// is all of them); the counts always cover every row the folder lists.
func (h *Hub) workspacePage(root, name string, open map[string]string, window treeWindow, limit int) sessionPage {
	return h.projectPage([]string{root}, name, open, window, limit)
}

func (h *Hub) projectPage(roots []string, name string, open map[string]string, window treeWindow, limit int) sessionPage {
	status := listingStatusFor(window.progressive)
	listed := []sessionstore.SessionInfo{}
	titlesByDir := map[string]titleSnapshot{}
	byDir := map[string]map[string]sessionstore.SessionInfo{}
	for _, root := range roots {
		dir := SessionDirFor(root)
		if _, seen := byDir[dir]; seen {
			continue
		}
		infos := h.listTreeSessions(dir, status, window.retryMetadata)
		listed = append(listed, infos...)
		titlesByDir[dir] = h.titleCacheFor(dir).snapshot()
		byDir[dir] = make(map[string]sessionstore.SessionInfo, len(infos))
		for _, si := range infos {
			byDir[dir][sessionstore.BranchID(si.Path)] = si
		}
	}
	sortTreeSessions(listed, window.recent)
	heldBy := openLookup(open)
	teamOwned := h.team.sessionPaths()
	out := make([]treeSession, 0, min(len(listed), max(limit, 0)))
	var total, archived, remaining, windowed int
	workspaceNamed := window.query != "" && strings.Contains(strings.ToLower(name), window.query)
	// Lineage root -> the row that copies of it fold into, or -1 when that row
	// is past the window. The list is newest-first, so the first copy to arrive
	// is the one still being written.
	lead := map[string]int{}
	for _, si := range listed {
		dir := filepath.Dir(si.Path)
		byID, titles := byDir[dir], titlesByDir[dir]
		lineage := dir + "\x00" + recoveryLineageRoot(si, byID)
		base := filepath.Base(si.Path)
		if hiddenFromProject(base, si.Path, teamOwned) {
			continue
		}
		runtimeID := heldBy(si.Path)
		if runtimeID == "" && hiddenRecoveryCopy(si, "") {
			continue
		}
		name := strings.TrimSuffix(base, ".jsonl")
		// A pane driving a copy keeps its own row: folding it away would hide
		// the conversation someone is looking at.
		folds := si.Recovered && runtimeID == ""
		if folds {
			if at, ok := lead[lineage]; ok {
				if at >= 0 {
					out[at].Copies = append(out[at].Copies, treeSession{Path: si.Path, Name: name, Turns: si.Turns})
				}
				continue
			}
		}
		total++
		if si.Archived {
			archived++
		}
		titleOf := rowTitle(titles, si, name)
		matches := window.matchesRow(si, titleOf, name, workspaceNamed)
		inWindow := matches && (limit == 0 || windowed < limit)
		if inWindow {
			windowed++
		}
		if !inWindow && !window.keepsOutsideWindow(si, runtimeID) {
			if matches {
				remaining++
			}
			if folds {
				lead[lineage] = -1
			}
			continue
		}
		title := titleOf()
		if folds {
			lead[lineage] = len(out)
		}
		row := treeSession{
			Path: si.Path, Name: name, Title: title, Turns: si.Turns, RuntimeID: runtimeID, Archived: si.Archived, Unread: si.Unread, ViewedAt: unreadSinceMs(si), LastActiveAt: activeAtMs(si), Legacy: sessionstore.IsForeignSessionLog(si.Path),
		}
		if window.progressive {
			known := si.CountsKnown
			row.CountsKnown, row.MetadataState = &known, si.MetadataState
		}
		out = append(out, row)
		decorateArchiveState(&out[len(out)-1], si.Path)
	}
	unlisted := []treeSession{}
	for _, root := range roots {
		attachVersions(SessionDirFor(root), out, h.openSessionsIn(root))
		if !window.recent {
			unlisted = append(unlisted, h.unlistedOpenSessions(root, out, listed)...)
		}
	}
	// Never nil: the frontend iterates rows, and a null blanks the window.
	rows := append(append([]treeSession{}, unlisted...), out...)
	for _, root := range roots {
		decorateWorkingDirectory(rows, root)
	}
	said := len(unlisted) > 0
	for _, si := range listed {
		said = said || si.Turns > 0
	}
	if window.progressive {
		h.prioritizeTreeRows(byDir, rows)
	}
	return sessionPage{Sessions: rows, Total: total + len(unlisted), Archived: archived, Remaining: remaining, Said: said, Status: status}
}

func sortTreeSessions(listed []sessionstore.SessionInfo, recent bool) {
	if !recent {
		sortSessions(listed)
		return
	}
	slices.SortFunc(listed, func(a, b sessionstore.SessionInfo) int {
		if c := cmp.Compare(activeAtMs(b), activeAtMs(a)); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})
}

func (w treeWindow) keepsOutsideWindow(si sessionstore.SessionInfo, runtimeID string) bool {
	return !w.recent && (runtimeID != "" || si.Pinned && !si.Archived)
}

func decorateWorkingDirectory(rows []treeSession, root string) {
	dir := SessionDirFor(root)
	for i := range rows {
		if filepath.Dir(rows[i].Path) == dir {
			rows[i].WorkspaceRoot = root
			rows[i].Isolated = worktree.IsManagedPath(root, config.DeliveryWorktreeDir())
		}
		decorateWorkingDirectory(rows[i].Copies, root)
		decorateWorkingDirectory(rows[i].Versions, root)
	}
}

// openLookup answers which pane holds a listed path. Canonicalising a path costs
// a stat per ancestor, so it is only paid for a file name some pane holds; on a
// folder of thousands of conversations that is the difference between one
// request and a second of disk.
func openLookup(open map[string]string) func(path string) string {
	names := make(map[string]bool, len(open))
	for held := range open {
		names[strings.ToLower(filepath.Base(held))] = true
	}
	return func(path string) string {
		if !names[strings.ToLower(filepath.Base(path))] {
			return ""
		}
		return open[sessionstore.CanonicalSessionPath(path)]
	}
}

// rowTitle reads a row's title once, on first use: a name the user typed
// outranks the generated one, and without that a rename would be written to the
// sidecar and never show up.
func rowTitle(titles titleSnapshot, si sessionstore.SessionInfo, name string) func() string {
	var title string
	done := false
	return func() string {
		if !done {
			done = true
			title = titles.display(name+".jsonl", titleSource(si.Preview), sessionstore.SessionContentModTime(si.Path).UnixNano(), si.CustomTitle)
			if title == "" {
				title = previewTitle(si.Preview)
			}
		}
		return title
	}
}

// matchesRow says whether a row is one the window's scope and word are about.
// The word is read against the title, or the name where there is none, and a
// folder whose own name holds it keeps every row.
func (w treeWindow) matchesRow(si sessionstore.SessionInfo, title func() string, name string, workspaceNamed bool) bool {
	if w.hideLegacy && w.query == "" && sessionstore.IsForeignSessionLog(si.Path) {
		return false
	}
	if (w.scope == scopeArchived && !si.Archived) || (w.scope == scopeActive && si.Archived) {
		return false
	}
	if w.query == "" || workspaceNamed {
		return true
	}
	label := title()
	if label == "" {
		label = name
	}
	return strings.Contains(strings.ToLower(label), w.query)
}

func unreadSinceMs(si sessionstore.SessionInfo) int64 {
	if !si.Unread || si.ViewedAt.IsZero() {
		return 0
	}
	return si.ViewedAt.UnixMilli()
}

func activeAtMs(si sessionstore.SessionInfo) int64 {
	if si.LastActivityAt.IsZero() {
		return 0
	}
	return si.LastActivityAt.UnixMilli()
}

// hiddenFromProject reports a conversation the project list leaves out: a
// sub-agent's transcript, or one of the team's agents'.
func hiddenFromProject(base, path string, team map[string]bool) bool {
	return store.IsSubagentTranscriptName(base) || team[sessionstore.CanonicalSessionPath(path)]
}
