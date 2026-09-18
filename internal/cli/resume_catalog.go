package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/session"
)

// canonicalResumeScanCap bounds how many final-format catalog rows one picker
// open walks. It matches the Serve-side listing page so both surfaces see the
// same conversation universe even on long-lived workspaces.
const canonicalResumeScanCap = 100

// cliResumeTarget is one resumable conversation: either a legacy transcript
// path or a final-format session identity. Exactly one side is set.
type cliResumeTarget struct {
	path string             // legacy .jsonl transcript
	ref  session.SessionRef // sessions-v4 identity (SessionID != "" when canonical)
}

func (t cliResumeTarget) canonical() bool { return t.ref.SessionID != "" }

func (t cliResumeTarget) empty() bool { return t.path == "" && t.ref.SessionID == "" }

// canonicalResumeEntries lists final-format (sessions-v4) sessions sharing the
// workspace of the legacy session dir. The catalog is the authoritative store
// once a legacy transcript has been imported, so every resume surface must
// offer these rows or switching between the desktop and the CLI hides history.
func canonicalResumeEntries(ctx context.Context, sessionDir string) []resumeEntry {
	service := cliSessionService(sessionDir)
	if service == nil {
		return nil
	}
	var out []resumeEntry
	cursor := ""
	for len(out) < canonicalResumeScanCap {
		page, err := service.Query().List(ctx, cursor, canonicalResumeScanCap)
		if err != nil {
			break
		}
		for _, info := range page.Sessions {
			if canonicalResumeHidden(info) {
				continue
			}
			out = append(out, resumeEntry{
				session: canonicalResumeDisplayInfo(info),
				target:  cliResumeTarget{ref: info.Ref},
			})
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].session.ModTime.After(out[j].session.ModTime) })
	if len(out) > canonicalResumeScanCap {
		out = out[:canonicalResumeScanCap]
	}
	return out
}

// canonicalResumeHidden mirrors the legacy picker's empty-session rule for
// catalog rows: a session that never saw a user message (no completed turn,
// no preview, no title) is an empty placeholder and stays out of the picker.
// Rows whose metadata has not been rebuilt yet stay listed — an unindexed
// conversation must remain reachable, matching the desktop tree.
func canonicalResumeHidden(info session.SessionInfo) bool {
	if info.MetadataStatus != session.MetadataReady {
		return false
	}
	return info.Turns == 0 && strings.TrimSpace(info.Preview) == "" && strings.TrimSpace(info.Title) == ""
}

// canonicalResumeDisplayInfo projects a catalog row onto the picker's legacy
// row shape: the v4 directory stands in for the transcript path, the event-log
// revision time drives recency ordering, and a model-set catalog title rides
// the custom-title slot so sessionPickerLabel prefers it over the preview.
func canonicalResumeDisplayInfo(info session.SessionInfo) agent.SessionInfo {
	return agent.SessionInfo{
		Path: info.Path, Preview: strings.TrimSpace(info.Preview),
		CustomTitle: strings.TrimSpace(info.Title), Turns: info.Turns,
		ModTime: info.UpdatedAt, CountsKnown: true,
	}
}

// migratedLegacyIndex returns the legacy transcript paths that already have
// one final-format successor among the listed canonical entries, plus the
// reverse source-for-target map. Sources with exactly one successor are hidden
// from the picker: the canonical row is the continuation, and offering the
// frozen source again would fork a duplicate identity instead of resuming the
// conversation. Multiple successors stay visible for the same reason the
// Serve keeps them.
func migratedLegacyIndex(sessionDir string, canonical []resumeEntry) (map[string]struct{}, map[string]string) {
	root := session.RootForLegacyDir(sessionDir)
	if root == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(root, "migration-map.json"))
	if err != nil {
		return nil, nil
	}
	var mapping session.MigrationMapping
	if json.Unmarshal(data, &mapping) != nil || mapping.SchemaVersion != session.SchemaVersion {
		return nil, nil
	}
	listed := make(map[string]struct{}, len(canonical))
	for _, entry := range canonical {
		if entry.target.canonical() {
			listed[entry.target.ref.SessionID] = struct{}{}
		}
	}
	targets := make(map[string][]string)
	for _, entry := range mapping.Entries {
		source := agent.CanonicalSessionPath(entry.SourcePath)
		target := strings.TrimSpace(entry.TargetID)
		if source == "" || target == "" {
			continue
		}
		if _, ok := listed[target]; !ok {
			continue
		}
		seen := false
		for _, existing := range targets[source] {
			seen = seen || existing == target
		}
		if !seen {
			targets[source] = append(targets[source], target)
		}
	}
	bySource := make(map[string]struct{}, len(targets))
	byTarget := make(map[string]string, len(targets))
	for source, ids := range targets {
		if len(ids) == 1 {
			bySource[source] = struct{}{}
			byTarget[ids[0]] = source
		}
	}
	return bySource, byTarget
}

