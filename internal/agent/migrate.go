package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/provider"
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

// MigrateLegacySessionsFromExplicitDir imports sessions from a user-selected
// legacy directory. It uses a source-specific marker so a previous default
// /migrate pass cannot hide later imports from a custom Windows install/data
// directory.
func MigrateLegacySessionsFromExplicitDir(srcDir, globalDest string, projectDir func(workspaceRoot string) string) (int, error) {
	marker := explicitLegacyImportMarker(srcDir)
	return migrateLegacySessionsWithMarkers(srcDir, globalDest, marker, marker+".jsonl", projectDir)
}

func explicitLegacyImportMarker(srcDir string) string {
	key := strings.TrimSpace(srcDir)
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	sum := sha256.Sum256([]byte(filepath.Clean(key)))
	return ".legacy-imported.explicit." + hex.EncodeToString(sum[:8])
}

func migrateLegacySessions(srcDir, globalDest, marker string, projectDir func(string) string) (int, error) {
	return migrateLegacySessionsWithMarkers(srcDir, globalDest, marker, legacyJsonlPassMarker, projectDir)
}

func migrateLegacySessionsWithMarkers(srcDir, globalDest, marker, jsonlMarker string, projectDir func(string) string) (int, error) {
	if strings.TrimSpace(marker) == "" {
		marker = legacyImportMarker
	}
	if strings.TrimSpace(jsonlMarker) == "" {
		jsonlMarker = legacyJsonlPassMarker
	}
	// Gate on both the routed marker AND the jsonl marker: an existing upgrader
	// whose events pass already stamped the routed marker must still reach the
	// .jsonl-only / subdir passes below (Pass 1 is idempotent via dest checks).
	if importMarkerExists(globalDest, marker) && importMarkerExists(globalDest, jsonlMarker) {
		// The one-time full passes already ran for this source. Still run the
		// bounded re-home pass: a user who downgrades to a pre-routing build
		// (which writes every session to the flat dir) and then upgrades again
		// leaves project sessions stranded in the flat dir that the marker would
		// otherwise hide forever (#4666). The pass is watermarked by the marker
		// mtime so a session the user imported and then deleted is not revived.
		return rehomeStrandedSessions(srcDir, globalDest, marker, projectDir)
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
		if !e.IsDir() && strings.HasSuffix(name, ".events.jsonl") && !isNativeSessionEventLog(filepath.Join(srcDir, name)) {
			hasEvents[strings.TrimSuffix(name, ".events.jsonl")] = true
		}
	}

	imported := 0
	hadArtifactFailure := false

	// Pass 1 — event-log sessions (*.events.jsonl). When a same-named .jsonl
	// exists in the source with a modification time >= the event log's, prefer
	// the .jsonl directly (it is already in the native message format).
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".events.jsonl") {
			continue
		}
		if isNativeSessionEventLog(filepath.Join(srcDir, name)) {
			continue
		}
		base := strings.TrimSuffix(name, ".events.jsonl")
		meta := readLegacyMeta(srcDir, base)
		destDir := globalDest
		if projectDir != nil && meta.Workspace != "" && dirExists(meta.Workspace) {
			if d := projectDir(meta.Workspace); d != "" {
				destDir = d
			}
		}
		dest := filepath.Join(destDir, base+".jsonl")
		if _, err := os.Stat(dest); err == nil {
			continue // already imported, or a v1+ session of the same name
		}
		eventsInfo, _ := e.Info()
		if destDir != globalDest && moveFlatImport(filepath.Join(globalDest, base+".jsonl"), dest, eventsInfo) {
			recordImportedTitle(destDir, base, meta.Summary)
			imported++
			continue
		}

		// If a .jsonl sidecar exists and is >= the event log's mtime, copy it
		// directly — the TS version wrote the native format alongside or after
		// the event log, so the .jsonl is the canonical record.
		jsonlPath := filepath.Join(srcDir, base+".jsonl")
		if jsonlInfo, err := os.Stat(jsonlPath); err == nil && isMessageFormat(jsonlPath) {
			if eventsInfo == nil || !jsonlInfo.ModTime().Before(eventsInfo.ModTime()) {
				if err := transformAndCopyJsonl(jsonlPath, dest); err == nil {
					if eventsInfo != nil {
						_ = os.Chtimes(dest, eventsInfo.ModTime(), eventsInfo.ModTime())
					}
					recordImportedTitle(destDir, base, meta.Summary)
					imported++
					continue
				}
			}
		}

		msgs, err := reconstructSession(filepath.Join(srcDir, name))
		if err != nil || len(msgs) == 0 {
			continue
		}
		s := &Session{Messages: msgs}
		if err := s.Save(dest); err != nil {
			return imported, err
		}
		if eventsInfo != nil {
			_ = os.Chtimes(dest, eventsInfo.ModTime(), eventsInfo.ModTime()) // preserve resume ordering
		}
		recordImportedTitle(destDir, base, meta.Summary)
		imported++
	}

	// Pass 2 — message-format .jsonl files without a .events.jsonl counterpart.
	// These are sessions the TS version wrote directly in the v1+ format (ACP,
	// desktop, subagent, and later-version chat sessions). The pass is gated by
	// its own marker so existing upgraders whose events passes completed still
	// get their .jsonl-only sessions imported.
	if !importMarkerExists(globalDest, jsonlMarker) {
		n, failed := importJsonlSessions(entries, srcDir, globalDest, hasEvents, projectDir)
		imported += n
		hadArtifactFailure = hadArtifactFailure || failed

		// .jsonl.bak recovery: when the .jsonl was lost but a backup remains.
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".jsonl.bak") {
				continue
			}
			base := strings.TrimSuffix(name, ".jsonl.bak")
			if hasEvents[base] {
				continue
			}
			jsonlName := base + ".jsonl"
			if _, err := os.Stat(filepath.Join(srcDir, jsonlName)); err == nil {
				continue // .jsonl exists, prefer it
			}
			meta := readLegacyMeta(srcDir, base)
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
			bakPath := filepath.Join(srcDir, name)
			if !isMessageFormat(bakPath) {
				continue
			}
			srcInfo, _ := e.Info()
			if err := transformAndCopyJsonl(bakPath, dest); err != nil {
				continue
			}
			if srcInfo != nil {
				_ = os.Chtimes(dest, srcInfo.ModTime(), srcInfo.ModTime())
			}
			recordImportedTitle(destDir, base, meta.Summary)
			imported++
		}
	}

	// Pass 3 — recurse into subdirectories that look like project session dirs
	// (e.g. Users_Yuki_git_polytone-audio-engine/ under ~/.reasonix/sessions/).
	// The TS version nested project-scoped sessions under a workspace slug.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "subagents" {
			continue
		}
		subDir := filepath.Join(srcDir, e.Name())
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		hasSessions := false
		for _, se := range subEntries {
			sn := se.Name()
			if !se.IsDir() && (strings.HasSuffix(sn, ".jsonl") || strings.HasSuffix(sn, ".events.jsonl")) {
				hasSessions = true
				break
			}
		}
		if !hasSessions {
			continue
		}
		n, err := migrateSubDirectory(subDir, globalDest, projectDir)
		if err != nil {
			continue
		}
		imported += n
	}

	// Also stamp the flat markers so a downgrade to an older build doesn't
	// re-run the flat import over routed sessions.
	if hadArtifactFailure {
		return imported, nil
	}
	writeImportMarkers(globalDest, marker, legacyImportMarker, legacyEventsHomeImportMarker, legacyEventsConfigImportMarker, jsonlMarker)
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
		if isNativeSessionEventLog(SessionEventLogPath(jsonlPath)) {
			if err := saveNativeSessionCopy(jsonlPath, dest); err != nil {
				continue
			}
		} else if err := transformAndCopyJsonl(jsonlPath, dest); err != nil {
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
		if !e.IsDir() && strings.HasSuffix(name, ".events.jsonl") && !isNativeSessionEventLog(filepath.Join(subDir, name)) {
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
			if isNativeSessionEventLog(filepath.Join(subDir, name)) {
				continue
			}
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
		} else if isNativeSessionEventLog(SessionEventLogPath(srcPath)) {
			if err := saveNativeSessionCopy(srcPath, dest); err != nil {
				continue
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

// isMessageFormat returns true when path's first non-whitespace bytes look like
// a JSON object with a "role" key — i.e. the v1+ message format — as opposed to
// the legacy event-log format whose first key is "id".
func isMessageFormat(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [64]byte
	n, _ := f.Read(buf[:])
	s := strings.TrimLeft(string(buf[:n]), " \t\r\n")
	return strings.HasPrefix(s, `{"role":`)
}

// isNativeSessionEventLog reports whether the file at an .events.jsonl path is
// a native session event log (as opposed to a legacy v0.x event transcript
// that happens to share the suffix).
func isNativeSessionEventLog(path string) bool {
	sessionPath := strings.TrimSuffix(path, ".events.jsonl") + ".jsonl"
	probe, err := probeSessionEventLog(sessionPath)
	return err == nil && probe.native && probe.size > 0
}

func saveNativeSessionCopy(src, dst string) error {
	session, err := LoadSession(src)
	if err != nil {
		return err
	}
	return session.Save(dst)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// legacyAssistantMsg is the minimal JSON shape needed to detect and transform
// the legacy nested-function tool-call format into the flat format the Go
// version expects.
type legacyAssistantMsg struct {
	Role      string          `json:"role"`
	ToolCalls json.RawMessage `json:"tool_calls"`
}

// legacyToolCallObj matches the OpenAI-style tool call where name and
// arguments live under a "function" key.
type legacyToolCallObj struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// transformAndCopyJsonl copies src to dst, flattening any legacy nested-function
// tool calls into the flat name/arguments format the v1+ message format uses.
// Non-assistant messages and messages without tool_calls pass through unchanged.
func transformAndCopyJsonl(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".session.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpPath)
		}
	}()
	enc := json.NewEncoder(tmp)
	dec := json.NewDecoder(in)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// Malformed tail — keep what we've written so far.
			break
		}
		var m legacyAssistantMsg
		if err := json.Unmarshal(raw, &m); err != nil || m.Role != "assistant" || len(m.ToolCalls) == 0 {
			// Pass through unchanged (user, tool, or assistant without tool calls).
			if err := enc.Encode(raw); err != nil {
				return err
			}
			continue
		}
		// Try legacy nested-function format; if it doesn't match, pass through.
		var legacyCalls []legacyToolCallObj
		if err := json.Unmarshal(m.ToolCalls, &legacyCalls); err != nil || len(legacyCalls) == 0 {
			if err := enc.Encode(raw); err != nil {
				return err
			}
			continue
		}
		// Build flat-format tool calls.
		flatCalls := make([]provider.ToolCall, len(legacyCalls))
		for i, tc := range legacyCalls {
			flatCalls[i] = provider.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}
		}
		// Re-serialize the full message with flat tool_calls. We only modify
		// tool_calls; all other fields (content, reasoning_content, etc.) stay
		// as-is by round-tripping through a map.
		var full map[string]json.RawMessage
		if err := json.Unmarshal(raw, &full); err != nil {
			if err := enc.Encode(raw); err != nil {
				return err
			}
			continue
		}
		b, err := json.Marshal(flatCalls)
		if err != nil {
			if err := enc.Encode(raw); err != nil {
				return err
			}
			continue
		}
		full["tool_calls"] = b
		if err := enc.Encode(full); err != nil {
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	ok = true
	return nil
}

// readLegacyMeta loads the v0.x sidecar for a session; missing or corrupt
// sidecars yield the zero value (session routes to the global dir, untitled).
func readLegacyMeta(srcDir, base string) legacyMeta {
	var m legacyMeta
	b, err := os.ReadFile(filepath.Join(srcDir, base+".meta.json"))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	m.Workspace = strings.TrimSpace(m.Workspace)
	m.Summary = strings.TrimSpace(m.Summary)
	return m
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// moveFlatImport re-homes a session the flat import left in the global dir.
// The legacy event log's mtime was stamped onto the imported file, so a match
// identifies it; a same-named native v1+ session never matches and stays put.
func moveFlatImport(oldPath, newPath string, srcInfo os.FileInfo) bool {
	if srcInfo == nil {
		return false
	}
	info, err := os.Stat(oldPath)
	if err != nil {
		return false
	}
	d := info.ModTime().Sub(srcInfo.ModTime())
	if d < -2*time.Second || d > 2*time.Second {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return false
	}
	return os.Rename(oldPath, newPath) == nil
}

// recordImportedTitle stores the legacy summary as the session's display title
// in the dir's .titles.json — the same map the desktop sidebar reads
// (desktop/sessions.go). Existing titles are never overwritten.
func recordImportedTitle(destDir, base, summary string) {
	if summary == "" {
		return
	}
	path := filepath.Join(destDir, ".titles.json")
	titles := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &titles)
	}
	key := base + ".jsonl"
	if titles[key] != "" {
		return
	}
	titles[key] = summary
	b, err := json.MarshalIndent(titles, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func importMarkerExists(destDir, marker string) bool {
	if strings.TrimSpace(destDir) == "" || strings.TrimSpace(marker) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(destDir, marker))
	return err == nil
}

func writeImportMarkers(destDir string, markers ...string) {
	if strings.TrimSpace(destDir) == "" {
		return
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return
	}
	seen := map[string]bool{}
	for _, marker := range markers {
		marker = strings.TrimSpace(marker)
		if marker == "" || seen[marker] {
			continue
		}
		seen[marker] = true
		_ = os.WriteFile(filepath.Join(destDir, marker), nil, 0o644)
	}
}

// rehomeStrandedSessions copies project-scoped sessions that were written into
// the flat global dir AFTER the one-time routing pass already ran — the
// signature of a user who downgraded to a pre-routing build (which writes every
// session to the flat dir regardless of workspace) and then upgraded again
// (#4666). Without this, the routing marker hides those sessions from the
// desktop sidebar forever, even though they are sitting in the flat dir.
//
// It is deliberately conservative:
//   - Only sessions whose mtime is newer than the marker (the last migration
//     watermark) are considered, so a session the user imported and then
//     deleted is never resurrected.
//   - Only sessions that explicitly name a still-existing workspace — via a v1+
//     branch-meta sidecar with scope=project, or a v0.x .meta.json — are moved.
//     Flat global sessions (CLI conversations, the desktop's global tab) carry
//     no workspace and are left untouched.
//   - It never modifies the source files; the destination is written via the
//     same transform-and-copy path the full passes use, and the branch-meta
//     sidecar is copied alongside so the sidebar shows the right title/topic.
//
// The marker mtime is advanced to now after a successful scan so the next boot
// does not re-walk the same files.
func rehomeStrandedSessions(srcDir, globalDest, marker string, projectDir func(string) string) (int, error) {
	if projectDir == nil {
		return 0, nil
	}
	markerPath := filepath.Join(globalDest, marker)
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		return 0, nil // no watermark to compare against — full passes own this dir
	}
	watermark := markerInfo.ModTime()

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, nil
	}
	imported := 0
	hadCopyFailure := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") ||
			strings.HasSuffix(name, ".events.jsonl") || strings.HasSuffix(name, ".jsonl.bak") {
			continue
		}
		base := strings.TrimSuffix(name, ".jsonl")
		if strings.HasPrefix(base, "subagent-") {
			continue // surfaced only through their parent session
		}
		info, ierr := e.Info()
		if ierr != nil || !info.ModTime().After(watermark) {
			continue // written before the last migration — not a downgrade straggler
		}
		srcPath := filepath.Join(srcDir, name)
		if !isMessageFormat(srcPath) {
			continue
		}
		destDir, summary := strandedSessionDestDir(srcDir, srcPath, base, projectDir)
		if destDir == "" || sameDirPath(destDir, globalDest) {
			continue // global session, or no live workspace — leave it in the flat dir
		}
		dest := filepath.Join(destDir, name)
		if _, err := os.Stat(dest); err == nil {
			if err := copySubagentArtifacts(srcDir, destDir, base); err != nil {
				hadCopyFailure = true
			}
			continue // already routed on a previous boot
		}
		if err := transformAndCopyJsonl(srcPath, dest); err != nil {
			hadCopyFailure = true
			continue
		}
		_ = os.Chtimes(dest, info.ModTime(), info.ModTime()) // preserve resume ordering
		copyBranchMetaSidecar(srcPath, dest)
		if err := copySubagentArtifacts(srcDir, destDir, base); err != nil {
			hadCopyFailure = true
		}
		recordImportedTitle(destDir, base, summary)
		imported++
	}
	// Advance the watermark so the next boot starts from here, unless a matched
	// project session failed to copy and still needs a retry.
	if !hadCopyFailure {
		now := time.Now()
		_ = os.Chtimes(markerPath, now, now)
	}
	return imported, nil
}

