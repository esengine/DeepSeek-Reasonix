// Package hook runs user-configured shell-command hooks around the agent loop:
// PreToolUse / PostToolUse fire around each tool call, PermissionRequest fires
// before a tool approval prompt is shown, UserPromptSubmit before a turn, Stop
// after it. Hooks come from settings.json — a project
// (.reasonix/settings.json, only when the project is trusted) and a global
// (<Reasonix home>/settings.json) file. A hook's exit
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
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

// Event is a point in the agent loop a hook can fire at.
type Event string

const (
	PreToolUse         Event = "PreToolUse"
	PostToolUse        Event = "PostToolUse"
	PostToolUseFailure Event = "PostToolUseFailure"
	PermissionRequest  Event = "PermissionRequest"
	UserPromptSubmit   Event = "UserPromptSubmit"
	Stop               Event = "Stop"
	StopFailure        Event = "StopFailure"
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
	PreToolUse, PostToolUse, PostToolUseFailure, PermissionRequest, UserPromptSubmit, Stop, StopFailure,
	PostLLMCall,
	SessionStart, SessionEnd, SubagentStop, Notification, PreCompact,
}

// IsBlocking reports whether a non-zero/exit-2 (or timed-out) hook on this event
// can block the loop. Only the gating events qualify. (PreCompact does not block;
// it only contributes guidance via stdout.) This governs native Reasonix hooks;
// see claudePermissionBlocking for the Claude-imported PermissionRequest case.
func IsBlocking(e Event) bool { return e == PreToolUse || e == UserPromptSubmit }

// claudePermissionBlocking reports whether exit code 2 (or a timeout) on h
// aborts the action even though PermissionRequest is not one of Reasonix's own
// blocking events (docs/DESKTOP_HOOKS.md: "只有 PreToolUse 和 UserPromptSubmit
// 是阻塞型事件"). Claude's own PermissionRequest contract denies the permission
// on exit 2 the same way PreToolUse does (https://code.claude.com/docs/en/hooks),
// so an imported Claude hook (PayloadFormat "claude") honors that instead of
// silently downgrading to a notification.
func claudePermissionBlocking(h ResolvedHook) bool {
	return h.Event == PermissionRequest && h.PayloadFormat == "claude"
}

// defaultTimeout is the per-event timeout when a hook sets none. Tool/prompt
// hooks gate progress, so they're tight; post/stop hooks get more room.
func defaultTimeout(e Event) time.Duration {
	switch e {
	case PreToolUse, PermissionRequest, UserPromptSubmit:
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
	ScopePlugin  Scope = "plugin"
	ScopeGlobal  Scope = "global"
)

// HookConfig is one hook as written in settings.json.
type HookConfig struct {
	// Match is an anchored regex selecting tools (Pre/PostToolUse and
	// PermissionRequest only); "" or "*" = every tool. Anchored: "file" won't
	// match "read_file" — use ".*file".
	Match string `json:"match,omitempty"`
	// Command is the shell command to run (spawned through the platform shell).
	Command string `json:"command"`
	// Argv bypasses the shell and is used by imported Claude hooks whose
	// command and args are separate manifest fields.
	Argv []string `json:"-"`
	// ContextFile is an internal plugin-package helper: when set, the hook reads
	// this file and treats it as stdout instead of spawning a shell command.
	ContextFile string `json:"contextFile,omitempty"`
	// Description is an optional human label surfaced in `/hooks`.
	Description string `json:"description,omitempty"`
	// Timeout overrides the per-event default, in milliseconds.
	Timeout int `json:"timeout,omitempty"`
	// Cwd overrides the working directory (defaults to the payload's cwd).
	Cwd string `json:"cwd,omitempty"`
	// Env adds environment variables for this hook invocation.
	Env map[string]string `json:"env,omitempty"`
	// Async and PayloadFormat are internal compatibility metadata populated for
	// imported Claude hooks. Native Reasonix settings keep their old behavior.
	Async         bool   `json:"-"`
	PayloadFormat string `json:"-"`
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

// GlobalSettingsPath is <Reasonix home>/settings.json (homeDir overrides ~ for
// tests and legacy callers).
func GlobalSettingsPath(homeDir string) string {
	return filepath.Join(reasonixHome(homeDir), SettingsFilename)
}

// ProjectSettingsPath is <root>/.reasonix/settings.json.
func ProjectSettingsPath(projectRoot string) string {
	return filepath.Join(projectRoot, SettingsDirname, SettingsFilename)
}

// ContextFileUsable reports whether a plugin contextFile can take the same
// execution path as readContextFile. Keep machine status and diagnostics on
// this shared predicate so a path that merely exists (for example, a
// directory) is not advertised as runnable.
func ContextFileUsable(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	return file.Close() == nil
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
	appendPluginHooks(&out, reasonixHome(opts.HomeDir), opts.ProjectRoot)
	g := GlobalSettingsPath(opts.HomeDir)
	if s := readSettings(g); s != nil {
		appendResolved(&out, s, ScopeGlobal, g)
	} else if !pathExists(g) {
		if legacy := legacyGlobalSettingsPath(opts.HomeDir); legacy != "" {
			if s := readSettings(legacy); s != nil {
				appendResolved(&out, s, ScopeGlobal, legacy)
			}
		}
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
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return nil
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil // malformed → treat as no hooks, don't crash
	}
	return &s
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
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
			cfg.Command = NormalizeCommand(cfg.Command)
			*out = append(*out, ResolvedHook{HookConfig: cfg, Event: event, Scope: scope, Source: source})
		}
	}
}