// workspaceResumeScan captures the shared facts every resume surface needs
// from one workspace: the legacy rows with migrated sources hidden (keeping
// their branch ids for mirror folding and a by-path lookup for label
// borrowing), and the visible canonical rows with engine mirrors folded and
// migration borrows applied.
type workspaceResumeScan struct {
	legacy       []agent.SessionInfo
	legacyByPath map[string]agent.SessionInfo
	legacyIDs    map[string]struct{}
	canonical    []resumeEntry
}

func scanWorkspaceResume(ctx context.Context, sessionDir string) workspaceResumeScan {
	canonical := canonicalResumeEntries(ctx, sessionDir)
	bySource, byTarget := migratedLegacyIndex(sessionDir, canonical)
	sessions, err := agent.ListSessions(sessionDir)
	if err != nil {
		sessions = nil
	}
	scan := workspaceResumeScan{
		legacyByPath: make(map[string]agent.SessionInfo, len(sessions)),
		legacyIDs:    make(map[string]struct{}, len(sessions)),
		legacy:       make([]agent.SessionInfo, 0, len(sessions)),
	}
	for _, info := range sessions {
		scan.legacyByPath[agent.CanonicalSessionPath(info.Path)] = info
		scan.legacyIDs[agent.BranchID(info.Path)] = struct{}{}
		if _, hidden := bySource[agent.CanonicalSessionPath(info.Path)]; hidden {
			continue
		}
		scan.legacy = append(scan.legacy, info)
	}
	for _, entry := range canonical {
		// The engine mirrors an in-flight legacy transcript into a final-format
		// event log whose session id is the legacy branch id. That mirror is
		// plumbing, not a second conversation: fold it into the legacy row.
		if _, mirrored := scan.legacyIDs[entry.target.ref.SessionID]; mirrored {
			continue
		}
		// Mirror the Serve listing's migration borrow: until the catalog row
		// grows its own title, the frozen source's label identifies the same
		// conversation on both surfaces.
		if source, migrated := byTarget[entry.target.ref.SessionID]; migrated {
			if legacy, ok := scan.legacyByPath[source]; ok {
				if entry.session.CustomTitle == "" {
					entry.session.CustomTitle = firstNonEmpty(legacy.CustomTitle, legacy.TopicTitle, legacy.Preview)
				}
				if entry.session.Turns == 0 {
					entry.session.Turns = legacy.Turns
				}
				if legacy.ModTime.After(entry.session.ModTime) {
					entry.session.ModTime = legacy.ModTime
				}
			}
		}
		scan.canonical = append(scan.canonical, entry)
	}
	return scan
}

// legacyResumeRows returns the workspace's legacy transcript rows with
// migrated sources removed, in ListSessions' newest-first order. Resuming a
// frozen source reuses its canonical target, so listing it beside that target
// only offers the same conversation twice under two identities.
func legacyResumeRows(sessionDir string) []agent.SessionInfo {
	if sessionDir == "" {
		return nil
	}
	return scanWorkspaceResume(context.Background(), sessionDir).legacy
}

// mergedResumeEntries unifies the legacy picker rows with the final-format
// catalog for one workspace, capped at limit with recovery families kept
// together and their leaf-first arrangement intact. Both hosts of the same
// workspace (desktop tree via Serve, CLI pickers) must offer the same
// conversations after a migration.
func mergedResumeEntries(sessionDir string, limit int) []resumeEntry {
	if sessionDir == "" {
		return nil
	}
	scan := scanWorkspaceResume(context.Background(), sessionDir)
	return mergeResumeStores(orderResumeSessions(scan.legacy), scan.canonical, limit)
}

