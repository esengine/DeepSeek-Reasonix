package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(systemController{}) }

// systemController provides OS-level actions: open a URL, run a whitelisted
// application, or create a file.  roots and workDir are populated at runtime
// by the composition root for the create_file action; the zero value (registered
// at init) is unconfined.
type systemController struct {
	roots   []string
	workDir string
}

func (systemController) Name() string { return "system_control" }

func (systemController) Description() string {
	return `Control the local system. Supports three actions:
  • open_url    — Open a URL in the default browser.
  • run_command — Start a whitelisted application (e.g. notepad, code, chrome).
  • create_file — Create a new file with content (refuses to overwrite).`
}

func (systemController) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["open_url", "run_command", "create_file"],
      "description": "Which system action to perform."
    },
    "url": {
      "type": "string",
      "description": "URL to open (only for action=open_url)."
    },
    "cmd": {
      "type": "string",
      "description": "Application name or path to run (only for action=run_command). Whitelisted: notepad, calc, mspaint, code, code-insiders, notion, obsidian, chrome, firefox, msedge, word, excel, powerpoint."
    },
    "path": {
      "type": "string",
      "description": "Target file path (only for action=create_file)."
    },
    "content": {
      "type": "string",
      "description": "Content to write (only for action=create_file)."
    }
  },
  "required": ["action"]
}`)
}

func (systemController) ReadOnly() bool { return false }

func (s systemController) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Action  string `json:"action"`
		URL     string `json:"url,omitempty"`
		Cmd     string `json:"cmd,omitempty"`
		Path    string `json:"path,omitempty"`
		Content string `json:"content,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	switch p.Action {
	case "open_url":
		return s.openURL(ctx, p.URL)
	case "run_command":
		return s.runCommand(ctx, p.Cmd)
	case "create_file":
		return s.createFile(ctx, p.Path, p.Content)
	default:
		return "", fmt.Errorf("unknown action %q", p.Action)
	}
}

// ---------------------------------------------------------------------------
// open_url
// ---------------------------------------------------------------------------

func (systemController) openURL(ctx context.Context, url string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("url is required for action=open_url")
	}
	// Basic sanity: reject javascript: / file: / vbscript: to keep the model
	// from crafting an XSS-like vector.
	low := strings.ToLower(url)
	if strings.HasPrefix(low, "javascript:") ||
		strings.HasPrefix(low, "file:") ||
		strings.HasPrefix(low, "vbscript:") {
		return "", fmt.Errorf("refusing to open potentially dangerous URL scheme")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	default: // linux, freebsd, etc.
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("open URL: %w", err)
	}
	// Detach: wait in a goroutine so we don't block the reply.  We discard the
	// wait result because the child process may out-live the parent.
	go cmd.Wait() //nolint:errcheck

	return fmt.Sprintf("Opened %q in the default browser", url), nil
}

// ---------------------------------------------------------------------------
// run_command
// ---------------------------------------------------------------------------

// allowedCommands is the whitelist of bare executable names that run_command
// will accept.  The map value is unused; the key is the lower-cased name.
var allowedCommands = map[string]bool{
	// Windows inbox
	"notepad.exe":   true,
	"calc.exe":      true,
	"mspaint.exe":   true,
	"explorer.exe":  true,
	// Editors
	"code.exe":              true,
	"code-insiders.cmd":     true,
	// Notes
	"notion.exe":  true,
	"obsidian.exe": true,
	// Browsers
	"chrome.exe":   true,
	"msedge.exe":   true,
	"firefox.exe":  true,
	"brave.exe":    true,
	// Office (Windows)
	"winword.exe":   true,
	"excel.exe":     true,
	"powerpnt.exe":  true,
	// Cross-platform names (also used as fallback on macOS / Linux)
	"code":          true,
	"code-insiders": true,
	"notion":        true,
	"obsidian":      true,
	"chrome":        true,
	"msedge":        true,
	"firefox":       true,
	"brave":         true,
}

// dangerousPatterns are substrings that, when present in the cmd string, cause
// an immediate rejection — shell metacharacters and destructive operations.
var dangerousPatterns = []string{
	"&&", "||", "|", ";", "`", "$(",
	"rm", "del ", "format ", "rd ", "rmdir",
	":(){",   // fork bomb
	">",      // redirect (could be used for stealth writes)
}

// runCommand starts a whitelisted application.  Only the first whitespace-
// separated token is treated as the executable; remaining tokens are passed as
// arguments.
func (systemController) runCommand(ctx context.Context, cmdStr string) (string, error) {
	if cmdStr == "" {
		return "", fmt.Errorf("cmd is required for action=run_command")
	}

	// --- security checks ---------------------------------------------------

	low := strings.ToLower(cmdStr)
	for _, pat := range dangerousPatterns {
		if strings.Contains(low, pat) {
			return "", fmt.Errorf("refusing to run: command contains dangerous pattern %q", pat)
		}
	}

	// Extract the executable name (first token).
	tokens := strings.Fields(cmdStr)
	if len(tokens) == 0 {
		return "", fmt.Errorf("empty command")
	}
	exe := tokens[0]
	exeName := strings.ToLower(filepath.Base(exe))
	if !allowedCommands[exeName] {
		return "", fmt.Errorf("command %q is not in the allowed list", exe)
	}

	// Verify the executable actually exists on the system.
	path, err := exec.LookPath(exe)
	if err != nil {
		return "", fmt.Errorf("executable %q not found on PATH: %w", exe, err)
	}

	// --- run ----------------------------------------------------------------
	var args []string
	if len(tokens) > 1 {
		args = tokens[1:]
	}
	cmd := exec.CommandContext(ctx, path, args...)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("run command: %w", err)
	}
	go cmd.Wait() //nolint:errcheck

	return fmt.Sprintf("Started %s", path), nil
}

// ---------------------------------------------------------------------------
// create_file
// ---------------------------------------------------------------------------

func (s systemController) createFile(ctx context.Context, path, content string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required for action=create_file")
	}

	p := resolveIn(s.workDir, path)
	if err := confine(s.roots, p); err != nil {
		return "", err
	}

	// Refuse to overwrite an existing file.
	if _, err := os.Stat(p); err == nil {
		return "", fmt.Errorf("refusing to overwrite existing file %q", p)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", p, err)
	}

	if dir := filepath.Dir(p); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", p, err)
	}
	return fmt.Sprintf("Created %s (%d bytes)", p, len(content)), nil
}