func appendPluginHooks(out *[]ResolvedHook, reasonixHomeDir, projectRoot string) {
	if strings.TrimSpace(reasonixHomeDir) == "" {
		return
	}
	installed, _ := pluginpkg.LoadInstalled(reasonixHomeDir)
	for _, item := range installed {
		pkg := item.Package
		events := make([]string, 0, len(pkg.Manifest.Hooks))
		for event := range pkg.Manifest.Hooks {
			events = append(events, event)
		}
		sort.Strings(events)
		for _, eventName := range events {
			event := Event(eventName)
			if !validEvent(event) {
				continue
			}
			for _, h := range pkg.Manifest.Hooks[eventName] {
				command := expandPluginRoot(h.Command, pkg.Root)
				if command != "" && !h.ShellCommand && !filepath.IsAbs(command) {
					command = filepath.Join(pkg.Root, filepath.FromSlash(command))
				}
				command = NormalizeCommand(command)
				var argv []string
				if len(h.Args) > 0 {
					argv = make([]string, 0, len(h.Args))
				}
				for _, arg := range h.Args {
					argv = append(argv, expandPluginRoot(arg, pkg.Root))
				}
				contextFile := expandPluginRoot(h.ContextFile, pkg.Root)
				if contextFile != "" {
					contextFile = filepath.FromSlash(contextFile)
					if !filepath.IsAbs(contextFile) {
						contextFile = filepath.Join(pkg.Root, contextFile)
					} else {
						contextFile = filepath.Clean(contextFile)
					}
				}
				cwd := expandPluginRoot(h.Cwd, pkg.Root)
				if cwd == "" {
					cwd = pkg.Root
				} else {
					cwd = filepath.FromSlash(cwd)
					if !filepath.IsAbs(cwd) {
						cwd = filepath.Join(pkg.Root, cwd)
					} else {
						cwd = filepath.Clean(cwd)
					}
				}
				env := cloneEnv(h.Env)
				for key, value := range env {
					env[key] = expandPluginRoot(value, pkg.Root)
				}
				env["REASONIX_PLUGIN_ROOT"] = pkg.Root
				env["REASONIX_PLUGIN_NAME"] = item.Installed.Name
				env["REASONIX_HOME"] = reasonixHomeDir
				env["REASONIX_WORKSPACE_ROOT"] = projectRoot
				env["CLAUDE_PROJECT_DIR"] = projectRoot
				env["CLAUDE_PLUGIN_ROOT"] = pkg.Root
				if item.Installed.Version != "" {
					env["REASONIX_PLUGIN_VERSION"] = item.Installed.Version
				}
				*out = append(*out, ResolvedHook{
					HookConfig: HookConfig{
						Match:         h.Match,
						Command:       command,
						Argv:          argv,
						ContextFile:   contextFile,
						Description:   h.Description,
						Timeout:       h.Timeout,
						Cwd:           cwd,
						Env:           env,
						Async:         h.Async,
						PayloadFormat: h.PayloadFormat,
					},
					Event:  event,
					Scope:  ScopePlugin,
					Source: filepath.Join(pkg.Root, pluginpkg.ManifestPath(pkg.ManifestKind)),
				})
			}
		}
	}
}

func expandPluginRoot(value, root string) string {
	// Plugin hook manifests are host configuration, not platform-native shell
	// scripts. Scan the manifest value once so text inside the resolved root is
	// never mistaken for another placeholder and expanded recursively.
	lastWrite := 0
	replaced := false
	var out strings.Builder
	for i := 0; i < len(value); {
		tokenLen := pluginRootTokenLen(value[i:])
		if tokenLen == 0 {
			i++
			continue
		}
		if !replaced {
			out.Grow(len(value) - tokenLen + len(root))
			replaced = true
		}
		out.WriteString(value[lastWrite:i])
		out.WriteString(root)
		i += tokenLen
		lastWrite = i
	}
	if !replaced {
		return value
	}
	out.WriteString(value[lastWrite:])
	return out.String()
}

