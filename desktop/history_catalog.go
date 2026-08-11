package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/history"
	"reasonix/internal/historycatalog"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

type HistorySessionPageRequest struct {
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	Status        string `json:"status"`
	TimeFilter    string `json:"timeFilter"`
	Query         string `json:"query"`
	Cursor        string `json:"cursor"`
	Limit         int    `json:"limit"`
}

type HistorySessionPage struct {
	Items       []SessionMeta `json:"items"`
	NextCursor  string        `json:"nextCursor"`
	Revision    uint64        `json:"revision"`
	Partial     bool          `json:"partial"`
	StaleCursor bool          `json:"staleCursor"`
}

type HistorySearchRequest struct {
	Query         string   `json:"query"`
	Scope         string   `json:"scope"`
	WorkspaceRoot string   `json:"workspaceRoot,omitempty"`
	Status        string   `json:"status"`
	TimeFilter    string   `json:"timeFilter"`
	Kinds         []string `json:"kinds"`
	ToolName      string   `json:"toolName"`
	Cursor        string   `json:"cursor"`
	Limit         int      `json:"limit"`
}

type HistorySearchHit struct {
	SessionPath    string  `json:"sessionPath"`
	SessionID      string  `json:"sessionId"`
	Source         string  `json:"source"`
	MessageIndex   int     `json:"messageIndex"`
	Role           string  `json:"role"`
	Kind           string  `json:"kind"`
	ToolName       string  `json:"toolName,omitempty"`
	Snippet        string  `json:"snippet"`
	Score          float64 `json:"score"`
	SessionTitle   string  `json:"sessionTitle,omitempty"`
	TopicTitle     string  `json:"topicTitle,omitempty"`
	WorkspaceRoot  string  `json:"workspaceRoot,omitempty"`
	LastActivityAt int64   `json:"lastActivityAt"`
	Open           bool    `json:"open"`
	Running        bool    `json:"running"`
	Current        bool    `json:"current"`
}

type HistoryIndexStatus = historycatalog.Status

type HistoryIndexChangedV1 struct {
	Revision uint64   `json:"revision"`
	Indexed  int64    `json:"indexed"`
	Total    int64    `json:"total"`
	Pending  int64    `json:"pending"`
	Roots    []string `json:"roots"`
	Reason   string   `json:"reason"`
}

func (a *App) registerHistoryIndexEvents() {
	history.RegisterCatalogObserver(func(status historycatalog.Status, roots []string, reason string) {
		if roots == nil {
			roots = []string{}
		}
		a.emitRuntimeEvent("history-index:changed-v1", HistoryIndexChangedV1{Revision: status.Revision,
			Indexed: status.Indexed, Total: status.Total, Pending: status.Pending, Roots: roots, Reason: reason})
	})
}

type HistorySearchPage struct {
	Items       []HistorySearchHit `json:"items"`
	NextCursor  string             `json:"nextCursor"`
	Revision    uint64             `json:"revision"`
	Partial     bool               `json:"partial"`
	StaleCursor bool               `json:"staleCursor"`
	Status      HistoryIndexStatus `json:"status"`
}

type HistorySearchContextRequest struct {
	SessionPath  string `json:"sessionPath"`
	MessageIndex int    `json:"messageIndex"`
	Before       int    `json:"before"`
	After        int    `json:"after"`
}

type HistorySearchContextLine struct {
	Index int    `json:"index"`
	Role  string `json:"role"`
	Text  string `json:"text"`
}

type historySearchCursor struct {
	Revision uint64  `json:"r"`
	Rank     float64 `json:"b"`
	Score    float64 `json:"s"`
	Path     string  `json:"p"`
	Message  int     `json:"m"`
	Part     int     `json:"a"`
	RowID    int64   `json:"i"`
}

func historyCatalogRoots(targets []sessioncatalog.DirectoryTarget) []historycatalog.Root {
	roots := make([]historycatalog.Root, 0, len(targets)*2+1)
	for _, target := range targets {
		source := target.Scope
		if source == "" {
			source = "global"
		}
		root := historycatalog.Root{Path: target.Path, Source: source, Scope: target.Scope, WorkspaceRoot: target.WorkspaceRoot}
		roots = append(roots, root)
		root.Path = filepath.Join(target.Path, "subagents")
		root.Subagents = true
		roots = append(roots, root)
	}
	roots = append(roots, historycatalog.Root{Path: config.ArchiveDir(), Source: "archive", Scope: "global", Archive: true})
	return roots
}

