package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/session"
	"reasonix/internal/store"
)

type sessionListEntry struct {
	HostID     string `json:"hostId,omitempty"`
	SessionID  string `json:"sessionId,omitempty"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Title      string `json:"title,omitempty"`
	Turns      int    `json:"turns,omitempty"`
	Current    bool   `json:"current,omitempty"`
	Running    bool   `json:"running,omitempty"`
	TakenOver  bool   `json:"takenOver,omitempty"`
	MtimeMilli int64  `json:"mtimeMilli"`

	Preview       string `json:"preview,omitempty"`
	MetadataReady bool   `json:"metadataReady,omitempty"`
	// A pointer distinguishes a supported zero baseline from legacy listings.
	ResultSequence *uint64 `json:"resultSequence,omitempty"`
}

// sessions lists saved sessions with event-log-aware titles and turn counts.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	ctrl := s.ctl()
	entries, ok := readSessionDir(ctrl.SessionDir())
	if !ok {
		writeJSON(w, []any{})
		return
	}
	out := mergeSessionRows(s.legacySessionRows(r, ctrl, entries))
	sort.SliceStable(out, func(i, j int) bool { return out[i].MtimeMilli > out[j].MtimeMilli })
	writeJSON(w, out)
}

// readSessionDir reports ok=false only when the directory exists but cannot be
// read; a missing directory is an empty session list, not a failure.
func readSessionDir(dir string) ([]os.DirEntry, bool) {
	if dir == "" {
		return nil, true
	}
	entries, err := os.ReadDir(dir)
	switch {
	case os.IsNotExist(err):
		return nil, true
	case err != nil:
		return nil, false
	}
	return entries, true
}

// legacySessionRows builds one row per .jsonl transcript plus the same rows
// keyed by canonical path, which is how a canonical row later finds the
// migrated source whose title and turn count it borrows.
func (s *Server) legacySessionRows(r *http.Request, ctrl control.SessionAPI, entries []os.DirEntry) ([]sessionListEntry, map[string]sessionListEntry, []canonicalSessionRow) {
	dir := ctrl.SessionDir()
	current := agent.CanonicalSessionPath(ctrl.SessionPath())
	running := s.detachedRuntimeWork()
	rows := make([]sessionListEntry, 0, len(entries))
	byPath := make(map[string]sessionListEntry, len(entries))
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
		row := sessionListEntry{
			Name:       strings.TrimSuffix(entry.Name(), ".jsonl"),
			Path:       path,
			Current:    cleanPath == current,
			Running:    running[cleanPath],
			TakenOver:  s.sessionMirrored(cleanPath) || leaseHeldByForeignRuntime(cleanPath),
			MtimeMilli: mtime.UnixMilli(),
		}
		if row.Current {
			row.Running = controllerHasActiveRuntimeWork(ctrl) && !row.TakenOver
		}
		first, turns, cached := agent.SessionPreviewCached(path)
		if !cached {
			first, turns = agent.SessionPreview(path)
		}
		if turns > 0 {
			row.Turns = turns
			row.Title = s.sessionTitle(r.Context(), entry.Name(), first, mtime.UnixNano())
		}
		rows = append(rows, row)
		byPath[cleanPath] = row
	}
	return rows, byPath, s.canonicalSessionRows(r, ctrl)
}

func (s *Server) detachedRuntimeWork() map[string]bool {
	running := map[string]bool{}
	s.detachedMu.Lock()
	defer s.detachedMu.Unlock()
	for path, detached := range s.detached {
		running[filepath.Clean(path)] = controllerHasActiveRuntimeWork(detached.ctrl)
	}
	return running
}

// canonicalSessionRows follows the catalog cursor to the end. One List call
// caps at 100 rows ordered by the random session id, so a workspace past 100
// sessions would otherwise hide an arbitrary subset — including a session just
// taken over. The row bound keeps a broken cursor from looping forever.
func (s *Server) canonicalSessionRows(r *http.Request, ctrl control.SessionAPI) []canonicalSessionRow {
	rows := make([]canonicalSessionRow, 0)
	concrete, ok := ctrl.(*control.Controller)
	if !ok {
		return rows
	}
	service := concrete.SessionService()
	if service == nil {
		return rows
	}
	_, runtime, bound := concrete.SessionBinding()
	const pageLimit = 100
	const maxCanonicalRows = 500
	cursor := ""
	for pages := 1; ; pages++ {
		page, err := service.Query().List(r.Context(), cursor, pageLimit)
		if err != nil {
			break
		}
		if len(rows) == 0 {
			rows = make([]canonicalSessionRow, 0, len(page.Sessions))
		}
		for _, info := range page.Sessions {
			resultSequence := info.ResultSequence
			row := sessionListEntry{
				HostID: info.Ref.HostID, SessionID: info.Ref.SessionID, Name: info.SessionID,
				Title: info.Title, Turns: info.Turns, MtimeMilli: info.CreatedAt.UnixMilli(),
				Current:        bound && info.Ref == runtime.Ref(),
				Preview:        info.Preview,
				MetadataReady:  info.MetadataStatus == session.MetadataReady,
				ResultSequence: &resultSequence,
				TakenOver:      s.sessionMirrored(remoteSessionIDQueryPrefix + info.Ref.SessionID),
			}
			// Canonical rows carry no legacy preview fallback, so a chatted
			// session would list as untitled until the model renames it.
			if strings.TrimSpace(row.Title) == "" && strings.TrimSpace(info.Preview) != "" {
				row.Title = truncatedPreview(info.Preview)
			}
			if live, exists := service.Runtime(info.Ref); exists {
				row.Running = live.Snapshot().Phase.Busy()
			}
			rows = append(rows, canonicalSessionRow{row: row, info: info})
		}
		if page.NextCursor == "" || page.NextCursor == cursor || pages*pageLimit >= maxCanonicalRows || len(rows) >= maxCanonicalRows {
			break
		}
		cursor = page.NextCursor
	}
	return rows
}

// truncatedPreview clamps a catalog preview the way previewTitle clamps a
// transcript's first message. Catalog previews are already plain user text, so
// they need no paste-label stripping.
func truncatedPreview(preview string) string {
	preview = strings.TrimSpace(preview)
	if r := []rune(preview); len(r) > 50 {
		return string(r[:47]) + "..."
	}
	return preview
}

// mergeSessionRows produces the user-visible list from both catalogs.
//
// Migration deliberately preserves the old transcript, so a host can contain
// both the source .jsonl and its canonical session directory. The source is not
// a second user-visible session once the migration map proves there is exactly
// one canonical target for it; the canonical row stays the authoritative
// open/delete identity and borrows the old preview title until its asynchronous
// catalog metadata is ready.
func mergeSessionRows(legacyRows []sessionListEntry, legacyByPath map[string]sessionListEntry, canonicalRows []canonicalSessionRow) []sessionListEntry {
	migration := migrationIndexFor(canonicalRows)
	// The engine mirrors an in-flight legacy transcript into a final-format
	// event log keyed by the legacy branch id. That mirror is plumbing, not a
	// second conversation, except when it is the current row's own tree badge.
	legacyBranchIDs := make(map[string]struct{}, len(legacyByPath))
	for path := range legacyByPath {
		legacyBranchIDs[agent.BranchID(path)] = struct{}{}
	}
	out := make([]sessionListEntry, 0, len(legacyRows)+len(canonicalRows))
	for _, row := range legacyRows {
		if _, migrated := migration.bySource[agent.CanonicalSessionPath(row.Path)]; migrated {
			continue
		}
		out = append(out, row)
	}
	for i := range canonicalRows {
		row := &canonicalRows[i].row
		if !row.Current {
			if _, mirrored := legacyBranchIDs[row.SessionID]; mirrored {
				continue
			}
		}
		if source, ok := migration.byTarget[row.SessionID]; ok {
			inheritLegacyRow(row, legacyByPath[source])
		}
		out = append(out, *row)
	}
	return out
}

func inheritLegacyRow(row *sessionListEntry, legacy sessionListEntry) {
	if row.Title == "" {
		row.Title = legacy.Title
	}
	if row.Turns == 0 {
		row.Turns = legacy.Turns
	}
	if row.MtimeMilli < legacy.MtimeMilli {
		row.MtimeMilli = legacy.MtimeMilli
	}
}

func migrationIndexFor(canonicalRows []canonicalSessionRow) migrationSourceIndex {
	canonicalIDs := make(map[string]struct{}, len(canonicalRows))
	roots := make(map[string]struct{})
	for _, row := range canonicalRows {
		if row.row.SessionID != "" {
			canonicalIDs[row.row.SessionID] = struct{}{}
		}
		if path := strings.TrimSpace(row.info.Path); path != "" {
			roots[filepath.Dir(filepath.Clean(path))] = struct{}{}
		}
	}
	return loadMigrationIndex(roots, func(targetID string) bool {
		_, exists := canonicalIDs[targetID]
		return exists
	})
}

type canonicalSessionRow struct {
	row  sessionListEntry
	info session.SessionInfo
}

// migrationSourceIndex is the one rule for when a frozen legacy transcript is
// hidden behind its canonical row. Listing consults it to fold the source out
// of /sessions; deletion consults it to decide whether removing a canonical row
// may also remove the source. Both answers must agree, or a delete can remove a
// transcript the listing still shows as a distinct session.
type migrationSourceIndex struct {
	bySource map[string]struct{}
	byTarget map[string]string
}

// loadMigrationIndex reads the migration maps under roots and keeps only
// unambiguous source->target mappings. Multiple canonical targets can
// legitimately be produced from one legacy DAG head, in which case hiding or
// deleting the source would remove a still-distinct view. exists reports
// whether a target id is a live canonical row; targets that are gone do not
// count, so a source whose other targets were already deleted is again the
// sole source of the remaining one.
func loadMigrationIndex(roots map[string]struct{}, exists func(targetID string) bool) migrationSourceIndex {
	index := migrationSourceIndex{bySource: map[string]struct{}{}, byTarget: map[string]string{}}
	targetsBySource := make(map[string][]string)
	for root := range roots {
		data, err := os.ReadFile(filepath.Join(root, "migration-map.json"))
		if err != nil {
			continue
		}
		var mapping session.MigrationMapping
		if json.Unmarshal(data, &mapping) != nil || mapping.SchemaVersion != session.SchemaVersion {
			continue
		}
		for _, entry := range mapping.Entries {
			source := agent.CanonicalSessionPath(entry.SourcePath)
			target := strings.TrimSpace(entry.TargetID)
			if source == "" || target == "" || !exists(target) {
				continue
			}
			seen := false
			for _, existing := range targetsBySource[source] {
				seen = seen || existing == target
			}
			if !seen {
				targetsBySource[source] = append(targetsBySource[source], target)
			}
		}
	}
	for source, targets := range targetsBySource {
		if len(targets) != 1 {
			continue
		}
		index.bySource[source] = struct{}{}
		index.byTarget[targets[0]] = source
	}
	return index
}
