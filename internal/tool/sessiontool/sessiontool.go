// Package sessiontool provides list_sessions and read_session tools that let
// the AI discover and read past conversation sessions, enabling cross-session
// AI context sharing. The tools reuse agent.ListSessionOrder, agent.LoadSession,
// and agent.IsCleanupPending — the same infrastructure used by the history
// tool and session picker — to avoid duplicating session-file logic.
package sessiontool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/textutil"
)

// ---- list_sessions tool -----------------------------------------------------

type listSessionsTool struct {
	sessionDir string
}

// NewListSessionsTool creates a tool that lists saved sessions.
func NewListSessionsTool(sessionDir string) *listSessionsTool {
	return &listSessionsTool{sessionDir: sessionDir}
}

func (t *listSessionsTool) Name() string   { return "list_sessions" }
func (t *listSessionsTool) ReadOnly() bool { return true }

func (t *listSessionsTool) Description() string {
	return "List saved AI conversation sessions. Returns timestamp, model, turn count, preview, and file for each visible session, newest first. Uses session metadata (branch sidecar timestamps). Use read_session to view a session's conversation."
}

func (t *listSessionsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (t *listSessionsTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	ordered, err := agent.ListSessionOrder(t.sessionDir)
	if err != nil {
		return "", fmt.Errorf("list_sessions: %w", err)
	}
	if len(ordered) == 0 {
		return "No sessions found.\n", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Saved Sessions (%d total)\n\n", len(ordered))
	b.WriteString("| # | Timestamp | Model | Turns | Preview | File\n")
	b.WriteString("|---|-----------|-------|-------|-----------------|-----\n")
	for i, s := range ordered {
		ts := s.LastActivityAt.Format("2006-01-02 15:04")
		model := modelFromPath(s.Path)
		preview, turns := agent.SessionPreview(s.Path)
		fmt.Fprintf(&b, "| %d | %s | %s | %d | %s | `%s`\n",
			i+1, ts, model, turns, preview, filepath.Base(s.Path))
	}
	b.WriteString("\nUse `read_session` with the filename under \"File\" to view the session.\n")
	return b.String(), nil
}

// ---- read_session tool ------------------------------------------------------

type readSessionTool struct {
	sessionDir string
}

// NewReadSessionTool creates a tool that reads saved sessions.
func NewReadSessionTool(sessionDir string) *readSessionTool {
	return &readSessionTool{sessionDir: sessionDir}
}

func (t *readSessionTool) Name() string   { return "read_session" }
func (t *readSessionTool) ReadOnly() bool { return true }

func (t *readSessionTool) Description() string {
	return `Read a saved AI conversation session by file name (e.g. "20260618-231556.000000000-gpt-4.jsonl"). Returns a bounded, privacy-safe view: each message truncated to 2000 runes, no reasoning content, no system prompts, no tool result content (opt-in via show_tool_results). Use list_sessions to discover available sessions.`
}

func (t *readSessionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
	"properties": {
		"session": {
			"type": "string",
			"description": "Session file name (e.g. \"20260618-231556.000000000-gpt-4.jsonl\") or full path. Use list_sessions to see available sessions."
		},
    "max_turns": {
      "type": "integer",
      "description": "Maximum user-assistant turns to return (default 50). 0 = no limit."
    },
    "show_tool_results": {
      "type": "boolean",
      "description": "When true, include tool result content (default false). Tool results may contain secrets, command output, environment data, or private file contents."
    }
  },
  "required": ["session"]
}`)
}

func (t *readSessionTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Session         string `json:"session"`
		MaxTurns        *int   `json:"max_turns"`
		ShowToolResults bool   `json:"show_tool_results"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("read_session: invalid args: %w", err)
	}
	if params.Session == "" {
		return "", fmt.Errorf("read_session: 'session' argument is required")
	}

	sessionPath := params.Session
	// If it's just a filename (no path separator), resolve relative to sessionDir
	if !strings.Contains(sessionPath, string(filepath.Separator)) && !strings.Contains(sessionPath, "/") {
		sessionPath = filepath.Join(t.sessionDir, sessionPath)
	}
	// Guard against path traversal
	sessionPath = filepath.Clean(sessionPath)
	dir := filepath.Clean(t.sessionDir)
	if !strings.HasPrefix(sessionPath, dir+string(filepath.Separator)) && sessionPath != dir {
		return "", fmt.Errorf("read_session: path %q is outside the session directory", params.Session)
	}

	// Reject cleanup-pending sessions (reuses agent.IsCleanupPending directly).
	if agent.IsCleanupPending(sessionPath) {
		return "", fmt.Errorf("read_session: session %q is pending cleanup", filepath.Base(sessionPath))
	}

	// Reuse agent.LoadSession for JSONL decoding.
	ses, err := agent.LoadSession(sessionPath)
	if err != nil {
		return "", fmt.Errorf("read_session: %w", err)
	}
	msgs := ses.Snapshot()
	if len(msgs) == 0 {
		return "Session is empty.\n", nil
	}

	// Parse max_turns: default 50, 0 means no limit.
	maxTurns := 50
	if params.MaxTurns != nil {
		if *params.MaxTurns == 0 {
			maxTurns = 0
		} else if *params.MaxTurns > 0 {
			maxTurns = *params.MaxTurns
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Session: %s\n", filepath.Base(sessionPath))

	turnCount := 0
loop:
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleSystem:
			// System prompts excluded for privacy (matching history tool).

		case provider.RoleUser:
			turnCount++
			if maxTurns > 0 && turnCount > maxTurns {
				b.WriteString("... (truncated, use max_turns to increase limit)\n")
				break loop
			}
			fmt.Fprintf(&b, "## User (turn %d)\n", turnCount)
			b.WriteString(truncateRunes(m.Content, 2000))
			b.WriteString("\n\n")

		case provider.RoleAssistant:
			if m.Content != "" {
				fmt.Fprintf(&b, "## Assistant (turn %d)\n", max(turnCount, 1))
				b.WriteString(truncateRunes(m.Content, 2000))
				b.WriteString("\n\n")
			}
			if len(m.ToolCalls) > 0 {
				b.WriteString("### Tool Calls\n\n")
				for _, tc := range m.ToolCalls {
					fmt.Fprintf(&b, "- `%s(%s)`\n", tc.Name, truncateRunes(string(tc.Arguments), 1200))
				}
				b.WriteString("\n")
			}

		case provider.RoleTool:
			fmt.Fprintf(&b, "### Tool Result: %s\n\n", m.Name)
			if params.ShowToolResults && m.Content != "" {
				b.WriteString(truncateRunes(m.Content, 2000))
				b.WriteString("\n\n")
			}
		}
	}

	return b.String(), nil
}

// ---- helpers ----------------------------------------------------------------

// truncateRunes preserves the historical name but truncates by grapheme
// clusters so previews do not split combined emoji or other visible characters.
func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	return textutil.TruncateGraphemes(s, max, "...")
}

// modelFromPath extracts the model name from a session file path.
// Filename format: "20060102-150405.000000000-model-name.jsonl"
func modelFromPath(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".jsonl")
	firstDash := strings.Index(name, "-")
	if firstDash < 0 {
		return "(unknown)"
	}
	rest := name[firstDash+1:]
	secondDash := strings.Index(rest, "-")
	if secondDash < 0 {
		return rest
	}
	return rest[secondDash+1:]
}
