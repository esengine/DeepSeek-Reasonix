package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/pty"
	"reasonix/internal/shellparse"
	"reasonix/internal/tool"
)

func init() {
	tool.RegisterBuiltin(ptyTool{})
}

type ptyTool struct{}

func (ptyTool) Name() string { return "pty" }

func (ptyTool) Description() string {
	return "Control a persistent interactive pseudo-terminal session (PTY) across turns. " +
		"Use for REPLs (Python, Node.js), interactive debuggers (gdb, lldb, pdb), maintaining shell state (cd, venv, export), or managing dev servers. " +
		"Actions: 'start' (spawn session), 'write' (send input/commands and auto-capture output), 'read' (read new unread output), 'list' (list active PTY sessions), 'close' (terminate session), 'resize' (change terminal size)."
}

func (ptyTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {
      "type": "string",
      "enum": ["start", "write", "read", "list", "close", "resize"],
      "description": "The PTY action to perform."
    },
    "session_id": {
      "type": "string",
      "description": "Identifier for the PTY session (defaults to 'default' if omitted)."
    },
    "command": {
      "type": "string",
      "description": "Initial shell or command to spawn on 'start' (e.g. 'bash', 'zsh', 'python3', 'node'). If omitted, uses user's default login shell."
    },
    "input": {
      "type": "string",
      "description": "Text or keystrokes to send on 'write'. Special keystrokes: \\n (enter), \\x03 (Ctrl+C), \\x04 (Ctrl+D), \\x1a (Ctrl+Z)."
    },
    "wait_ms": {
      "type": "integer",
      "description": "Milliseconds to wait for output to settle after writing (default: 500ms, 0 means non-blocking fire-and-forget, max: 10000ms)."
    },
    "max_bytes": {
      "type": "integer",
      "description": "Maximum bytes to read from buffer on 'read' (default: 32768, max: 131072)."
    },
    "cwd": {
      "type": "string",
      "description": "Working directory when starting the PTY session."
    },
    "cols": {
      "type": "integer",
      "description": "Terminal column width for 'resize' or 'start' (default: 120)."
    },
    "rows": {
      "type": "integer",
      "description": "Terminal row height for 'resize' or 'start' (default: 40)."
    }
  }
}`)
}

func (ptyTool) ReadOnly() bool { return false }

func (ptyTool) ProviderVisible(ctx context.Context) bool {
	_, ok := pty.FromContext(ctx)
	return ok
}

type ptyParams struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id,omitempty"`
	Command   string `json:"command,omitempty"`
	Input     string `json:"input,omitempty"`
	WaitMs    *int   `json:"wait_ms,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Cols      uint16 `json:"cols,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
}

// isDangerousPTYInput scans raw terminal input for catastrophic root wipe or fork-bomb patterns.
func isDangerousPTYInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false
	}
	// Direct string patterns for catastrophic root destruction
	dangerousPatterns := []string{
		"rm -rf /",
		"rm -fr /",
		"rm -rf /*",
		"rm -fr /*",
		"rm -rf --no-preserve-root",
		":(){ :|:& };:",
		"mkfs.",
		"> /dev/sda",
		"> /dev/nvme",
	}
	for _, pat := range dangerousPatterns {
		if strings.Contains(trimmed, pat) {
			return true
		}
	}
	// Parse fields if static
	if fields, _ := shellparse.StaticFields(trimmed); len(fields) >= 2 {
		if (fields[0] == "rm" || strings.HasSuffix(fields[0], "/rm")) &&
			(fields[1] == "-rf" || fields[1] == "-fr" || fields[1] == "-r") {
			for _, arg := range fields[2:] {
				if arg == "/" || arg == "/*" || arg == "~" || arg == "$HOME" {
					return true
				}
			}
		}
	}
	return false
}

func (ptyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p ptyParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid pty args: %w", err)
	}

	mgr, ok := pty.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("persistent pty is not available in this context")
	}

	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		sessionID = pty.DefaultSessionID
	}

	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case "start":
		sess, err := mgr.Start(ctx, pty.StartOptions{
			ID:      sessionID,
			Command: p.Command,
			Cwd:     p.Cwd,
			Cols:    p.Cols,
			Rows:    p.Rows,
		})
		if err != nil {
			return "", err
		}
		// Settle initial shell prompt
		initialOut, _ := sess.Write(ctx, "", 250*time.Millisecond)
		info := sess.Info()
		msg := fmt.Sprintf("Started PTY session %q (pid: %d, cmd: %s, cwd: %s)\n", info.ID, info.PID, info.Command, info.Cwd)
		if initialOut != "" {
			msg += "\n" + initialOut
		}
		return strings.TrimRight(msg, "\n"), nil

	case "write":
		// Security guard: verify input doesn't bypass system safety
		if isDangerousPTYInput(p.Input) {
			return "", fmt.Errorf("blocked: dangerous command execution denied by security policy: %s", strings.TrimSpace(p.Input))
		}

		sess, err := mgr.GetOrCreate(ctx, sessionID, p.Cwd)
		if err != nil {
			return "", err
		}

		waitBudget := 500 * time.Millisecond
		if p.WaitMs != nil {
			if *p.WaitMs <= 0 {
				waitBudget = 0
			} else if *p.WaitMs > 10000 {
				waitBudget = 10000 * time.Millisecond
			} else {
				waitBudget = time.Duration(*p.WaitMs) * time.Millisecond
			}
		}

		out, err := sess.Write(ctx, p.Input, waitBudget)
		if err != nil && !sess.IsRunning() {
			return fmt.Sprintf("Session exited (code %d).\n%s", sess.Info().ExitCode, out), nil
		}
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(out) == "" {
			if waitBudget == 0 {
				return "Input written (non-blocking).", nil
			}
			return "Input written (no new output).", nil
		}
		return out, nil

	case "read":
		sess, err := mgr.Get(sessionID)
		if err != nil {
			return "", err
		}
		out := sess.Read(p.MaxBytes)
		if strings.TrimSpace(out) == "" {
			if !sess.IsRunning() {
				return fmt.Sprintf("(session exited with code %d, no unread output)", sess.Info().ExitCode), nil
			}
			return "(no new output)", nil
		}
		return out, nil

	case "list":
		sessions := mgr.List()
		if len(sessions) == 0 {
			return "No active PTY sessions.", nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Active PTY Sessions (%d):\n", len(sessions)))
		for _, s := range sessions {
			status := "running"
			if !s.Running {
				status = fmt.Sprintf("exited(%d)", s.ExitCode)
			}
			b.WriteString(fmt.Sprintf("- [%s] id=%q pid=%d cmd=%q cwd=%q (size: %dx%d, uptime: %s)\n",
				status, s.ID, s.PID, s.Command, s.Cwd, s.Cols, s.Rows, time.Since(s.StartedAt).Round(time.Second)))
		}
		return strings.TrimRight(b.String(), "\n"), nil

	case "close":
		if err := mgr.Close(sessionID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Closed PTY session %q.", sessionID), nil

	case "resize":
		cols := p.Cols
		if cols == 0 {
			cols = pty.DefaultTerminalCols
		}
		rows := p.Rows
		if rows == 0 {
			rows = pty.DefaultTerminalRows
		}
		if err := mgr.Resize(sessionID, cols, rows); err != nil {
			return "", err
		}
		return fmt.Sprintf("Resized PTY session %q to %dx%d.", sessionID, cols, rows), nil

	default:
		return "", fmt.Errorf("unknown pty action %q (supported: start, write, read, list, close, resize)", p.Action)
	}
}