var pluginRootTokens = [...]struct {
	value         string
	needsBoundary bool
}{
	{value: "${CLAUDE_PLUGIN_ROOT}"},
	{value: "$CLAUDE_PLUGIN_ROOT", needsBoundary: true},
	{value: "%CLAUDE_PLUGIN_ROOT%"},
	{value: "${REASONIX_PLUGIN_ROOT}"},
	{value: "$REASONIX_PLUGIN_ROOT", needsBoundary: true},
	{value: "%REASONIX_PLUGIN_ROOT%"},
}

func pluginRootTokenLen(value string) int {
	for _, token := range pluginRootTokens {
		if !strings.HasPrefix(value, token.value) {
			continue
		}
		if token.needsBoundary && len(value) > len(token.value) && isShellVariableNameByte(value[len(token.value)]) {
			continue
		}
		return len(token.value)
	}
	return 0
}

func isShellVariableNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func validEvent(event Event) bool {
	for _, e := range Events {
		if e == event {
			return true
		}
	}
	return false
}

func cloneEnv(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	return out
}

// MatchesTool reports whether a hook applies to toolName. The match field is an
// anchored regex; non-tool events always match. A malformed regex never fires
// (safer than firing on everything).
func MatchesTool(h ResolvedHook, toolName string) bool {
	if !UsesToolMatcher(h.Event) {
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
	if h.PayloadFormat != "claude" {
		return re.MatchString(toolName)
	}
	for _, candidate := range claudeMatchNames(toolName) {
		if re.MatchString(candidate) {
			return true
		}
	}
	return false
}

// claudeAgentSpawningTools are every Reasonix tool that spawns a subagent and
// so corresponds to Claude's single "Agent" tool: the general task delegator
// (task/read_only_task/parallel_tasks) and the dedicated named wrappers
// around a runAs=subagent skill (BuiltinSubagentTools in
// internal/skill/tools.go — each is a distinct, directly-callable tool, not
// routed through run_skill). A Claude "Agent" safety matcher must see all of
// them, or a hook scoped to it silently misses whichever entry point wasn't
// mapped.
var claudeAgentSpawningTools = []string{
	"task", "read_only_task", "parallel_tasks",
	"explore", "research", "review", "security_review",
}

// claudeAgentDefaultDescriptions fill Claude Agent's required description
// field when the corresponding Reasonix tool does not expose one or the model
// omitted Reasonix's optional description. These are stable operation labels;
// the complete task remains in prompt for hook policy decisions.
var claudeAgentDefaultDescriptions = map[string]string{
	"task":            "Run delegated subagent task",
	"read_only_task":  "Run read-only research task",
	"parallel_tasks":  "Run parallel subagent tasks",
	"explore":         "Explore the codebase",
	"research":        "Research external references",
	"review":          "Review the current changes",
	"security_review": "Review security risks",
}

// claudeToolNames maps Reasonix's own tool names to the *current* Claude Code
// built-in tool name (https://code.claude.com/docs/en/tools-reference) — what
// an imported hook's emitted tool_name payload field shows, and a script's own
// tool_name check is written against. MCP tool names already share the
// mcp__<server>__<tool> convention in both systems.
var claudeToolNames = buildClaudeToolNames()

func buildClaudeToolNames() map[string]string {
	out := map[string]string{
		"bash":            "Bash",
		"read_file":       "Read",
		"write_file":      "Write",
		"edit_file":       "Edit",
		"multi_edit":      "MultiEdit",
		"glob":            "Glob",
		"grep":            "Grep",
		"web_fetch":       "WebFetch",
		"ask":             "AskUserQuestion",
		"run_skill":       "Skill",
		"read_only_skill": "Skill",
		"todo_write":      "TodoWrite",
		"notebook_edit":   "NotebookEdit",
		"bash_output":     "TaskOutput",
		"wait":            "TaskOutput",
		"kill_shell":      "TaskStop",
	}
	for _, name := range claudeAgentSpawningTools {
		out[name] = "Agent"
	}
	return out
}

// claudeToolMatchAliases lists every tool name — current and legacy — an
// imported hook's matcher may have been authored against for a Reasonix
// tool, so a matcher written against an older Claude Code tool name keeps
// firing after Claude renames the tool (Task became Agent; BashOutput/KillShell
// became TaskOutput/TaskStop). claudeFacingToolName (the emitted tool_name
// payload) always reports the current name; only matcher evaluation considers
// aliases.
var claudeToolMatchAliases = buildClaudeToolMatchAliases()

func buildClaudeToolMatchAliases() map[string][]string {
	out := map[string][]string{}
	for _, name := range claudeAgentSpawningTools {
		out[name] = []string{"Agent", "Task"}
	}
	out["bash_output"] = []string{"TaskOutput", "BashOutput"}
	out["wait"] = []string{"TaskOutput", "BashOutput"}
	out["kill_shell"] = []string{"TaskStop", "KillShell"}
	return out
}

// claudeMatchNames returns every name an imported hook's matcher should be
// tried against for a Reasonix tool call.
func claudeMatchNames(name string) []string {
	if aliases, ok := claudeToolMatchAliases[name]; ok {
		return aliases
	}
	return []string{claudeFacingToolName(name)}
}

// claudeFacingToolName returns the current Claude tool name a Claude-imported
// hook's tool_name payload field should see for a Reasonix tool call.
// Reasonix-only tools (wait, code_index, move_file, ...) have no Claude
// equivalent and pass through unchanged — an imported hook can't have been
// authored against a name Claude never had.
func claudeFacingToolName(name string) string {
	if mapped, ok := claudeToolNames[name]; ok {
		return mapped
	}
	return name
}

// claudeToolInputKeyRenames maps, per Reasonix tool name, JSON keys in its
// tool-call arguments that must be renamed to Claude's own tool_input field
// name — Reasonix's file tools use "path", Claude's use "file_path" — so a
// hook script reading e.g. ".tool_input.file_path" sees the value instead of
// failing open on an empty field. Only tools whose Reasonix schema differs
// from Claude's by a plain key rename are listed: Bash's "command",
// Glob/Grep's "pattern"/"path", web_fetch's "url", ask's "questions",
// todo_write's "todos", and task/read_only_task's "prompt"/"description"
// already use Claude's field names. Agent description can still be absent and
// is filled separately below. NotebookEdit's cell_number (a
// 0-based index) has no Claude field — Claude targets cells only by the
// opaque cell_id, which Reasonix also accepts — so it passes through as an
// extra key. parallel_tasks is a structural mismatch handled separately in
// claudeFacingToolInput.
var claudeToolInputKeyRenames = map[string]map[string]string{
	"read_file":       {"path": "file_path"},
	"write_file":      {"path": "file_path"},
	"edit_file":       {"path": "file_path"},
	"multi_edit":      {"path": "file_path"},
	"notebook_edit":   {"path": "notebook_path"},
	"run_skill":       {"name": "skill", "arguments": "args"},
	"read_only_skill": {"name": "skill", "arguments": "args"},
	"bash_output":     {"job_id": "task_id"},
	"kill_shell":      {"job_id": "task_id"},
	// The dedicated subagent wrappers take their task text as "task";
	// Claude's Agent tool calls the same thing "prompt".
	"explore":         {"task": "prompt"},
	"research":        {"task": "prompt"},
	"review":          {"task": "prompt"},
	"security_review": {"task": "prompt"},
}

// claudeAbsolutePathInputKeys are the translated tool_input keys whose Claude
// schema demands an absolute path ("must be absolute, not relative" on
// Read/Write/Edit/NotebookEdit). Reasonix's file tools accept relative paths
// and resolve them against the workspace root (resolveIn in
// internal/tool/builtin/workspace.go); the payload resolves against
// payload.Cwd — the same root — so a prefix-matching guard inspects the path
// the tool actually accesses, not a relative spelling it never compares.
var claudeAbsolutePathInputKeys = []string{"file_path", "notebook_path"}

// claudeFacingToolInput adapts tool-call arguments to the tool_input a
// Claude-authored hook script was written against: keys are renamed per
// claudeToolInputKeyRenames, file paths are made absolute, current TaskOutput
// fields and required Agent/AskUserQuestion/TodoWrite fields are supplied, and
// parallel_tasks synthesizes Agent's "prompt". Args needing no translation, or
// that aren't a JSON object, pass through unchanged.
func claudeFacingToolInput(toolName string, args json.RawMessage, cwd string) json.RawMessage {
	renames := claudeToolInputKeyRenames[toolName]
	defaultAgentDescription, isAgent := claudeAgentDefaultDescriptions[toolName]
	if len(renames) == 0 && !isAgent && toolName != "ask" && toolName != "todo_write" && toolName != "wait" {
		return args
	}
	if len(args) == 0 {
		return args
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return args
	}
	changed := false
	for from, to := range renames {
		if v, exists := obj[from]; exists {
			obj[to] = v
			delete(obj, from)
			changed = true
		}
	}
	if toolName == "notebook_edit" {
		if _, exists := obj["new_source"]; !exists {
			for _, alias := range []string{"content", "source", "new_string"} {
				var value string
				if err := json.Unmarshal(obj[alias], &value); err == nil && value != "" {
					obj["new_source"] = obj[alias]
					break
				}
			}
			if _, exists := obj["new_source"]; !exists {
				obj["new_source"] = json.RawMessage(`""`)
			}
			changed = true
		}
	}
	if toolName == "bash_output" {
		obj["block"] = json.RawMessage("false")
		obj["timeout"] = json.RawMessage("0")
		changed = true
	}
	if toolName == "wait" {
		obj["block"] = json.RawMessage("true")
		var jobIDs []string
		if err := json.Unmarshal(obj["job_ids"], &jobIDs); err == nil && len(jobIDs) == 1 {
			if body, err := json.Marshal(jobIDs[0]); err == nil {
				obj["task_id"] = body
			}
		}
		// An unbounded Reasonix wait omits TaskOutput's optional timeout
		// entirely: in Claude's schema timeout is the maximum wait in ms, so
		// claiming 0 would read as "don't wait" — the opposite of the call.
		var timeoutSeconds int64
		if err := json.Unmarshal(obj["timeout_seconds"], &timeoutSeconds); err == nil && timeoutSeconds > 0 && timeoutSeconds <= (1<<63-1)/1000 {
			if body, err := json.Marshal(timeoutSeconds * 1000); err == nil {
				obj["timeout"] = body
			}
		}
		changed = true
	}
	if toolName == "ask" && fillClaudeAskDefaults(obj) {
		changed = true
	}
	if toolName == "todo_write" && fillClaudeTodoDefaults(obj) {
		changed = true
	}
	// parallel_tasks maps to Claude's Agent tool but carries an array of
	// sub-tasks where Agent has a single prompt — a structural difference no
	// key rename bridges. Synthesize "prompt" from every sub-task's prompt
	// (the original "tasks" array stays alongside) so an Agent-scoped guard
	// reading .tool_input.prompt inspects all dispatched work instead of
	// failing open on a missing field.
	if toolName == "parallel_tasks" {
		if prompt := joinedParallelTaskPrompts(obj["tasks"]); prompt != "" {
			if v, err := json.Marshal(prompt); err == nil {
				obj["prompt"] = v
				changed = true
			}
		}
	}
	if isAgent {
		var prompt string
		_ = json.Unmarshal(obj["prompt"], &prompt)
		if strings.TrimSpace(prompt) != "" {
			var description string
			_ = json.Unmarshal(obj["description"], &description)
			if strings.TrimSpace(description) == "" {
				if v, err := json.Marshal(defaultAgentDescription); err == nil {
					obj["description"] = v
					changed = true
				}
			}
		}
	}
	for _, key := range claudeAbsolutePathInputKeys {
		v, exists := obj[key]
		if !exists || cwd == "" {
			continue
		}
		var p string
		if err := json.Unmarshal(v, &p); err != nil || p == "" || filepath.IsAbs(p) {
			continue
		}
		if abs, err := json.Marshal(filepath.Join(cwd, p)); err == nil {
			obj[key] = abs
			changed = true
		}
	}
	if !changed {
		return args
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return args
	}
	return out
}

// fillClaudeAskDefaults supplies fields Claude requires but Reasonix treats as
// optional. Empty option descriptions are honest (Reasonix has no explanation
// to add), and omitted multiSelect has the same false default in both systems.
func fillClaudeAskDefaults(obj map[string]json.RawMessage) bool {
	var questions []map[string]json.RawMessage
	if err := json.Unmarshal(obj["questions"], &questions); err != nil {
		return false
	}
	changed := false
	for _, question := range questions {
		if _, exists := question["multiSelect"]; !exists {
			question["multiSelect"] = json.RawMessage("false")
			changed = true
		}
		var options []map[string]json.RawMessage
		if err := json.Unmarshal(question["options"], &options); err != nil {
			continue
		}
		optionsChanged := false
		for _, option := range options {
			if _, exists := option["description"]; !exists {
				option["description"] = json.RawMessage(`""`)
				optionsChanged = true
				changed = true
			}
		}
		if optionsChanged {
			body, err := json.Marshal(options)
			if err != nil {
				return false
			}
			question["options"] = body
		}
	}
	if !changed {
		return false
	}
	body, err := json.Marshal(questions)
	if err != nil {
		return false
	}
	obj["questions"] = body
	return true
}

// fillClaudeTodoDefaults supplies Claude's required activeForm label from the
// Reasonix task content when the caller omitted it.
func fillClaudeTodoDefaults(obj map[string]json.RawMessage) bool {
	var todos []map[string]json.RawMessage
	if err := json.Unmarshal(obj["todos"], &todos); err != nil {
		return false
	}
	changed := false
	for _, todo := range todos {
		var activeForm string
		_ = json.Unmarshal(todo["activeForm"], &activeForm)
		if strings.TrimSpace(activeForm) != "" {
			continue
		}
		var content string
		if err := json.Unmarshal(todo["content"], &content); err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		body, err := json.Marshal(content)
		if err != nil {
			return false
		}
		todo["activeForm"] = body
		changed = true
	}
	if !changed {
		return false
	}
	body, err := json.Marshal(todos)
	if err != nil {
		return false
	}
	obj["todos"] = body
	return true
}

// joinedParallelTaskPrompts flattens a parallel_tasks "tasks" array into one
// prompt string, blank-line separated. Malformed or empty input yields "".
func joinedParallelTaskPrompts(tasks json.RawMessage) string {
	if len(tasks) == 0 {
		return ""
	}
	var items []struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(tasks, &items); err != nil {
		return ""
	}
	var prompts []string
	for _, item := range items {
		if s := strings.TrimSpace(item.Prompt); s != "" {
			prompts = append(prompts, s)
		}
	}
	return strings.Join(prompts, "\n\n")
}

// Payload is the JSON envelope written to a hook's stdin.
type Payload struct {
	Event            Event           `json:"event"`
	SessionID        string          `json:"sessionId,omitempty"`
	Cwd              string          `json:"cwd"`
	ToolName         string          `json:"toolName,omitempty"`
	ToolArgs         json.RawMessage `json:"toolArgs,omitempty"`
	Subject          string          `json:"subject,omitempty"`
	ToolResult       string          `json:"toolResult,omitempty"`
	Prompt           string          `json:"prompt,omitempty"`
	LastAssistant    string          `json:"lastAssistantText,omitempty"`
	Turn             int             `json:"turn,omitempty"`
	Message          string          `json:"message,omitempty"`   // Notification: what needs attention
	Trigger          string          `json:"trigger,omitempty"`   // PreCompact: "auto" | "manual"
	Reasoning        string          `json:"reasoning,omitempty"` // PostLLMCall: the model's raw reasoning text
	Error            string          `json:"error,omitempty"`
	Source           string          `json:"source,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	NotificationType string          `json:"notificationType,omitempty"`
	IsInterrupt      bool            `json:"isInterrupt,omitempty"`
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
	// Allowed is set when a Claude-imported PermissionRequest hook returned an
	// explicit JSON "allow" decision on exit 0 (see claudeJSONAllow) — the
	// caller should treat this as an auto-approval instead of prompting.
	Allowed bool
}

// HookOutput is the parsed, model-facing part of a successful hook stdout.
type HookOutput struct {
	AdditionalContext string
	// Deny and DenyReason carry a Claude-style JSON deny decision returned on
	// exit 0: hookSpecificOutput.permissionDecision for PreToolUse,
	// hookSpecificOutput.decision.behavior for PermissionRequest, or a
	// top-level decision:"block" for UserPromptSubmit. Claude hooks commonly
	// deny this way instead of exiting 2; see
	// https://code.claude.com/docs/en/hooks.
	Deny       bool
	DenyReason string
	// Allow carries a Claude PermissionRequest "allow" decision
	// (hookSpecificOutput.decision.behavior == "allow"): the hook answers the
	// permission dialog on the user's behalf instead of only observing it.
	Allow bool
}

type hookJSONOutput struct {
	// Decision and Reason are UserPromptSubmit's (and Stop/SubagentStop's)
	// top-level deny shape: {"decision":"block","reason":"..."}.
	Decision           string `json:"decision"`
	Reason             string `json:"reason"`
	HookSpecificOutput struct {
		HookEventName            Event  `json:"hookEventName"`
		AdditionalContext        string `json:"additionalContext"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
		Decision                 struct {
			Behavior string `json:"behavior"`
		} `json:"decision"`
	} `json:"hookSpecificOutput"`
}

