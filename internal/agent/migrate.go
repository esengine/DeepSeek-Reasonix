package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// legacyEvent is the subset of the v0.x typed event stream (<name>.events.jsonl)
// needed to rebuild the conversation: user input, assistant turns (text + tool
// calls), and tool results. All other event types (UI, plan, checkpoint, …) are
// presentation and carry no message state.
type legacyEvent struct {
	Type             string           `json:"type"`
	Text             string           `json:"text"`             // user.message
	Content          string           `json:"content"`          // model.final
	ReasoningContent string           `json:"reasoningContent"` // model.final
	ToolCalls        []legacyToolCall `json:"toolCalls"`        // model.final
	CallID           string           `json:"callId"`           // tool.result
	Output           string           `json:"output"`           // tool.result
}

type legacyToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// legacyImportMarker, once present in the v1+ session dir, records that the
// one-time v0.x import has already run — so a session the user deletes after it
// was imported doesn't reappear on the next launch.
const legacyImportMarker = ".legacy-imported"
const legacyEventsHomeImportMarker = ".legacy-imported.v0-events-home"
const legacyEventsConfigImportMarker = ".legacy-imported.v0-events-config"

// Routed markers are independent of the flat-import ones above: the routed pass
// must run once even for users whose flat import already completed, because it
// re-homes sessions the flat import left in the global dir (#3937).
const legacyRoutedHomeImportMarker = ".legacy-imported.v2-routed"
const legacyRoutedConfigImportMarker = ".legacy-imported.v0-events-config.v2-routed"

// legacyJsonlPassMarker gates the v3 pass that imports .jsonl files already in
// message format (no .events.jsonl counterpart). It is independent of all
// earlier markers so existing upgraders whose events-only passes completed still
// get their .jsonl-only sessions imported.
const legacyJsonlPassMarker = ".legacy-imported.v3-jsonl"

// legacyMeta is the v0.x sidecar (<name>.meta.json): the workspace the session
// belonged to and the generated summary used as its display title.
type legacyMeta struct {
	Workspace string `json:"workspace"`
	Summary   string `json:"summary"`
}

// MigrateLegacySessions imports v0.x event-log sessions (<name>.events.jsonl under
// srcDir) into the v1+ message-log format, routing each session into the
// per-workspace dir its sidecar meta names (via projectDir) so the desktop
// sidebar can see it; sessions without a live workspace land in globalDest. It
// also re-homes sessions a previous flat import left in globalDest. Runs once —
// guarded by a marker in globalDest — and never modifies the legacy files.
// Returns the count imported (including re-homed).
func MigrateLegacySessions(srcDir, globalDest string, projectDir func(workspaceRoot string) string) (int, error) {
	return migrateLegacySessions(srcDir, globalDest, legacyRoutedHomeImportMarker, projectDir)
}

// MigrateLegacySessionsFromConfigDir imports v0.x event-log sessions found in
// the current user config session directory. It uses an independent marker so a
// previous ~/.reasonix import marker cannot hide sessions from a redirected
// config root on Windows/macOS.
func MigrateLegacySessionsFromConfigDir(srcDir, globalDest string, projectDir func(workspaceRoot string) string) (int, error) {
	return migrateLegacySessions(srcDir, globalDest, legacyRoutedConfigImportMarker, projectDir)
}

// migrateLegacySessions is the pipeline orchestrator. It reads the source
// directory, builds shared context, and runs each registered migration pass in
// order. Marker stamping happens only when all main passes complete without
// artifact failures.
func migrateLegacySessions(srcDir, globalDest, marker string, projectDir func(string) string) (int, error) {
	if strings.TrimSpace(marker) == "" {
		marker = legacyImportMarker
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, nil
	}

	// Build the set of base names that have a .events.jsonl so the .jsonl-only
	// pass can skip sessions that will be (or were) handled by event reconstruction.
	hasEvents := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".events.jsonl") {
			hasEvents[strings.TrimSuffix(name, ".events.jsonl")] = true
		}
	}

	ctx := &MigrateContext{
		SrcDir:     srcDir,
		GlobalDest: globalDest,
		ProjectDir: projectDir,
		Entries:    entries,
		HasEvents:  hasEvents,
		Marker:     marker,
	}

	imported := 0
	hadArtifactFailure := false

	for _, pass := range migrateRegistry {
		// Skip passes guarded by an existing marker (unless AlwaysRun).
		if !pass.AlwaysRun && pass.Marker != "" && importMarkerExists(globalDest, pass.Marker) {
			continue
		}
		// Also skip marker-guarded passes when the primary marker already exists
		// (the events/subdir passes are covered by the primary marker).
		if !pass.AlwaysRun && pass.Marker == "" && importMarkerExists(globalDest, marker) && importMarkerExists(globalDest, legacyJsonlPassMarker) {
			continue
		}
		n, artifactFail, err := pass.Run(ctx)
		imported += n
		if artifactFail {
			hadArtifactFailure = true
		}
		if err != nil {
			return imported, err
		}
	}

	// Stamp markers only when all main passes completed without artifact failures.
	if !hadArtifactFailure {
		writeImportMarkers(globalDest, marker, legacyImportMarker, legacyEventsHomeImportMarker, legacyEventsConfigImportMarker, legacyJsonlPassMarker)
	}
	return imported, nil
}

