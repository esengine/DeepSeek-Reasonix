package agent

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// MigrateLegacySessions imports v0.x event-log sessions (<name>.events.jsonl under
// srcDir) into the v1+ message-log format (<name>.jsonl under destDir), back-filling
// any whose .jsonl isn't already present. It runs once — guarded by a marker file in
// destDir — so it still imports when destDir already holds v1+ sessions (the old
// all-or-nothing skip hid a v0.x user's history the moment they opened v1, #2869)
// without re-importing on every launch. Never modifies the legacy files. Returns the
// count imported.
func MigrateLegacySessions(srcDir, destDir string) (int, error) {
	return migrateLegacySessions(srcDir, destDir, legacyEventsHomeImportMarker, true)
}

// MigrateLegacySessionsFromConfigDir imports v0.x event-log sessions found in
// the current user config session directory. It uses an independent marker so a
// previous ~/.reasonix import marker cannot hide sessions from a redirected
// config root on Windows/macOS.
func MigrateLegacySessionsFromConfigDir(srcDir, destDir string) (int, error) {
	return migrateLegacySessions(srcDir, destDir, legacyEventsConfigImportMarker, false)
}

func migrateLegacySessions(srcDir, destDir, marker string, honorLegacyMarker bool) (int, error) {
	if strings.TrimSpace(marker) == "" {
		marker = legacyImportMarker
	}
	if importMarkerExists(destDir, marker) {
		return 0, nil
	}
	if honorLegacyMarker && importMarkerExists(destDir, legacyImportMarker) {
		writeImportMarkers(destDir, marker)
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
		dest := filepath.Join(destDir, strings.TrimSuffix(name, ".events.jsonl")+".jsonl")
		if _, err := os.Stat(dest); err == nil {
			continue // already imported, or a v1+ session of the same name
		}
		msgs, err := reconstructSession(filepath.Join(srcDir, name))
		if err != nil || len(msgs) == 0 {
			continue
		}
		s := &Session{Messages: msgs}
		if err := s.Save(dest); err != nil {
			return imported, err
		}
		if info, err := e.Info(); err == nil {
			_ = os.Chtimes(dest, info.ModTime(), info.ModTime()) // preserve resume ordering
		}
		imported++
	}
	markers := []string{marker}
	if honorLegacyMarker {
		markers = append(markers, legacyImportMarker)
	}
	writeImportMarkers(destDir, markers...)
	return imported, nil
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