// mergeResumeStores interleaves canonical rows with the ordered legacy rows
// without disturbing the legacy order itself: orderResumeSessions deliberately
// places a recovery family's writable leaf first, so numeric /resume indices
// must stay stable. Canonical rows slot between legacy family runs by
// recency, and the display cap keeps whole runs together.
func mergeResumeStores(legacy []agent.SessionInfo, canonical []resumeEntry, limit int) []resumeEntry {
	byID := make(map[string]agent.SessionInfo, len(legacy))
	for _, session := range legacy {
		byID[agent.BranchID(session.Path)] = session
	}
	merged := make([]resumeEntry, 0, len(legacy)+len(canonical))
	appendRun := func(run []agent.SessionInfo) {
		for _, info := range run {
			merged = append(merged, resumeEntry{session: info, target: cliResumeTarget{path: info.Path}})
		}
	}
	nextCanonical := 0
	runStart := 0
	for runStart < len(legacy) {
		key := recoveryResumeGroupKey(legacy[runStart], byID)
		runEnd := runStart + 1
		runActivity := legacy[runStart].ModTime
		for runEnd < len(legacy) && recoveryResumeGroupKey(legacy[runEnd], byID) == key {
			if legacy[runEnd].ModTime.After(runActivity) {
				runActivity = legacy[runEnd].ModTime
			}
			runEnd++
		}
		for nextCanonical < len(canonical) && !canonical[nextCanonical].session.ModTime.After(runActivity) {
			merged = append(merged, canonical[nextCanonical])
			nextCanonical++
		}
		appendRun(legacy[runStart:runEnd])
		runStart = runEnd
	}
	for ; nextCanonical < len(canonical); nextCanonical++ {
		merged = append(merged, canonical[nextCanonical])
	}
	return capResumeEntries(merged, limit)
}

// capResumeEntries limits the merged picker list while keeping recovery
// families intact at the display cap, mirroring capResumeSessionGroups over
// the entry shape that also carries canonical identities.
func capResumeEntries(entries []resumeEntry, limit int) []resumeEntry {
	if limit <= 0 || len(entries) <= limit {
		return entries
	}
	sessions := make([]agent.SessionInfo, len(entries))
	for i := range entries {
		sessions[i] = entries[i].session
	}
	byID := make(map[string]agent.SessionInfo, len(sessions))
	for _, session := range sessions {
		byID[agent.BranchID(session.Path)] = session
	}
	out := make([]resumeEntry, 0, limit)
	for start := 0; start < len(entries); {
		key := recoveryResumeGroupKey(entries[start].session, byID)
		end := start + 1
		for end < len(entries) && recoveryResumeGroupKey(entries[end].session, byID) == key {
			end++
		}
		if len(out) > 0 && len(out)+(end-start) > limit {
			break
		}
		out = append(out, entries[start:end]...)
		start = end
		if len(out) >= limit {
			break
		}
	}
	return out
}

// newestResumeTarget returns the newest resumable conversation of a workspace
// across both stores. It backs --continue: like mostRecentSession it wants the
// chronologically newest conversation, not the picker's leaf-first family
// preference; migrated sources and engine mirrors defer to the legacy row
func newestResumeTarget(sessionDir string) (cliResumeTarget, bool) {
	scan := scanWorkspaceResume(context.Background(), sessionDir)
	var newestLegacy agent.SessionInfo
	if len(scan.legacy) > 0 {
		// ListSessions is newest-first, so the first visible row wins.
		newestLegacy = scan.legacy[0]
	}
	var newestCanonical resumeEntry
	if len(scan.canonical) > 0 {
		newestCanonical = scan.canonical[0]
	}
	switch {
	case newestLegacy.Path == "" && newestCanonical.target.empty():
		return cliResumeTarget{}, false
	case newestLegacy.Path == "":
		return newestCanonical.target, true
	case newestCanonical.target.empty():
		return cliResumeTarget{path: newestLegacy.Path}, true
	case newestCanonical.session.ModTime.After(newestLegacy.ModTime):
		return newestCanonical.target, true
	default:
		return cliResumeTarget{path: newestLegacy.Path}, true
	}
}