// ParseOutput extracts hook-specific context from stdout. Plain text is accepted
// for SessionStart compatibility; JSON output must identify the current event.
func ParseOutput(event Event, stdout string) (HookOutput, []string) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return HookOutput{}, nil
	}
	if !strings.HasPrefix(stdout, "{") {
		if event == SessionStart {
			return HookOutput{AdditionalContext: stdout}, nil
		}
		return HookOutput{}, nil
	}
	var parsed hookJSONOutput
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return HookOutput{}, []string{fmt.Sprintf("hook %s returned invalid JSON stdout: %v", event, err)}
	}
	spec := parsed.HookSpecificOutput
	topLevelDeny := event == UserPromptSubmit && strings.EqualFold(parsed.Decision, "block")
	deny := strings.EqualFold(spec.PermissionDecision, "deny") || strings.EqualFold(spec.Decision.Behavior, "deny") || topLevelDeny
	allow := event == PermissionRequest && strings.EqualFold(spec.Decision.Behavior, "allow")
	if spec.HookEventName == "" && strings.TrimSpace(spec.AdditionalContext) == "" && !deny && !allow {
		return HookOutput{}, nil
	}
	if spec.HookEventName != "" && spec.HookEventName != event {
		return HookOutput{}, []string{fmt.Sprintf("hook output event %q does not match current event %q", spec.HookEventName, event)}
	}
	out := HookOutput{AdditionalContext: strings.TrimSpace(spec.AdditionalContext)}
	if deny {
		out.Deny = true
		reason := spec.PermissionDecisionReason
		if topLevelDeny {
			reason = parsed.Reason
		}
		out.DenyReason = strings.TrimSpace(reason)
	}
	out.Allow = allow
	return out, nil
}