func sessionMetaFromCatalog(record sessioncatalog.SessionRecord, current, open bool) SessionMeta {
	title := strings.TrimSpace(record.CustomTitle)
	preview := record.Preview
	if strings.TrimSpace(preview) == "" && record.TurnsState == sessioncatalog.TurnsUnknown {
		preview = "History is being indexed — " + filepath.Base(record.Path)
	}
	recovered := record.Recovered || strings.TrimSpace(record.RecoveryDigest) != "" || isAutomaticRecoverySessionPath(record.Path)
	return SessionMeta{Path: record.Path, Preview: preview, Title: title, Turns: record.Turns,
		TurnsState: string(record.TurnsState), CreatedAt: record.CreatedAt, LastActivityAt: record.LastActivityAt,
		ModTime: record.LastActivityAt, Current: current, Open: open, Scope: record.Scope,
		WorkspaceRoot: record.WorkspaceRoot, TopicID: record.TopicID, TopicTitle: record.TopicTitle,
		Recovered: recovered}
}

func (a *App) ListHistorySessions(req HistorySessionPageRequest) HistorySessionPage {
	out := HistorySessionPage{Items: []SessionMeta{}}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		out.Partial = true
		return out
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	statusFilter := strings.TrimSpace(req.Status)
	needRuntimeFilter := statusFilter == "open" || statusFilter == "current"
	_, overlays := a.catalogRuntimeOverlays()
	active := a.activeSessionPath(a.activeSessionDir())
	cursor := req.Cursor
	for {
		page, err := catalog.ListSessions(a.bootContext(), sessioncatalog.SessionPageRequest{
			Scope: req.Scope, WorkspaceRoot: req.WorkspaceRoot, Cursor: cursor, Limit: limit,
			Query: req.Query, TimeFilter: req.TimeFilter,
		})
		if err != nil {
			out.Partial = true
			return out
		}
		out.Revision = page.Revision
		if page.StaleCursor {
			out.StaleCursor = true
			return out
		}
		for i, record := range page.Items {
			overlay := overlays[sessionRuntimeKey(record.Path)]
			// Match frontend HistoryPanel: "open" means open-but-not-current.
			if statusFilter == "open" && (!overlay.open || record.Path == active) {
				continue
			}
			if statusFilter == "current" && record.Path != active {
				continue
			}
			out.Items = append(out.Items, sessionMetaFromCatalog(record, record.Path == active, overlay.open))
			if len(out.Items) == limit {
				if i+1 < len(page.Items) || page.NextCursor != "" {
					out.NextCursor = sessioncatalog.CursorAfter(page.Revision, record.LastActivityAt, record.Path)
				}
				status := catalog.Status()
				out.Partial = status.State != sessioncatalog.StateReady || status.Indexed < status.Total
				return out
			}
		}
		if !needRuntimeFilter {
			// No post-filter: a single catalog page is the whole result page.
			out.NextCursor = page.NextCursor
			break
		}
		if page.NextCursor == "" {
			out.NextCursor = ""
			break
		}
		cursor = page.NextCursor
	}
	status := catalog.Status()
	out.Partial = status.State != sessioncatalog.StateReady || status.Indexed < status.Total
	return out
}

func (a *App) GetHistoryIndexStatus() HistoryIndexStatus {
	if catalog := history.SharedCatalog(); catalog != nil {
		return catalog.Status()
	}
	return HistoryIndexStatus{State: "opening", Pending: 1, Mode: "memory"}
}

func decodeHistoryCursor(encoded string) (*historySearchCursor, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	var cursor historySearchCursor
	if err := json.Unmarshal(b, &cursor); err != nil || cursor.Path == "" {
		return nil, errors.New("invalid history search cursor")
	}
	return &cursor, nil
}

func encodeHistoryCursor(cursor historySearchCursor) string {
	b, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(b)
}

