package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Runner binds a set of resolved hooks to a session: a working directory, the
// spawner, and a notify callback that surfaces non-blocking hook messages to the
// user. It is the single object the agent (tool events) and the controller
// (prompt/stop events) fire hooks through, so neither has to know how hooks load
// or run. A nil *Runner is a valid no-op (no hooks configured).
type Runner struct {
	hooks   []ResolvedHook
	cwd     string
	spawner Spawner
	notify  func(string) // surface a non-blocking (warn/error) hook message; may be nil
}

// NewRunner builds a Runner. spawner nil uses DefaultSpawner; notify nil drops
// non-blocking messages.
func NewRunner(hooks []ResolvedHook, cwd string, spawner Spawner, notify func(string)) *Runner {
	return &Runner{hooks: hooks, cwd: cwd, spawner: spawner, notify: notify}
}

// Hooks returns the resolved hooks (for `/hooks` listing).
func (r *Runner) Hooks() []ResolvedHook {
	if r == nil {
		return nil
	}
	return r.hooks
}

// Enabled reports whether any hooks are configured.
func (r *Runner) Enabled() bool { return r != nil && len(r.hooks) > 0 }

// Has reports whether any configured hook listens for the given event. Callers
// use it to skip work that only matters when a specific hook exists (e.g. the
// agent buffers reasoning for transform only when a PostLLMCall hook is set).
func (r *Runner) Has(event Event) bool {
	if r == nil {
		return false
	}
	for _, h := range r.hooks {
		if h.Event == event {
			return true
		}
	}
	return false
}

// HasPostLLMCall reports whether a PostLLMCall hook is configured, so the agent
// keeps streaming reasoning live unless a transform is actually wired up.
func (r *Runner) HasPostLLMCall() bool { return r.Has(PostLLMCall) }

// PreToolUse fires before a tool call. block=true means the call must be
// refused; message is the reason (fed back to the model and shown to the user).
func (r *Runner) PreToolUse(ctx context.Context, name string, args json.RawMessage) (block bool, message string) {
	if !r.Enabled() {
		return false, ""
	}
	rep := Run(ctx, Payload{Event: PreToolUse, Cwd: r.cwd, ToolName: name, ToolArgs: args}, r.hooks, r.spawner)
	return r.handle(rep)
}

// PostToolUse fires after a tool call. It can't block; non-pass outcomes are
// surfaced to the user via notify.
func (r *Runner) PostToolUse(ctx context.Context, name string, args json.RawMessage, result string) {
	if !r.Enabled() {
		return
	}
	rep := Run(ctx, Payload{Event: PostToolUse, Cwd: r.cwd, ToolName: name, ToolArgs: args, ToolResult: result}, r.hooks, r.spawner)
	r.handle(rep)
}

// PromptSubmit fires before a turn starts. block=true aborts the turn; message
// is the reason.
func (r *Runner) PromptSubmit(ctx context.Context, prompt string, turn int) (block bool, message string) {
	if !r.Enabled() {
		return false, ""
	}
	rep := Run(ctx, Payload{Event: UserPromptSubmit, Cwd: r.cwd, Prompt: prompt, Turn: turn}, r.hooks, r.spawner)
	return r.handle(rep)
}

// Stop fires after a turn finishes. It can't block.
func (r *Runner) Stop(ctx context.Context, lastAssistant string, turn int) {
	if !r.Enabled() {
		return
	}
	rep := Run(ctx, Payload{Event: Stop, Cwd: r.cwd, LastAssistant: lastAssistant, Turn: turn}, r.hooks, r.spawner)
	r.handle(rep)
}

// SessionStart fires when a session becomes active. It can't block; its purpose
// is setup side effects (logging, prepping the workspace, desktop notifications).
func (r *Runner) SessionStart(ctx context.Context) {
	if !r.Enabled() {
		return
	}
	r.handle(Run(ctx, Payload{Event: SessionStart, Cwd: r.cwd}, r.hooks, r.spawner))
}

