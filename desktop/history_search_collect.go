package main

import (
	"context"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/historycatalog"
	"reasonix/internal/provider"
	"reasonix/internal/retrieval"
)

func historySearchRootFilter(a *App, req HistorySearchRequest) []string {
	rootFilter := []string{}
	for _, root := range historyCatalogRoots(a.sessionCatalogTargets()) {
		if req.Scope == "project" && (root.Scope != "project" || !sameProjectRoot(root.WorkspaceRoot, req.WorkspaceRoot)) {
			continue
		}
		if req.Scope == "global" && root.Scope == "project" {
			continue
		}
		rootFilter = append(rootFilter, root.Path)
	}
	return rootFilter
}

func historyStatusMatches(status string, open, current bool) bool {
	switch strings.TrimSpace(status) {
	case "open":
		// Match HistoryPanel: open-but-not-current.
		return open && !current
	case "current":
		return current
	default:
		return true
	}
}

func (a *App) historyHitFromCandidate(
	req HistorySearchRequest,
	candidate historycatalog.Candidate,
	active string,
	overlays map[string]catalogRuntimeOverlay,
	queryTerms []string,
	loaded map[string][]provider.Message,
	catalog *historycatalog.Catalog,
) (HistorySearchHit, bool) {
	overlay := overlays[sessionRuntimeKey(candidate.SessionPath)]
	current := candidate.SessionPath == active
	if !historyStatusMatches(req.Status, overlay.open, current) {
		return HistorySearchHit{}, false
	}
	if !historyTimeMatches(candidate.LastActivityAt, req.TimeFilter) {
		return HistorySearchHit{}, false
	}
	messages, ok := loaded[candidate.SessionPath]
	if !ok {
		if identity, known, identityErr := agent.SessionContentIdentity(candidate.SessionPath); identityErr == nil && known &&
			candidate.ContentDigest != "" && identity.DigestHex != candidate.ContentDigest {
			catalog.EnqueueExisting(context.Background(), candidate.SessionPath)
			return HistorySearchHit{}, false
		}
		session, loadErr := agent.LoadSession(candidate.SessionPath)
		if loadErr != nil {
			return HistorySearchHit{}, false
		}
		messages = session.Snapshot()
		loaded[candidate.SessionPath] = messages
	}
	text, ok := desktopHistoryText(messages, candidate)
	if !ok {
		catalog.EnqueueExisting(context.Background(), candidate.SessionPath)
		return HistorySearchHit{}, false
	}
	return HistorySearchHit{
		SessionPath: candidate.SessionPath,
		SessionID:   strings.TrimSuffix(filepath.Base(candidate.SessionPath), filepath.Ext(candidate.SessionPath)),
		Source:      candidate.Source, MessageIndex: candidate.MessageIndex, Role: candidate.Role,
		Kind: candidate.Kind, ToolName: candidate.ToolName,
		Snippet: retrieval.MakeSnippet(text, req.Query, queryTerms, 240), Score: candidate.Score,
		SessionTitle: candidate.SessionTitle, TopicTitle: candidate.TopicTitle, WorkspaceRoot: candidate.WorkspaceRoot,
		LastActivityAt: candidate.LastActivityAt, Open: overlay.open, Running: overlay.running, Current: current,
	}, true
}

func (a *App) collectHistorySearchItems(
	req HistorySearchRequest,
	catalog *historycatalog.Catalog,
	kinds, rootFilter []string,
	after *historycatalog.SearchCursor,
	limit int,
	revision uint64,
) ([]HistorySearchHit, string, string) {
	const batchLimit = 200
	need := limit + 1
	_, overlays := a.catalogRuntimeOverlays()
	active := a.activeSessionPath(a.activeSessionDir())
	queryTerms, _ := retrieval.QueryTerms(req.Query)
	loaded := map[string][]provider.Message{}
	items := []HistorySearchHit{}
	var cursorCandidate historycatalog.Candidate
	for len(items) < need {
		result, searchErr := catalog.Search(a.bootContext(), historycatalog.SearchRequest{
			Query: req.Query, Scope: req.Scope, WorkspaceRoot: req.WorkspaceRoot,
			Kinds: kinds, ToolName: req.ToolName, Limit: batchLimit, Roots: rootFilter, After: after,
		})
		if searchErr != nil {
			return items, "", searchErr.Error()
		}
		if len(result.Items) == 0 {
			break
		}
		for _, candidate := range result.Items {
			hit, ok := a.historyHitFromCandidate(req, candidate, active, overlays, queryTerms, loaded, catalog)
			if !ok {
				continue
			}
			items = append(items, hit)
			if len(items) == limit {
				cursorCandidate = candidate
			}
			if len(items) == need {
				break
			}
		}
		if len(items) == need {
			break
		}
		last := result.Items[len(result.Items)-1]
		after = &historycatalog.SearchCursor{Rank: last.Rank, SessionPath: last.SessionPath, MessageIndex: last.MessageIndex,
			PartIndex: last.PartIndex, RowID: last.RowID}
		if len(result.Items) < batchLimit {
			break
		}
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = encodeHistoryCursor(historySearchCursor{Revision: revision, Rank: cursorCandidate.Rank, Score: cursorCandidate.Score,
			Path: cursorCandidate.SessionPath, Message: cursorCandidate.MessageIndex, Part: cursorCandidate.PartIndex, RowID: cursorCandidate.RowID})
	}
	return items, nextCursor, ""
}
