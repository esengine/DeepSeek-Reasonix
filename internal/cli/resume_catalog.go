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
// row shape: the v4 directory stands in for the transcript path, the title
// (or first-user-message preview) stands in for the preview text, and the
// event-log revision time drives recency ordering.
func canonicalResumeDisplayInfo(info session.SessionInfo) agent.SessionInfo {
	preview := strings.TrimSpace(info.Title)
	if preview == "" {
		preview = strings.TrimSpace(info.Preview)
	}
	return agent.SessionInfo{
		Path: info.Path, Preview: preview, Turns: info.Turns,
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

// legacyResumeRows returns the workspace's legacy transcript rows with
// migrated sources removed, ordered newest first. Resuming a frozen source
// reuses its canonical target, so listing it beside that target only offers
// the same conversation twice under two identities.
func legacyResumeRows(sessionDir string) []agent.SessionInfo {
	if sessionDir == "" {
		return nil
	}
	sessions, err := agent.ListSessions(sessionDir)
	if err != nil {
		return nil
	}
	migrated, _ := migratedLegacyIndex(sessionDir, canonicalResumeEntries(context.Background(), sessionDir))
	rows := make([]agent.SessionInfo, 0, len(sessions))
	for _, info := range sessions {
		if _, hidden := migrated[agent.CanonicalSessionPath(info.Path)]; hidden {
			continue
		}
		rows = append(rows, info)
	}
	return rows
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
	canonical := canonicalResumeEntries(context.Background(), sessionDir)
	migrated, _ := migratedLegacyIndex(sessionDir, canonical)
	sessions, err := agent.ListSessions(sessionDir)
	if err != nil {
		sessions = nil
	}
	legacy := make([]agent.SessionInfo, 0, len(sessions))
	legacyIDs := make(map[string]struct{}, len(sessions))
	for _, info := range sessions {
		if _, hidden := migrated[agent.CanonicalSessionPath(info.Path)]; hidden {
			continue
		}
		legacy = append(legacy, info)
		legacyIDs[agent.BranchID(info.Path)] = struct{}{}
	}
	visibleCanonical := make([]resumeEntry, 0, len(canonical))
	for _, entry := range canonical {
		// The engine mirrors an in-flight legacy transcript into a final-format
		// event log whose session id is the legacy branch id. That mirror is
		// plumbing, not a second conversation: fold it into the legacy row.
		if _, mirrored := legacyIDs[entry.target.ref.SessionID]; mirrored {
			continue
		}
		visibleCanonical = append(visibleCanonical, entry)
	}
	return mergeResumeStores(orderResumeSessions(legacy), visibleCanonical, limit)
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
// they belong beside.
func newestResumeTarget(sessionDir string) (cliResumeTarget, bool) {
	canonical := canonicalResumeEntries(context.Background(), sessionDir)
	bySource, _ := migratedLegacyIndex(sessionDir, canonical)
	allLegacy, err := agent.ListSessions(sessionDir)
	if err != nil {
		allLegacy = nil
	}
	legacyIDs := make(map[string]struct{}, len(allLegacy))
	newestLegacy := agent.SessionInfo{}
	for _, info := range allLegacy {
		legacyIDs[agent.BranchID(info.Path)] = struct{}{}
		if _, hidden := bySource[agent.CanonicalSessionPath(info.Path)]; hidden {
			continue
		}
		// ListSessions is newest-first, so the first visible row wins.
		if newestLegacy.Path == "" {
			newestLegacy = info
		}
	}
	var newestCanonical resumeEntry
	for _, entry := range canonical {
		if _, mirrored := legacyIDs[entry.target.ref.SessionID]; mirrored {
			continue
		}
		newestCanonical = entry
		break
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

// resolveCanonicalSessionQuery matches a --resume query against final-format
// catalog rows: exact session id, unique id prefix, then title/preview text.
func resolveCanonicalSessionQuery(sessionDir, query string) (cliResumeTarget, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return cliResumeTarget{}, false
	}
	entries := canonicalResumeEntries(context.Background(), sessionDir)
	lower := strings.ToLower(query)
	var byPrefix, byText []cliResumeTarget
	for _, entry := range entries {
		id := entry.target.ref.SessionID
		if id == query {
			return entry.target, true
		}
		if strings.HasPrefix(id, query) {
			byPrefix = append(byPrefix, entry.target)
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{id, entry.session.Preview}, "\n"))
		if strings.Contains(haystack, lower) {
			byText = append(byText, entry.target)
		}
	}
	if len(byPrefix) == 1 {
		return byPrefix[0], true
	}
	if len(byText) == 1 {
		return byText[0], true
	}
	return cliResumeTarget{}, false
}
