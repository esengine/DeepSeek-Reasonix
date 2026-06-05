// Package hook runs user-configured shell-command hooks around the agent loop:
// PreToolUse / PostToolUse fire around each tool call, UserPromptSubmit before a
// turn, Stop after it. Hooks come from settings.json — a project
// (.reasonix/settings.json, only when the project is trusted) and a global
// (~/.reasonix/settings.json) file. A hook's exit
// code is its verdict: 0 = pass, 2 = block (only on the gating events), other =
// warn. The payload is delivered as JSON on stdin; output is captured (capped)
// and surfaced to the user. This package only loads, matches, and runs hooks;
// the agent and controller decide what a block means (see internal/agent,
// internal/control).
package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/proc"
)

// Event is a point in the agent loop a hook can fire at.
type Event string

const (
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	UserPromptSubmit Event = "UserPromptSubmit"
	Stop             Event = "Stop"
	// PostLLMCall fires after every model turn completes (streaming finishes) but
	// before the reasoning_content is stored in the session. The hook receives the
	// raw reasoning text in the payload; its stdout, if non-empty on exit 0,
	// replaces the reasoning stored and displayed to the user. It can't block — a
	// non-zero exit or empty stdout leaves the reasoning unchanged.
	PostLLMCall Event = "PostLLMCall"
	// SessionStart fires once when a session becomes active (fresh, resumed, or
	// after /new). SessionEnd fires when it is closed or rotated. SubagentStop
	// fires when a `task` sub-agent finishes. Notification fires when the agent
	// needs the user's attention (e.g. a pending approval). PreCompact fires just
	// before a compaction pass; its stdout is injected as extra summary guidance.
	SessionStart Event = "SessionStart"
	SessionEnd   Event = "SessionEnd"
	SubagentStop Event = "SubagentStop"
	Notification Event = "Notification"
	PreCompact   Event = "PreCompact"
)

// Events is every event, in a stable order — drives loading and `/hooks`.
var Events = []Event{
	PreToolUse, PostToolUse, UserPromptSubmit, Stop,
	PostLLMCall,
	SessionStart, SessionEnd, SubagentStop, Notification, PreCompact,
}

// IsBlocking reports whether a non-zero/exit-2 (or timed-out) hook on this event
// can block the loop. Only the gating events qualify. (PreCompact does not block;
// it only contributes guidance via stdout.)
func IsBlocking(e Event) bool { return e == PreToolUse || e == UserPromptSubmit }

// defaultTimeout is the per-event timeout when a hook sets none. Tool/prompt
// hooks gate progress, so they're tight; post/stop hooks get more room.
func defaultTimeout(e Event) time.Duration {
	switch e {
	case PreToolUse, UserPromptSubmit:
		return 5 * time.Second
	default:
		return 30 * time.Second
	}
}

// Scope records which settings.json a hook came from. Project hooks fire before
// global ones.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// HookConfig is one hook as written in settings.json.
type HookConfig struct {
	// Match is an anchored regex selecting tools (Pre/PostToolUse only); "" or
	// "*" = every tool. Anchored: "file" won't match "read_file" — use ".*file".
	Match string `json:"match,omitempty"`
	// Command is the shell command to run (spawned through the platform shell).
	Command string `json:"command"`
	// Description is an optional human label surfaced in `/hooks`.
	Description string `json:"description,omitempty"`
	// Timeout overrides the per-event default, in milliseconds.
	Timeout int `json:"timeout,omitempty"`
	// Cwd overrides the working directory (defaults to the payload's cwd).
	Cwd string `json:"cwd,omitempty"`
}

// Settings is the shape of a settings.json (only hooks for now).
type Settings struct {
	Hooks map[Event][]HookConfig `json:"hooks"`
}

// ResolvedHook is a loaded hook with its origin baked in.
type ResolvedHook struct {
	HookConfig
	Event  Event
	Scope  Scope
	Source string // absolute path to the settings.json it came from
}

func (h ResolvedHook) timeout() time.Duration {
	if h.Timeout > 0 {
		return time.Duration(h.Timeout) * time.Millisecond
	}
	return defaultTimeout(h.Event)
}

// SettingsDirname / SettingsFilename locate a scope's settings.json.
const (
	SettingsDirname  = ".reasonix"
	SettingsFilename = "settings.json"
)

// GlobalSettingsPath is ~/.reasonix/settings.json (homeDir overrides ~).
func GlobalSettingsPath(homeDir string) string {
	return filepath.Join(home(homeDir), SettingsDirname, SettingsFilename)
}

