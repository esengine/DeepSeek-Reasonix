package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// SettingsPath returns the file a scope's hooks are written to. A project scope
// with no root has no path — the caller must refuse rather than fall back to the
// global file, which would silently widen a project rule to every project.
func SettingsPath(scope Scope, projectRoot string) string {
	if scope == ScopeProject {
		if strings.TrimSpace(projectRoot) == "" {
			return ""
		}
		return ProjectSettingsPath(projectRoot)
	}
	return GlobalSettingsPath("")
}

// Save replaces the hooks block of one scope's settings.json. Every other key in
// that file is preserved: settings.json is shared with unrelated configuration,
// and a hooks editor has no business dropping it.
func Save(scope Scope, projectRoot string, settings Settings) error {
	path := SettingsPath(scope, projectRoot)
	if path == "" {
		return fmt.Errorf("no project workspace to save project hooks into")
	}
	hooks := map[Event][]HookConfig{}
	for event, list := range settings.Hooks {
		if !validEvent(event) {
			return fmt.Errorf("unknown hook event %q", event)
		}
		for _, cfg := range list {
			cmd := strings.TrimSpace(cfg.Command)
			if cmd == "" {
				// An empty command is a half-finished row, not a hook. Writing it
				// would produce a file Load silently skips and Inspect flags.
				continue
			}
			hooks[event] = append(hooks[event], HookConfig{
				Match:       strings.TrimSpace(cfg.Match),
				Command:     NormalizeCommand(cmd),
				Description: strings.TrimSpace(cfg.Description),
				Timeout:     cfg.Timeout,
				Cwd:         strings.TrimSpace(cfg.Cwd),
			})
		}
	}
	raw := map[string]json.RawMessage{}
	if body, err := fileencoding.ReadFileUTF8(path); err == nil {
		if err := json.Unmarshal(body, &raw); err != nil {
			return fmt.Errorf("%s is not valid JSON; fix it before saving hooks: %w", path, err)
		}
	}
	encoded, err := json.Marshal(hooks)
	if err != nil {
		return err
	}
	raw["hooks"] = encoded
	body, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, body, 0o644)
}

// DryRunResult is one trial invocation, in the terms the user asked the question
// in: not "exit 2" but "this would have stopped the agent".
type DryRunResult struct {
	Decision   Decision      `json:"decision"`
	ExitCode   int           `json:"exitCode"`
	Stdout     string        `json:"stdout,omitempty"`
	Stderr     string        `json:"stderr,omitempty"`
	TimedOut   bool          `json:"timedOut,omitempty"`
	Duration   time.Duration `json:"-"`
	DurationMs int64         `json:"durationMs"`
	// Blocks reports the consequence on this event specifically: the same exit
	// code stops a PreToolUse and only warns on a PostToolUse.
	Blocks bool `json:"blocks"`
}

// DryRun executes one hook against a representative payload so a broken command
// is found while editing rather than mid-task. It is a real execution — a hook
// with side effects will have them — which is why the caller must present it as
// running the command, not as validating it.
func DryRun(ctx context.Context, cfg HookConfig, event Event, cwd string, spawner Spawner) (DryRunResult, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return DryRunResult{}, fmt.Errorf("this hook has no command to run")
	}
	if !validEvent(event) {
		return DryRunResult{}, fmt.Errorf("unknown hook event %q", event)
	}
	if msg := ValidateMatcher(cfg.Match); msg != "" {
		return DryRunResult{}, fmt.Errorf("%s", msg)
	}
	cfg.Command = NormalizeCommand(strings.TrimSpace(cfg.Command))
	resolved := ResolvedHook{HookConfig: cfg, Event: event, Scope: ScopeGlobal}
	payload := samplePayload(event, cwd)
	report := Run(ctx, payload, []ResolvedHook{resolved}, spawner)
	if len(report.Outcomes) == 0 {
		// The matcher excluded the sample tool. That is a legitimate answer: this
		// hook would not have fired at all.
		return DryRunResult{Decision: DecisionPass, ExitCode: 0}, nil
	}
	o := report.Outcomes[0]
	return DryRunResult{
		Decision:   o.Decision,
		ExitCode:   o.ExitCode,
		Stdout:     o.Stdout,
		Stderr:     o.Stderr,
		TimedOut:   o.TimedOut,
		Duration:   o.Duration,
		DurationMs: o.Duration.Milliseconds(),
		Blocks:     o.Decision == DecisionBlock && IsBlocking(event),
	}, nil
}

// samplePayload builds a plausible payload for an event so the hook reads the
// same field names it will see in a real run.
func samplePayload(event Event, cwd string) Payload {
	p := Payload{Event: event, Cwd: cwd, SessionID: "dry-run"}
	switch event {
	case PreToolUse, PostToolUse, PostToolUseFailure, PermissionRequest:
		p.ToolName = "edit_file"
		p.ToolArgs = json.RawMessage(`{"path":"README.md"}`)
		p.Subject = "README.md"
		if event == PostToolUse {
			p.ToolResult = "ok"
		}
		if event == PostToolUseFailure {
			p.Error = "permission denied"
		}
	case UserPromptSubmit:
		p.Prompt = "把这个仓库跑一遍测试"
	case SubagentStart:
		p.CallID = "call_1"
		p.ToolArgs = json.RawMessage(`{"prompt":"把这个仓库跑一遍测试"}`)
	case SubagentStop:
		p.CallID = "call_1"
		p.LastAssistant = "测试全绿。"
	case Stop, StopFailure:
		p.LastAssistant = "测试全绿。"
		p.Turn = 1
	case PostLLMCall:
		p.Reasoning = "这是一次试跑，用来看 hook 会不会改写推理文本。"
	case Notification:
		p.Message = "有一个工具调用在等你批准"
		p.NotificationType = "approval"
	case PreCompact:
		p.Trigger = "manual"
	case SessionStart, SessionEnd:
		p.Source = "dry-run"
	}
	return p
}
