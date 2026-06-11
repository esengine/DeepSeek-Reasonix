package agent

import (
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

func migrateLegacySessions(srcDir, globalDest, marker string, projectDir func(string) string) (int, error) {
	if strings.TrimSpace(marker) == "" {
		marker = legacyImportMarker
	}
	if importMarkerExists(globalDest, marker) {
		return 0, nil
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, nil
	}
	imported := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".events.jsonl") {
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
		srcInfo, _ := e.Info()
		if destDir != globalDest && moveFlatImport(filepath.Join(globalDest, base+".jsonl"), dest, srcInfo) {
			recordImportedTitle(destDir, base, meta.Summary)
			imported++
			continue
		}
		msgs, err := reconstructSession(filepath.Join(srcDir, name))
		if err != nil || len(msgs) == 0 {
			continue
		}
		s := &Session{Messages: msgs}
		if err := s.Save(dest); err != nil {
			return imported, err
		}
		if srcInfo != nil {
			_ = os.Chtimes(dest, srcInfo.ModTime(), srcInfo.ModTime()) // preserve resume ordering
		}
		recordImportedTitle(destDir, base, meta.Summary)
		imported++
	}
	// Also stamp the flat markers so a downgrade to an older build doesn't
	// re-run the flat import over routed sessions.
	writeImportMarkers(globalDest, marker, legacyImportMarker, legacyEventsHomeImportMarker, legacyEventsConfigImportMarker)
	return imported, nil
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
