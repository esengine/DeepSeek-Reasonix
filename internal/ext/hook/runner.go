package hook

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Runner binds a set of resolved hooks to a session: a working directory, the
// spawner, and a notify callback that surfaces non-blocking hook messages to the
// user. It is the single object the agent (tool events) and the controller
// (prompt/stop events) fire hooks through, so neither has to know how hooks load
// or run. A nil *Runner is a valid no-op (no hooks configured).
type Runner struct {
	set       *hookSet
	cwd       string
	spawner   Spawner
	notify    func(Notice) // surface a non-pass hook outcome; may be nil
	mu        sync.RWMutex
	sessionID string
	parent    *Runner // set by ForRole: the session id is read from it at fire time
	role      string
}

// hookSet is what a session and every runner derived from it with ForRole
// fire, so an edit reaches them all and a repeat is judged once for the user.
type hookSet struct {
	mu    sync.RWMutex
	hooks []ResolvedHook
	// lastOutcome holds the message each hook reported last, so a repeat of the
	// same one is not surfaced again. Keyed by hook, so it is bounded by the
	// configured hook count rather than by how often they run.
	lastOutcome map[string]string
}

// SetSessionID updates the Claude-compatible session identifier used in hook
// payloads. It is safe to call when a controller rotates sessions. A runner from
// ForRole ignores it: its id is always derived from its parent's.
func (r *Runner) SetSessionID(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.sessionID = id
	r.mu.Unlock()
}

func (r *Runner) payload(event Event) Payload {
	return Payload{Event: event, Cwd: r.cwd, SessionID: r.currentSessionID()}
}