// SessionEnd fires when a session is closed or rotated (/new). It can't block.
func (r *Runner) SessionEnd(ctx context.Context) {
	if !r.Enabled() {
		return
	}
	r.handle(Run(ctx, Payload{Event: SessionEnd, Cwd: r.cwd}, r.hooks, r.spawner))
}

// SubagentStop fires when a `task` sub-agent finishes. It can't block; last is
// the sub-agent's final answer.
func (r *Runner) SubagentStop(ctx context.Context, last string) {
	if !r.Enabled() {
		return
	}
	r.handle(Run(ctx, Payload{Event: SubagentStop, Cwd: r.cwd, LastAssistant: last}, r.hooks, r.spawner))
}

// Notification fires when the agent needs the user's attention (e.g. a pending
// approval). It can't block; message describes what's waiting.
func (r *Runner) Notification(ctx context.Context, message string) {
	if !r.Enabled() {
		return
	}
	r.handle(Run(ctx, Payload{Event: Notification, Cwd: r.cwd, Message: message}, r.hooks, r.spawner))
}

// PostLLMCall fires after every model turn completes but before the
// reasoning_content is stored in the session. It returns the hook's stdout as
// the new reasoning text, or the original reasoning if the hook passes with
// empty stdout / doesn't exist / fails. A non-pass outcome is surfaced via
// notify but doesn't block.
func (r *Runner) PostLLMCall(ctx context.Context, reasoning string, turn int) string {
	if !r.Has(PostLLMCall) {
		return reasoning
	}
	rep := Run(ctx, Payload{Event: PostLLMCall, Cwd: r.cwd, Reasoning: reasoning, Turn: turn}, r.hooks, r.spawner)
	r.handle(rep)
	for _, o := range rep.Outcomes {
		if o.Decision == DecisionPass {
			if s := strings.TrimSpace(o.Stdout); s != "" {
				return s
			}
		}
	}
	return reasoning
}

// PreCompact fires just before a compaction pass and returns the concatenated
// stdout of its hooks as extra summary guidance, so a hook can steer what the
// summary keeps. Non-pass outcomes are surfaced via notify.
func (r *Runner) PreCompact(ctx context.Context, trigger string) string {
	if !r.Enabled() {
		return ""
	}
	rep := Run(ctx, Payload{Event: PreCompact, Cwd: r.cwd, Trigger: trigger}, r.hooks, r.spawner)
	r.handle(rep)
	var b strings.Builder
	for _, o := range rep.Outcomes {
		if s := strings.TrimSpace(o.Stdout); s != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(s)
		}
	}
	return b.String()
}

// handle surfaces every non-pass outcome to the user (notify) and returns the
// block decision plus the blocking hook's message.
func (r *Runner) handle(rep Report) (bool, string) {
	var blockMsg string
	for _, o := range rep.Outcomes {
		if o.Decision == DecisionPass {
			continue
		}
		msg := FormatOutcome(o)
		if r.notify != nil {
			r.notify(msg)
		}
		if o.Decision == DecisionBlock {
			blockMsg = msg
		}
	}
	return rep.Blocked, blockMsg
}

// FormatOutcome renders a non-pass outcome as a one-line human message.
func FormatOutcome(o Outcome) string {
	detail := strings.TrimSpace(o.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(o.Stdout)
	}
	tag := string(o.Hook.Scope) + "/" + string(o.Hook.Event)
	cmd := clipRunes(o.Hook.Command, 60)
	trunc := ""
	if o.Truncated {
		trunc = " (output truncated)"
	}
	head := fmt.Sprintf("hook [%s] %s — %s%s", tag, cmd, o.Decision, trunc)
	if detail != "" {
		return head + ": " + detail
	}
	return head
}

func clipRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max < 1 {
		return ""
	}
	return string(r[:max]) + "…"
}