func desktopHistoryText(messages []provider.Message, candidate historycatalog.Candidate) (string, bool) {
	if candidate.MessageIndex < 0 || candidate.MessageIndex >= len(messages) {
		return "", false
	}
	message := messages[candidate.MessageIndex]
	switch candidate.Kind {
	case "user_text", "assistant_text":
		return message.Content, true
	case "tool_input":
		if candidate.PartIndex < 0 || candidate.PartIndex >= len(message.ToolCalls) {
			return "", false
		}
		call := message.ToolCalls[candidate.PartIndex]
		return strings.TrimSpace(call.Name + " " + call.Arguments), true
	case "tool_error", "tool_output":
		return strings.TrimSpace(message.Name + " " + message.Content), true
	default:
		return "", false
	}
}

func (a *App) SearchHistoryContent(req HistorySearchRequest) HistorySearchPage {
	status := a.GetHistoryIndexStatus()
	out := HistorySearchPage{Items: []HistorySearchHit{}, Status: status, Revision: status.Revision,
		Partial: status.State != "ready" || status.Pending > 0 || (status.Total > 0 && status.Indexed < status.Total)}
	catalog := history.SharedCatalog()
	if catalog == nil || strings.TrimSpace(req.Query) == "" {
		return out
	}
	cursor, err := decodeHistoryCursor(req.Cursor)
	if err != nil {
		return out
	}
	if cursor != nil && cursor.Revision != status.Revision {
		out.StaleCursor = true
		return out
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	kinds := req.Kinds
	if len(kinds) == 0 {
		kinds = []string{"user_text", "assistant_text", "tool_input", "tool_error"}
	}
	var after *historycatalog.SearchCursor
	if cursor != nil {
		after = &historycatalog.SearchCursor{Rank: cursor.Rank, SessionPath: cursor.Path, MessageIndex: cursor.Message,
			PartIndex: cursor.Part, RowID: cursor.RowID}
	}
	// Keep pulling FTS candidates until the filtered page is full.
	items, nextCursor, lastErr := a.collectHistorySearchItems(req, catalog, kinds, historySearchRootFilter(a, req), after, limit, status.Revision)
	if lastErr != "" {
		out.Status.LastError = lastErr
		return out
	}
	out.Items, out.NextCursor = items, nextCursor
	return out
}

func historyTimeMatches(timestamp int64, filter string) bool {
	if strings.TrimSpace(filter) == "" || filter == "all" {
		return true
	}
	now := time.Now()
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	value := time.UnixMilli(timestamp)
	switch filter {
	case "today":
		return !value.Before(startToday)
	case "yesterday":
		return !value.Before(startToday.AddDate(0, 0, -1)) && value.Before(startToday)
	case "older":
		return value.Before(startToday.AddDate(0, 0, -1))
	default:
		return true
	}
}

func (a *App) GetHistorySearchContext(req HistorySearchContextRequest) []HistorySearchContextLine {
	var options history.Options
	bestLength := -1
	for _, root := range historyCatalogRoots(a.sessionCatalogTargets()) {
		if !desktopHistoryPathWithin(req.SessionPath, root.Path) || len(root.Path) <= bestLength {
			continue
		}
		bestLength = len(root.Path)
		options = history.Options{}
		switch {
		case root.Archive:
			options.ArchiveDir = root.Path
		case root.Subagents:
			options.SessionDir = filepath.Dir(root.Path)
		default:
			options.SessionDir = root.Path
		}
	}
	if bestLength < 0 {
		return []HistorySearchContextLine{}
	}
	searcher := history.NewSearcher(options)
	lines, err := searcher.Around(a.bootContext(), history.AroundRequest{SessionPath: req.SessionPath, MessageIndex: req.MessageIndex, Before: req.Before, After: req.After})
	if err != nil {
		return []HistorySearchContextLine{}
	}
	out := make([]HistorySearchContextLine, 0, len(lines))
	for _, line := range lines {
		role := ""
		if fields := strings.Fields(line.Text); len(fields) > 1 {
			role = strings.TrimSuffix(fields[1], "]")
		}
		out = append(out, HistorySearchContextLine{Index: line.Index, Role: role, Text: line.Text})
	}
	return out
}

func desktopHistoryPathWithin(path, root string) bool {
	absPath, err := filepath.Abs(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(filepath.Clean(strings.TrimSpace(root)))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func (a *App) RebuildHistoryIndex() error {
	if a == nil || a.shuttingDown.Load() {
		return errors.New("application is shutting down")
	}
	return history.RebuildSharedCatalog(a.bootContext(), historyCatalogRoots(a.sessionCatalogTargets()))
}