func (r *Runner) currentSessionID() string {
	if r.parent != nil {
		if id := r.parent.currentSessionID(); id != "" {
			return id + ":" + r.role
		}
		return r.role
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessionID
}

// NewRunner builds a Runner. spawner nil uses DefaultSpawner; notify nil drops
// non-blocking messages.
func NewRunner(hooks []ResolvedHook, cwd string, spawner Spawner, notify func(Notice)) *Runner {
	return &Runner{set: &hookSet{hooks: hooks}, cwd: cwd, spawner: spawner, notify: notify}
}

// ForRole is a runner for an agent working under this session in its own
// conversation — a planner, a guardian, a subagent — firing the same live hook
// set in the same environment. Its session id is this runner's id at fire time
// suffixed with ":"+role, so a rotation of the parent reaches it.
func (r *Runner) ForRole(role string) *Runner {
	if r == nil {
		return nil
	}
	return &Runner{set: r.set, cwd: r.cwd, spawner: r.spawner, notify: r.notify, parent: r, role: role}
}

// Hooks returns the resolved hooks (for `/hooks` listing).
func (r *Runner) Hooks() []ResolvedHook { return r.snapshot() }

// Replace swaps the hook set a live session fires. Editing hooks has to reach
// the open session: a rule the user just fixed that only applies after a restart
// is indistinguishable from one that does not work.
func (r *Runner) Replace(hooks []ResolvedHook) {
	if r == nil {
		return
	}
	r.set.mu.Lock()
	r.set.hooks = hooks
	// Edited hooks report from a clean slate: a message suppressed as a repeat
	// of the old configuration says something new about the new one.
	r.set.lastOutcome = nil
	r.set.mu.Unlock()
}

// Spawner is the interpreter binding this session's hooks already run under, so
// a dry run resolves the same bash the real invocation would.
func (r *Runner) Spawner() Spawner {
	if r == nil {
		return nil
	}
	return r.spawner
}

// snapshot reads the hook set under the lock, since Replace may run while a turn
// is firing hooks.
func (r *Runner) snapshot() []ResolvedHook {
	if r == nil {
		return nil
	}
	r.set.mu.RLock()
	defer r.set.mu.RUnlock()
	return r.set.hooks
}

// Enabled reports whether any hooks are configured.
func (r *Runner) Enabled() bool { return len(r.snapshot()) > 0 }

// Has reports whether any configured hook listens for the given event. Callers
// use it to skip work that only matters when a specific hook exists (e.g. the
// agent buffers reasoning for transform only when a PostLLMCall hook is set).
func (r *Runner) Has(event Event) bool {
	if r == nil {
		return false
	}
	for _, h := range r.snapshot() {
		if h.Event == event {
			return true
		}
	}
	return false
}

// HasPostLLMCall reports whether a PostLLMCall hook is configured, so the agent
// keeps streaming reasoning live unless a transform is actually wired up.
func (r *Runner) HasPostLLMCall() bool { return r.Has(PostLLMCall) }

// ToolMutationHooksEnabled reports whether any hook runs around a tool call.
// These hooks execute user shell code and may mutate paths that the tool itself
// does not declare, so checkpoint coverage must account for them.
func (r *Runner) ToolMutationHooksEnabled() bool {
	return r.Has(PreToolUse) || r.Has(PostToolUse) || r.Has(PostToolUseFailure)
}

// PreToolUse fires before a tool call. block=true means the call must be
// refused; message is the reason (fed back to the model and shown to the user).
func (r *Runner) PreToolUse(ctx context.Context, name string, args json.RawMessage) (block bool, message string) {
	if !r.Enabled() {
		return false, ""
	}
	p := r.payload(PreToolUse)
	p.ToolName, p.ToolArgs = name, args
	rep := Run(ctx, p, r.snapshot(), r.spawner)
	return r.handle(rep)
}

// PostToolUse fires after a tool call. It can't block; non-pass outcomes are
// surfaced to the user via notify.
func (r *Runner) PostToolUse(ctx context.Context, name string, args json.RawMessage, result string) {
	if !r.Enabled() {
		return
	}
	p := r.payload(PostToolUse)
	p.ToolName, p.ToolArgs, p.ToolResult = name, args, result
	rep := Run(ctx, p, r.snapshot(), r.spawner)
	r.handle(rep)
}

// PostToolUseFailure fires when a tool invocation returns an error.
func (r *Runner) PostToolUseFailure(ctx context.Context, name string, args json.RawMessage, result string, err error) {
	if !r.Enabled() {
		return
	}
	p := r.payload(PostToolUseFailure)
	p.ToolName, p.ToolArgs, p.ToolResult = name, args, result
	if err != nil {
		p.Error = err.Error()
		p.IsInterrupt = errors.Is(err, context.Canceled)
	}
	r.handle(Run(ctx, p, r.snapshot(), r.spawner))
	// Native Reasonix PostToolUse historically observed both success and
	// failure. Preserve that contract while Claude hooks use the distinct event.
	legacy := r.nativeHooks(PostToolUse)
	if len(legacy) > 0 {
		p.Event = PostToolUse
		r.handle(Run(ctx, p, legacy, r.spawner))
	}
}

// PermissionRequest fires before a tool approval prompt is shown. A native
// Reasonix hook here can't answer the dialog (non-pass outcomes are surfaced
// via notify only); a Claude-imported hook (PayloadFormat "claude") can
// answer it on the user's behalf via exit 2 or a JSON decision, matching
// Claude's own contract. decision == nil means "no opinion, show the prompt
// normally"; a non-nil decision means the caller should skip the prompt and
// treat it as denied (false) or auto-approved (true).
func (r *Runner) PermissionRequest(ctx context.Context, name, subject string, args json.RawMessage) (decision *bool, message string) {
	if !r.Enabled() {
		return nil, ""
	}
	p := r.payload(PermissionRequest)
	p.ToolName, p.ToolArgs, p.Subject = name, args, subject
	rep := Run(ctx, p, r.snapshot(), r.spawner)
	block, msg := r.handle(rep)
	switch {
	case block:
		deny := false
		return &deny, msg
	case rep.Allowed:
		allow := true
		return &allow, msg
	default:
		return nil, msg
	}
}

// PromptSubmit fires before a turn starts. block=true aborts the turn; message
// is the reason.
func (r *Runner) PromptSubmit(ctx context.Context, prompt string, turn int) (block bool, message string) {
	if !r.Enabled() {
		return false, ""
	}
	p := r.payload(UserPromptSubmit)
	p.Prompt, p.Turn = prompt, turn
	rep := Run(ctx, p, r.snapshot(), r.spawner)
	return r.handle(rep)
}

// Stop fires after a turn finishes. It can't block.
func (r *Runner) Stop(ctx context.Context, lastAssistant string, turn int) {
	if !r.Enabled() {
		return
	}
	p := r.payload(Stop)
	p.LastAssistant, p.Turn = lastAssistant, turn
	rep := Run(ctx, p, r.snapshot(), r.spawner)
	r.handle(rep)
}

// StopResult emits Stop on success and StopFailure when the turn failed.
func (r *Runner) StopResult(ctx context.Context, lastAssistant string, turn int, err error) {
	if err == nil {
		r.Stop(ctx, lastAssistant, turn)
		return
	}
	if !r.Enabled() {
		return
	}
	p := r.payload(StopFailure)
	p.LastAssistant, p.Turn, p.Error = lastAssistant, turn, err.Error()
	p.IsInterrupt = errors.Is(err, context.Canceled)
	r.handle(Run(ctx, p, r.snapshot(), r.spawner))
	legacy := r.nativeHooks(Stop)
	if len(legacy) > 0 {
		p.Event = Stop
		r.handle(Run(ctx, p, legacy, r.spawner))
	}
}

func (r *Runner) nativeHooks(event Event) []ResolvedHook {
	var out []ResolvedHook
	for _, h := range r.snapshot() {
		if h.Event == event && h.PayloadFormat != "claude" {
			out = append(out, h)
		}
	}
	return out
}

// SessionStart fires when a session becomes active. It can't block; successful
// stdout may contribute one-shot context for the next model request.
func (r *Runner) SessionStart(ctx context.Context, source ...string) []string {
	if !r.Enabled() {
		return nil
	}
	p := r.payload(SessionStart)
	p.Source = "startup"
	if len(source) > 0 && strings.TrimSpace(source[0]) != "" {
		p.Source = strings.TrimSpace(source[0])
	}
	rep := Run(ctx, p, r.snapshot(), r.spawner)
	r.handle(rep)
	return r.additionalContexts(rep)
}

// SessionEnd fires when a session is closed or rotated (/new). It can't block.
func (r *Runner) SessionEnd(ctx context.Context, reason ...string) {
	if !r.Enabled() {
		return
	}
	p := r.payload(SessionEnd)
	p.Reason = "other"
	if len(reason) > 0 && strings.TrimSpace(reason[0]) != "" {
		p.Reason = strings.TrimSpace(reason[0])
	}
	r.handle(Run(ctx, p, r.snapshot(), r.spawner))
}

// SubagentStart fires before a foreground `task` sub-agent runs. It can't block;
// callID is the task call's id, which the matching SubagentStop carries too.
func (r *Runner) SubagentStart(ctx context.Context, callID string, args json.RawMessage) {
	if !r.Enabled() {
		return
	}
	p := r.payload(SubagentStart)
	p.CallID, p.ToolArgs = callID, args
	r.handle(Run(ctx, p, r.snapshot(), r.spawner))
}

// SubagentStop fires when a foreground `task` sub-agent ends, whether it
// answered, failed, was cancelled or refused. It can't block; err is why it
// did not answer.
func (r *Runner) SubagentStop(ctx context.Context, callID, last string, err error) {
	if !r.Enabled() {
		return
	}
	p := r.payload(SubagentStop)
	p.CallID, p.LastAssistant = callID, last
	if err != nil {
		p.Error = err.Error()
	}
	r.handle(Run(ctx, p, r.snapshot(), r.spawner))
}

// Notification fires when the agent needs the user's attention (e.g. a pending
// approval). It can't block; message describes what's waiting.
func (r *Runner) Notification(ctx context.Context, message string, notificationType ...string) {
	if !r.Enabled() {
		return
	}
	p := r.payload(Notification)
	p.Message = message
	if len(notificationType) > 0 {
		p.NotificationType = strings.TrimSpace(notificationType[0])
	}
	r.handle(Run(ctx, p, r.snapshot(), r.spawner))
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
	p := r.payload(PostLLMCall)
	p.Reasoning, p.Turn = reasoning, turn
	rep := Run(ctx, p, r.snapshot(), r.spawner)
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
	p := r.payload(PreCompact)
	p.Trigger = trigger
	rep := Run(ctx, p, r.snapshot(), r.spawner)
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

func (r *Runner) additionalContexts(rep Report) []string {
	var contexts []string
	for _, o := range rep.Outcomes {
		if o.Decision != DecisionPass {
			continue
		}
		out, warnings := ParseOutput(rep.Event, o.Stdout)
		for _, warning := range warnings {
			if r.notify != nil {
				r.notify(DescribeOutcome(Outcome{
					Hook:     o.Hook,
					Decision: DecisionWarn,
					Stdout:   warning,
				}))
			}
		}
		if out.AdditionalContext != "" {
			contexts = append(contexts, out.AdditionalContext)
		}
	}
	return contexts
}

// handle surfaces every non-pass outcome to the user (notify) and returns the
// block decision plus the blocking hook's message.
func (r *Runner) handle(rep Report) (bool, string) {
	var blockMsg string
	for _, o := range rep.Outcomes {
		if o.Decision == DecisionPass {
			continue
		}
		n := DescribeOutcome(o)
		msg := FormatOutcome(o)
		// A block always surfaces: it is how the user learns why the turn stopped.
		// A repeat does not — a hook broken on this host says the same thing
		// before every tool call, and the second copy adds nothing.
		if r.notify != nil && (o.Decision == DecisionBlock || r.outcomeChanged(o, msg)) {
			r.notify(n)
		}
		if o.Decision == DecisionBlock {
			blockMsg = msg
		}
	}
	return rep.Blocked, blockMsg
}

// outcomeChanged reports whether this hook is saying something different from
// what it said last time, recording the message either way.
func (r *Runner) outcomeChanged(o Outcome, msg string) bool {
	key := strings.Join([]string{string(o.Hook.Scope), string(o.Hook.Event), o.Hook.Source, o.Hook.Command}, "\x00")
	r.set.mu.Lock()
	defer r.set.mu.Unlock()
	if r.set.lastOutcome == nil {
		r.set.lastOutcome = map[string]string{}
	}
	previous, seen := r.set.lastOutcome[key]
	r.set.lastOutcome[key] = msg
	return !seen || previous != msg
}

// Notice is one outcome as a surface renders it: a headline naming the hook, the
// detail behind it, and a stable code for frontends that localize. The parts
// stay apart because the surfaces differ — a card puts the detail under the
// headline, a terminal puts it on the next line.
type Notice struct {
	// Decision is carried rather than a notice code: this package knows what a
	// hook did, and the frontends' own vocabulary for saying so is theirs.
	Decision Decision
	Text     string
	Detail   string
}

// DescribeOutcome turns a non-pass outcome into what a person needs: which hook,
// what happened to it, and its own words underneath. The command is not the
// headline — the reader wrote it, and sixty clipped characters of their own
// script identify it worse than the event and the label they gave it do.
func DescribeOutcome(o Outcome) Notice {
	return Notice{Decision: o.Decision, Text: outcomeHeadline(o), Detail: outcomeDetail(o)}
}

// outcomeHeadline is English by contract: frontends localize by Code and fall
// back to this. It names the hook by the label its author gave it, and by the
// event and scope when they gave it none.
func outcomeHeadline(o Outcome) string {
	name := strings.TrimSpace(o.Hook.Description)
	if name == "" {
		name = string(o.Hook.Event)
	}
	where := string(o.Hook.Scope)
	switch {
	case o.TimedOut:
		return fmt.Sprintf("%s hook (%s) ran out of time", name, where)
	case o.Decision == DecisionBlock:
		return fmt.Sprintf("%s hook (%s) stopped this call", name, where)
	case o.Decision == DecisionError:
		return fmt.Sprintf("%s hook (%s) could not run", name, where)
	default:
		return fmt.Sprintf("%s hook (%s) reported a problem", name, where)
	}
}

// outcomeDetail leads with the hook's own words, then names the command that
// produced them. The command is whole here: this is the place a reader goes
// looking for it, and a clipped one sends them to the settings file anyway.
func outcomeDetail(o Outcome) string {
	var parts []string
	if said := strings.TrimSpace(cmp.Or(o.Stderr, o.Stdout)); said != "" {
		if o.Truncated {
			said += " (output truncated)"
		}
		parts = append(parts, said)
	}
	if cmd := strings.TrimSpace(o.Hook.Command); cmd != "" {
		parts = append(parts, "command: "+cmd)
	} else if o.Hook.ContextFile != "" {
		parts = append(parts, "context: "+o.Hook.ContextFile)
	}
	if src := strings.TrimSpace(o.Hook.Source); src != "" {
		parts = append(parts, "source: "+src)
	}
	return strings.Join(parts, "\n")
}

// FormatOutcome renders a non-pass outcome as one line, for surfaces that have
// only one. It is the same words as the Notice, joined.
func FormatOutcome(o Outcome) string {
	n := DescribeOutcome(o)
	if n.Detail == "" {
		return n.Text
	}
	return n.Text + " — " + strings.ReplaceAll(n.Detail, "\n", " · ")
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