// decideOutcome maps a spawn result to a verdict for hook h.
func decideOutcome(h ResolvedHook, r SpawnResult) Decision {
	blocking := IsBlocking(h.Event) || claudePermissionBlocking(h)
	switch {
	case r.SpawnErr != nil:
		return DecisionError
	case r.TimedOut:
		if blocking {
			return DecisionBlock
		}
		return DecisionWarn
	case r.ExitCode == 0:
		return DecisionPass
	case r.ExitCode == 2 && blocking:
		return DecisionBlock
	default:
		return DecisionWarn
	}
}

// claudeJSONDeny reports whether a Claude-format hook's exit-0 stdout still
// carries a JSON deny decision (see HookOutput.Deny). Reasonix must honor it
// for the events it claims Claude hook compatibility for, or a plugin's
// "block this dangerous command" hook silently no-ops whenever the script
// signals deny via JSON instead of exit code 2. UserPromptSubmit uses a
// top-level decision:"block" instead of PreToolUse/PermissionRequest's
// hookSpecificOutput shape; ParseOutput handles both.
func claudeJSONDeny(event Event, stdout string) (bool, string) {
	if event != PreToolUse && event != PermissionRequest && event != UserPromptSubmit {
		return false, ""
	}
	out, _ := ParseOutput(event, stdout)
	return out.Deny, out.DenyReason
}