// strandedSessionDestDir resolves the per-project session dir a flat-dir session
// belongs to, preferring the v1+ branch-meta sidecar and falling back to the
// v0.x .meta.json. It returns "" when the session is global or names a workspace
// that no longer exists on disk. The second return is the display summary, if any.
func strandedSessionDestDir(srcDir, srcPath, base string, projectDir func(string) string) (string, string) {
	if meta, ok, err := LoadBranchMeta(srcPath); err == nil && ok {
		if meta.DefaultScope() == "project" && meta.WorkspaceRoot != "" && dirExists(meta.WorkspaceRoot) {
			if d := projectDir(meta.WorkspaceRoot); d != "" {
				return d, strings.TrimSpace(meta.TopicTitle)
			}
		}
		// A branch sidecar that explicitly marks the session global wins over a
		// stale v0.x sidecar of the same name.
		if meta.Scope != "" {
			return "", ""
		}
	}
	legacy := readLegacyMeta(srcDir, base)
	if legacy.Workspace != "" && dirExists(legacy.Workspace) {
		if d := projectDir(legacy.Workspace); d != "" {
			return d, legacy.Summary
		}
	}
	return "", ""
}

// copyBranchMetaSidecar copies <src>.meta to <dst>.meta when present so the
// desktop sidebar keeps the session's title, topic, and tree position. Best
// effort: a missing or unreadable sidecar just means the session shows with a
// generated title.
func copyBranchMetaSidecar(srcPath, dstPath string) {
	b, err := os.ReadFile(BranchMetaPath(srcPath))
	if err != nil {
		return
	}
	dstMeta := BranchMetaPath(dstPath)
	if err := os.MkdirAll(filepath.Dir(dstMeta), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(dstMeta), ".branch.*.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, dstMeta); err != nil {
		os.Remove(tmpPath)
	}
}

