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
}

// sessions lists saved sessions with event-log-aware titles and turn counts.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	ctrl := s.ctl()
	dir := ctrl.SessionDir()
	var entries []os.DirEntry
	if dir != "" {
		var err error
		entries, err = os.ReadDir(dir)
		if os.IsNotExist(err) {
			entries = nil
		} else if err != nil {
			writeJSON(w, []any{})
			return
		}
	}
	current := agent.CanonicalSessionPath(ctrl.SessionPath())
	running := map[string]bool{}
	s.detachedMu.Lock()
	for path, detached := range s.detached {
		running[filepath.Clean(path)] = controllerHasActiveRuntimeWork(detached.ctrl)
	}
	s.detachedMu.Unlock()
	legacyRows := make([]sessionListEntry, 0, len(entries))
	legacyByPath := make(map[string]sessionListEntry, len(entries))
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
		legacyRows = append(legacyRows, row)
		legacyByPath[cleanPath] = row
	}
	canonicalRows := make([]canonicalSessionRow, 0)
	if concrete, ok := ctrl.(*control.Controller); ok {
		if service := concrete.SessionService(); service != nil {
			_, runtime, bound := concrete.SessionBinding()
			page, listErr := service.Query().List(r.Context(), "", 100)
			if listErr == nil {
				canonicalRows = make([]canonicalSessionRow, 0, len(page.Sessions))
				for _, info := range page.Sessions {
					row := sessionListEntry{
						HostID: info.Ref.HostID, SessionID: info.Ref.SessionID, Name: info.SessionID,
						Title: info.Title, Turns: info.Turns, MtimeMilli: info.CreatedAt.UnixMilli(),
						Current:       bound && info.Ref == runtime.Ref(),
						Preview:       info.Preview,
						MetadataReady: info.MetadataStatus == session.MetadataReady,
					}
					// Canonical rows carry no legacy preview fallback; without
					// one a chatted session lists as an untitled blank until
					// the model renames it. Fall back to the first user
					// message, mirroring the legacy row's previewTitle.
					if strings.TrimSpace(row.Title) == "" && strings.TrimSpace(info.Preview) != "" {
						preview := []rune(strings.TrimSpace(info.Preview))
						if len(preview) > 50 {
							row.Title = string(preview[:47]) + "..."
						} else {
							row.Title = string(preview)
						}
					}
					if live, exists := service.Runtime(info.Ref); exists {
						phase := live.Snapshot().Phase
						row.Running = phase.Busy()
					}
					canonicalRows = append(canonicalRows, canonicalSessionRow{row: row, info: info})
				}
			}
		}
	}
	// Migration deliberately preserves the old transcript, so a host can
	// contain both the source .jsonl and its canonical session directory. The
	// source is not a second user-visible session once the migration map proves
	// there is exactly one canonical target for it. Keep the canonical row as
	// the authoritative open/delete identity, while borrowing the old preview
	// title until its asynchronous catalog metadata is ready.
	migration := loadMigrationIndex(canonicalRows)
	out := make([]sessionListEntry, 0, len(legacyRows)+len(canonicalRows))
	for _, row := range legacyRows {
		if _, migrated := migration.bySource[agent.CanonicalSessionPath(row.Path)]; migrated {
			continue
		}
		out = append(out, row)
	}
	for i := range canonicalRows {
		row := &canonicalRows[i].row
		if source, ok := migration.byTarget[row.SessionID]; ok {
			if legacy, exists := legacyByPath[source]; exists {
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
		}
		out = append(out, *row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].MtimeMilli > out[j].MtimeMilli })
	writeJSON(w, out)
}

type canonicalSessionRow struct {
	row  sessionListEntry
	info session.SessionInfo
}

type migrationSourceIndex struct {
	bySource map[string]struct{}
	byTarget map[string]string
}

// loadMigrationIndex returns only unambiguous source->target mappings.
// Multiple canonical targets can legitimately be produced from one legacy DAG
// head, in which case hiding the source would remove a still-distinct view.
func loadMigrationIndex(rows []canonicalSessionRow) migrationSourceIndex {
	index := migrationSourceIndex{bySource: map[string]struct{}{}, byTarget: map[string]string{}}
	canonicalIDs := make(map[string]struct{}, len(rows))
	roots := make(map[string]struct{})
	for _, row := range rows {
		if row.row.SessionID != "" {
			canonicalIDs[row.row.SessionID] = struct{}{}
		}
		if path := strings.TrimSpace(row.info.Path); path != "" {
			roots[filepath.Dir(filepath.Clean(path))] = struct{}{}
		}
	}
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
			if source == "" || target == "" {
				continue
			}
			if _, exists := canonicalIDs[target]; !exists {
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