// claudeJSONAllow reports whether a Claude-format PermissionRequest hook's
// exit-0 stdout carries an explicit "allow" decision
// (hookSpecificOutput.decision.behavior == "allow"): the hook answers the
// permission dialog on the user's behalf, same as an exit-2 deny preempts it.
func claudeJSONAllow(event Event, stdout string) bool {
	if event != PermissionRequest {
		return false
	}
	out, _ := ParseOutput(event, stdout)
	return out.Allow
}

// SpawnInput / SpawnResult / Spawner are the test seam around the real spawn.
type SpawnInput struct {
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string
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
		stdin := marshalPayload(payload, h.PayloadFormat)
		input := SpawnInput{Command: h.Command, Args: h.Argv, Cwd: cwd, Env: h.Env, Stdin: stdin, Timeout: timeout}
		if h.Async {
			asyncCtx := context.WithoutCancel(ctx)
			go runResolvedHook(asyncCtx, h, input, spawner)
			report.Outcomes = append(report.Outcomes, Outcome{Hook: h, Decision: DecisionPass})
			continue
		}
		start := time.Now()
		r := runResolvedHook(ctx, h, input, spawner)
		decision := decideOutcome(h, r)
		if decision == DecisionPass && h.PayloadFormat == "claude" {
			if deny, reason := claudeJSONDeny(event, r.Stdout); deny {
				decision = DecisionBlock
				if reason != "" {
					r.Stdout = reason
				}
			} else if claudeJSONAllow(event, r.Stdout) {
				report.Allowed = true
			}
		}
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

func marshalPayload(payload Payload, format string) string {
	var body []byte
	if format == "claude" {
		claude := map[string]any{
			"hook_event_name":        payload.Event,
			"session_id":             payload.SessionID,
			"cwd":                    payload.Cwd,
			"tool_name":              claudeFacingToolName(payload.ToolName),
			"tool_input":             claudeFacingToolInput(payload.ToolName, payload.ToolArgs, payload.Cwd),
			"tool_response":          claudeToolResponse(payload),
			"prompt":                 payload.Prompt,
			"last_assistant_message": payload.LastAssistant,
			"source":                 payload.Source,
			"reason":                 payload.Reason,
			"notification_type":      payload.NotificationType,
			"message":                payload.Message,
			"trigger":                payload.Trigger,
			"error":                  payload.Error,
			"is_interrupt":           payload.IsInterrupt,
		}
		body, _ = json.Marshal(claude)
	} else {
		body, _ = json.Marshal(payload)
	}
	return string(body) + "\n"
}

// claudeToolResponse adapts a Reasonix tool result to the tool_response a
// Claude-authored PostToolUse hook reads. Claude's Bash response is an object
// — {stdout, stderr, interrupted}, the fields the official security-guidance
// plugin's commit/push checks read (a non-object response is treated as empty
// and the check silently passes) — while Reasonix's bash returns one combined
// output string, so it is wrapped with the failure error as stderr. Other
// tools' results pass through as before: raw JSON when the result is a JSON
// document, else the plain string.
func claudeToolResponse(p Payload) any {
	if (p.Event == PostToolUse || p.Event == PostToolUseFailure) && claudeFacingToolName(p.ToolName) == "Bash" {
		return map[string]any{
			"stdout":      p.ToolResult,
			"stderr":      p.Error,
			"interrupted": p.IsInterrupt,
		}
	}
	trimmed := strings.TrimSpace(p.ToolResult)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return p.ToolResult
	}
	return json.RawMessage(trimmed)
}