func copySubagentArtifacts(srcSessionDir, dstSessionDir, parentSession string) error {
	if sameDirPath(srcSessionDir, dstSessionDir) {
		return nil
	}
	artifacts, err := ListSubagentsByParent(srcSessionDir, parentSession)
	if err != nil {
		return err
	}
	var errs []error
	dstSubagentDir := filepath.Join(dstSessionDir, "subagents")
	for _, artifact := range artifacts {
		for _, src := range []string{artifact.SessionPath, artifact.MetaPath} {
			if err := copyFileIfExists(src, filepath.Join(dstSubagentDir, filepath.Base(src))); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func copyFileIfExists(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".subagent.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return err
	}
	_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

// sameDirPath reports whether two directory paths resolve to the same location.
func sameDirPath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	if aa, err := filepath.Abs(ca); err == nil {
		if bb, err := filepath.Abs(cb); err == nil {
			return aa == bb
		}
	}
	return false
}

// reconstructSession folds the chronological event stream into the provider
// message sequence. Tool results inherit their tool name from the assistant turn
// that issued the call (the v0.x result event carries only the call id).
func reconstructSession(path string) ([]provider.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []provider.Message
	toolName := map[string]string{}
	dec := json.NewDecoder(f)
	for {
		var e legacyEvent
		if err := dec.Decode(&e); err != nil {
			if !errors.Is(err, io.EOF) {
				return msgs, nil // malformed tail — keep what parsed cleanly
			}
			break
		}
		switch e.Type {
		case "user.message":
			if e.Text != "" {
				msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: e.Text})
			}
		case "model.final":
			m := provider.Message{Role: provider.RoleAssistant, Content: e.Content, ReasoningContent: e.ReasoningContent}
			for _, tc := range e.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, provider.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
				toolName[tc.ID] = tc.Function.Name
			}
			msgs = append(msgs, m)
		case "tool.result":
			msgs = append(msgs, provider.Message{Role: provider.RoleTool, ToolCallID: e.CallID, Name: toolName[e.CallID], Content: e.Output})
		}
	}
	return msgs, nil
}