// importJsonlSessions copies .jsonl files that are already in message format
// (no .events.jsonl counterpart) from srcDir into their appropriate destination
// dirs. Returns the count imported and whether a related artifact copy failed.
func importJsonlSessions(entries []os.DirEntry, srcDir, globalDest string, hasEvents map[string]bool, projectDir func(string) string) (int, bool) {
	imported := 0
	hadArtifactFailure := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".events.jsonl") || strings.HasSuffix(name, ".jsonl.bak") {
			continue
		}
		base := strings.TrimSuffix(name, ".jsonl")
		if hasEvents[base] {
			continue // handled (or skipped) in the events pass
		}
		// Legacy subagent transcripts live under the subagents/ tree in the
		// current version and are only meaningful when accessed through their
		// parent session. Importing them as standalone sessions clutters the
		// history panel with partial, out-of-context conversations.
		if strings.HasPrefix(base, "subagent-") {
			continue
		}
		jsonlPath := filepath.Join(srcDir, name)
		if !isMessageFormat(jsonlPath) {
			continue
		}
		destDir, summary, copyBranchMeta := jsonlSessionDestDir(srcDir, jsonlPath, base, globalDest, projectDir)
		dest := filepath.Join(destDir, base+".jsonl")
		if _, err := os.Stat(dest); err == nil {
			if copyBranchMeta {
				if err := copySubagentArtifacts(srcDir, destDir, base); err != nil {
					hadArtifactFailure = true
				}
			}
			continue
		}
		srcInfo, _ := e.Info()
		if err := transformAndCopyJsonl(jsonlPath, dest); err != nil {
			continue
		}
		if srcInfo != nil {
			_ = os.Chtimes(dest, srcInfo.ModTime(), srcInfo.ModTime())
		}
		if copyBranchMeta {
			copyBranchMetaSidecar(jsonlPath, dest)
			if err := copySubagentArtifacts(srcDir, destDir, base); err != nil {
				hadArtifactFailure = true
			}
		}
		recordImportedTitle(destDir, base, summary)
		imported++
	}
	return imported, hadArtifactFailure
}

func jsonlSessionDestDir(srcDir, srcPath, base, globalDest string, projectDir func(string) string) (string, string, bool) {
	if meta, ok, err := LoadBranchMeta(srcPath); err == nil && ok {
		summary := strings.TrimSpace(meta.TopicTitle)
		scope := meta.DefaultScope()
		if projectDir != nil && scope == "project" && meta.WorkspaceRoot != "" && dirExists(meta.WorkspaceRoot) {
			if d := projectDir(meta.WorkspaceRoot); d != "" {
				return d, summary, true
			}
		}
		// Explicit branch meta is newer than any stale v0.x sidecar. Preserve
		// global branch metadata, but do not carry a dead project scope into the
		// global directory when its workspace can no longer be resolved.
		if meta.Scope != "" {
			return globalDest, summary, scope == "global"
		}
	}
	meta := readLegacyMeta(srcDir, base)
	destDir := globalDest
	if projectDir != nil && meta.Workspace != "" && dirExists(meta.Workspace) {
		if d := projectDir(meta.Workspace); d != "" {
			destDir = d
		}
	}
	return destDir, meta.Summary, false
}

// migrateSubDirectory imports sessions from a project-scoped subdirectory
// within the legacy session dir. It walks the subdirectory for .events.jsonl and
// .jsonl files and imports them using the projectDir callback for routing.
func migrateSubDirectory(subDir, globalDest string, projectDir func(string) string) (int, error) {
	entries, err := os.ReadDir(subDir)
	if err != nil {
		return 0, nil
	}
	hasEvents := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".events.jsonl") {
			hasEvents[strings.TrimSuffix(name, ".events.jsonl")] = true
		}
	}
	imported := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		var base string
		var srcPath string
		reconstruct := false
		switch {
		case strings.HasSuffix(name, ".events.jsonl"):
			base = strings.TrimSuffix(name, ".events.jsonl")
			srcPath = filepath.Join(subDir, name)
			// Prefer .jsonl sidecar if it's newer.
			if jsonlPath := filepath.Join(subDir, base+".jsonl"); fileExists(jsonlPath) && isMessageFormat(jsonlPath) {
				eventsInfo, _ := e.Info()
				if jsonlInfo, err := os.Stat(jsonlPath); err == nil {
					if eventsInfo == nil || !jsonlInfo.ModTime().Before(eventsInfo.ModTime()) {
						srcPath = jsonlPath
						reconstruct = false
					} else {
						reconstruct = true
					}
				} else {
					reconstruct = true
				}
			} else {
				reconstruct = true
			}
		case strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".events.jsonl") && !strings.HasSuffix(name, ".jsonl.bak"):
			base = strings.TrimSuffix(name, ".jsonl")
			if hasEvents[base] {
				continue // handled by the events branch above
			}
			srcPath = filepath.Join(subDir, name)
			if !isMessageFormat(srcPath) {
				continue
			}
			// reconstruct stays false
		default:
			continue
		}
		meta := readLegacyMeta(subDir, base)
		destDir := globalDest
		if projectDir != nil && meta.Workspace != "" && dirExists(meta.Workspace) {
			if d := projectDir(meta.Workspace); d != "" {
				destDir = d
			}
		}
		dest := filepath.Join(destDir, base+".jsonl")
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		srcInfo, _ := e.Info()
		if reconstruct {
			msgs, err := reconstructSession(srcPath)
			if err != nil || len(msgs) == 0 {
				continue
			}
			s := &Session{Messages: msgs}
			if err := s.Save(dest); err != nil {
				return imported, err
			}
		} else {
			if err := transformAndCopyJsonl(srcPath, dest); err != nil {
				continue
			}
		}
		if srcInfo != nil {
			_ = os.Chtimes(dest, srcInfo.ModTime(), srcInfo.ModTime())
		}
		recordImportedTitle(destDir, base, meta.Summary)
		imported++
	}
	return imported, nil
}