func runResolvedHook(ctx context.Context, h ResolvedHook, in SpawnInput, spawner Spawner) SpawnResult {
	if h.Scope == ScopePlugin && h.ContextFile != "" {
		return readContextFile(h.ContextFile)
	}
	return spawner(ctx, in)
}

func readContextFile(path string) SpawnResult {
	body, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return SpawnResult{ExitCode: -1, SpawnErr: err}
	}
	truncated := false
	if len(body) > outputCapBytes {
		body = body[:outputCapBytes]
		truncated = true
	}
	return SpawnResult{ExitCode: 0, Stdout: string(body), Truncated: truncated}
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

	cmd, spawnErr := spawnCommand(cctx, in.Command, in.Args)
	if spawnErr != nil {
		return SpawnResult{ExitCode: -1, SpawnErr: spawnErr}
	}
	proc.HideWindow(cmd)
	cmd.Dir = in.Cwd
	env := secrets.ProcessEnv()
	if len(in.Env) > 0 {
		keys := make([]string, 0, len(in.Env))
		for k := range in.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			env = append(env, k+"="+in.Env[k])
		}
	}
	cmd.Env = env
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
		Stdout:    decodeHookOutput(outBuf.Bytes(), outBuf.truncated),
		Stderr:    decodeHookOutput(errBuf.Bytes(), errBuf.truncated),
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