// ProjectSettingsPath is <root>/.reasonix/settings.json.
func ProjectSettingsPath(projectRoot string) string {
	return filepath.Join(projectRoot, SettingsDirname, SettingsFilename)
}

// LoadOptions configure Load. Project hooks load only when Trusted; global hooks
// always load.
type LoadOptions struct {
	ProjectRoot string
	HomeDir     string
	Trusted     bool
}

// Load resolves hooks: project first (only when trusted), then global; within a
// scope, settings.json array order. A malformed file yields no hooks (never an
// error — a typo shouldn't take down the CLI).
func Load(opts LoadOptions) []ResolvedHook {
	var out []ResolvedHook
	if opts.ProjectRoot != "" && opts.Trusted {
		p := ProjectSettingsPath(opts.ProjectRoot)
		if s := readSettings(p); s != nil {
			appendResolved(&out, s, ScopeProject, p)
		}
	}
	g := GlobalSettingsPath(opts.HomeDir)
	if s := readSettings(g); s != nil {
		appendResolved(&out, s, ScopeGlobal, g)
	}
	return out
}

// ProjectDefinesHooks reports whether a project's settings.json exists and
// declares at least one hook — regardless of trust. Frontends use this to decide
// whether to prompt the user to trust the project.
func ProjectDefinesHooks(projectRoot string) bool {
	s := readSettings(ProjectSettingsPath(projectRoot))
	if s == nil {
		return false
	}
	for _, e := range Events {
		for _, cfg := range s.Hooks[e] {
			if strings.TrimSpace(cfg.Command) != "" {
				return true
			}
		}
	}
	return false
}

func readSettings(path string) *Settings {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil // malformed → treat as no hooks, don't crash
	}
	return &s
}

func appendResolved(out *[]ResolvedHook, s *Settings, scope Scope, source string) {
	if s.Hooks == nil {
		return
	}
	for _, event := range Events {
		for _, cfg := range s.Hooks[event] {
			if strings.TrimSpace(cfg.Command) == "" {
				continue
			}
			*out = append(*out, ResolvedHook{HookConfig: cfg, Event: event, Scope: scope, Source: source})
		}
	}
}

// MatchesTool reports whether a hook applies to toolName. The match field is an
// anchored regex; non-tool events always match. A malformed regex never fires
// (safer than firing on everything).
func MatchesTool(h ResolvedHook, toolName string) bool {
	if h.Event != PreToolUse && h.Event != PostToolUse {
		return true
	}
	m := h.Match
	if m == "" || m == "*" {
		return true
	}
	re, err := regexp.Compile("^(?:" + m + ")$")
	if err != nil {
		return false
	}
	return re.MatchString(toolName)
}

// Payload is the JSON envelope written to a hook's stdin.
type Payload struct {
	Event         Event           `json:"event"`
	Cwd           string          `json:"cwd"`
	ToolName      string          `json:"toolName,omitempty"`
	ToolArgs      json.RawMessage `json:"toolArgs,omitempty"`
	ToolResult    string          `json:"toolResult,omitempty"`
	Prompt        string          `json:"prompt,omitempty"`
	LastAssistant string          `json:"lastAssistantText,omitempty"`
	Turn          int             `json:"turn,omitempty"`
	Message       string          `json:"message,omitempty"`   // Notification: what needs attention
	Trigger       string          `json:"trigger,omitempty"`   // PreCompact: "auto" | "manual"
	Reasoning     string          `json:"reasoning,omitempty"` // PostLLMCall: the model's raw reasoning text
}

// Decision is a single hook invocation's verdict.
type Decision string

const (
	DecisionPass  Decision = "pass"
	DecisionBlock Decision = "block"
	DecisionWarn  Decision = "warn"
	DecisionError Decision = "error" // spawn failed (ENOENT, EACCES, …)
)

// Outcome records one hook invocation.
type Outcome struct {
	Hook      ResolvedHook
	Decision  Decision
	ExitCode  int // -1 when unknown (killed / spawn error)
	Stdout    string
	Stderr    string
	TimedOut  bool
	Truncated bool
	Duration  time.Duration
}

// Report aggregates the outcomes of running an event's hooks.
type Report struct {
	Event    Event
	Outcomes []Outcome
	Blocked  bool // at least one outcome blocked (only meaningful on gating events)
}

// decideOutcome maps a spawn result to a verdict.
func decideOutcome(event Event, r SpawnResult) Decision {
	switch {
	case r.SpawnErr != nil:
		return DecisionError
	case r.TimedOut:
		if IsBlocking(event) {
			return DecisionBlock
		}
		return DecisionWarn
	case r.ExitCode == 0:
		return DecisionPass
	case r.ExitCode == 2 && IsBlocking(event):
		return DecisionBlock
	default:
		return DecisionWarn
	}
}

// SpawnInput / SpawnResult / Spawner are the test seam around the real spawn.
type SpawnInput struct {
	Command string
	Cwd     string
	Stdin   string
	Timeout time.Duration
}

type SpawnResult struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	TimedOut  bool
	SpawnErr  error
	Truncated bool
}

type Spawner func(ctx context.Context, in SpawnInput) SpawnResult

// outputCapBytes bounds per-stream capture so a runaway child can't blow up the
// heap between spawn and timeout.
const outputCapBytes = 256 * 1024

// Run executes the hooks matching payload.Event (and, for tool events, the tool
// name), feeding each the JSON payload on stdin. It stops at the first block so
// a gating hook can prevent later hooks running against a phantom success.
func Run(ctx context.Context, payload Payload, hooks []ResolvedHook, spawner Spawner) Report {
	if spawner == nil {
		spawner = DefaultSpawner
	}
	event := payload.Event
	stdinBytes, _ := json.Marshal(payload)
	stdin := string(stdinBytes) + "\n"

	report := Report{Event: event}
	for _, h := range hooks {
		if h.Event != event || !MatchesTool(h, payload.ToolName) {
			continue
		}
		cwd := h.Cwd
		if cwd == "" {
			cwd = payload.Cwd
		}
		timeout := h.timeout()
		start := time.Now()
		r := spawner(ctx, SpawnInput{Command: h.Command, Cwd: cwd, Stdin: stdin, Timeout: timeout})
		decision := decideOutcome(event, r)
		report.Outcomes = append(report.Outcomes, Outcome{
			Hook:      h,
			Decision:  decision,
			ExitCode:  r.ExitCode,
			Stdout:    r.Stdout,
			Stderr:    stderrFor(r, timeout),
			TimedOut:  r.TimedOut,
			Truncated: r.Truncated,
			Duration:  time.Since(start),
		})
		if decision == DecisionBlock {
			report.Blocked = true
			break
		}
	}
	return report
}

// stderrFor returns the best human message for an outcome: real stderr, else a
// spawn-error message, else a timeout note.
func stderrFor(r SpawnResult, timeout time.Duration) string {
	if r.Stderr != "" {
		return r.Stderr
	}
	if r.SpawnErr != nil {
		return r.SpawnErr.Error()
	}
	if r.TimedOut {
		return fmt.Sprintf("hook timed out after %s", timeout)
	}
	return ""
}

// DefaultSpawner runs the command through the platform shell with the payload on
// stdin, capping captured output and honoring both the per-hook timeout and the
// parent context's cancellation.
func DefaultSpawner(ctx context.Context, in SpawnInput) SpawnResult {
	cctx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()

	name, args := shellInvocation(in.Command)
	cmd := exec.CommandContext(cctx, name, args...)
	proc.HideWindow(cmd)
	cmd.Dir = in.Cwd
	cmd.Stdin = strings.NewReader(in.Stdin)
	var outBuf, errBuf cappedBuffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	// WaitDelay bounds Wait even if a grandchild keeps a pipe open after the
	// shell is killed on timeout/cancel.
	cmd.WaitDelay = 500 * time.Millisecond

	err := cmd.Run()
	res := SpawnResult{
		ExitCode:  -1,
		Stdout:    strings.TrimSpace(outBuf.String()),
		Stderr:    strings.TrimSpace(errBuf.String()),
		Truncated: outBuf.truncated || errBuf.truncated,
	}
	switch {
	case cctx.Err() == context.DeadlineExceeded:
		res.TimedOut = true
	case cctx.Err() == context.Canceled:
		res.SpawnErr = cctx.Err()
	case err != nil:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.SpawnErr = err
		}
	default:
		res.ExitCode = 0
	}
	return res
}

func shellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", command}
	}
	return "sh", []string{"-c", command}
}

// cappedBuffer is an io.Writer that stops storing after outputCapBytes and
// records that it truncated, but keeps reporting full writes so the child never
// sees a short-write error.
type cappedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := outputCapBytes - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		c.buf.Write(p[:remaining])
		c.truncated = true
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }

func home(override string) string {
	if override != "" {
		return override
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}