// spawnCommand picks the execution vehicle for a hook command. Commands run
// through the shell by default — that is the documented contract, and scripts
// may rely on shell expansion ($VAR, backticks). Direct exec (no shell) is
// used only where it is strictly better:
//   - a command this call just repaired (its broken quoting means it never
//     worked through a shell, so there is no expansion behavior to preserve);
//   - on Windows, a recognized node -e stdin-hook command: `cmd /c` mangles
//     quoted JS (&, %, nested quotes), which is the breakage this repair
//     exists for, and cmd performs no POSIX-style $ expansion to preserve.
//   - on Windows, an explicit `sh -c` / `bash -c` command: Git Bash is often
//     installed outside cmd.exe's PATH, and direct exec preserves its quoting.
//
// POSIX commands that were already well-formed keep their shell semantics
// verbatim — normalizeStaticNodeEval's rendering escapes $ and backticks, so
// even repaired commands re-entering here behave identically under sh -c.
func spawnCommand(ctx context.Context, command string, argv ...[]string) (*exec.Cmd, error) {
	if len(argv) > 0 && argv[0] != nil {
		if runtime.GOOS == "windows" {
			if cmd, matched := windowsBatchArgvCommand(ctx, command, argv[0]); matched {
				return cmd, nil
			}
			if shell, args, matched, err := windowsPOSIXShellArgvInvocation(command, argv[0]); matched {
				if err != nil {
					return nil, err
				}
				return exec.CommandContext(ctx, shell, args...), nil
			}
		}
		return exec.CommandContext(ctx, command, argv[0]...), nil
	}
	if node, flag, script, ok := repairableNodeEvalArgs(command); ok {
		return exec.CommandContext(ctx, node, flag, script), nil
	}
	if powershell, args, ok := repairablePowerShellFileArgs(command); ok {
		return exec.CommandContext(ctx, powershell, args...), nil
	}
	if runtime.GOOS == "windows" {
		if cmd, matched := windowsBatchCommand(ctx, command); matched {
			return cmd, nil
		}
		if shell, args, matched, err := windowsPOSIXShellInvocation(command); matched {
			if err != nil {
				return nil, err
			}
			return exec.CommandContext(ctx, shell, args...), nil
		}
		if node, flag, script, ok := directNodeEvalArgs(command); ok {
			return exec.CommandContext(ctx, node, flag, script), nil
		}
	}
	name, args := shellInvocation(command)
	return exec.CommandContext(ctx, name, args...), nil
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

func (c *cappedBuffer) Bytes() []byte  { return c.buf.Bytes() }
func (c *cappedBuffer) String() string { return c.buf.String() }

func reasonixHome(override string) string {
	if override != "" {
		return filepath.Join(override, SettingsDirname)
	}
	if dir := config.ReasonixHomeDir(); dir != "" {
		return dir
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, SettingsDirname)
	}
	return ""
}

func legacyGlobalSettingsPath(homeDir string) string {
	dir := legacyReasonixHome(homeDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, SettingsFilename)
}

func legacyTrustPath(homeDir string) string {
	dir := legacyReasonixHome(homeDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, TrustFilename)
}

func legacyReasonixHome(override string) string {
	if override != "" {
		return ""
	}
	if config.IsolatedHomeDir() != "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	legacy := filepath.Join(home, SettingsDirname)
	if sameCleanPath(legacy, reasonixHome("")) {
		return ""
	}
	return legacy
}

func sameCleanPath(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	if aa, err := filepath.Abs(a); err == nil {
		a = aa
	}
	if bb, err := filepath.Abs(b); err == nil {
		b = bb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
