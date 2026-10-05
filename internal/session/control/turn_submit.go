package control

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/skill"
)

// Submit is the one-call entry for a simple frontend: it takes raw user input
// and does everything — slash-command dispatch, @-reference expansion, plan-mode
// composition — emitting all output as events. The HTTP/SSE server uses this so
// a browser client only POSTs the typed line.
//
// Slash commands route to the matching primitive: /compact, /new, and /clear
// run their session op and emit a Notice; /mcp__server__prompt and custom /commands
// resolve to a turn; an unknown slash emits a Notice. Anything else is a normal
// turn with its @-references resolved first.
func (c *Controller) Submit(input string) {
	c.submit(input, "", "")
}

// SubmitHTTP accepts input from the unauthenticated localhost HTTP frontend. It
// deliberately omits the trusted TUI-only "!cmd" shell shortcut and resolves file
// references only through the controller's workspace root.
func (c *Controller) SubmitHTTP(input string) {
	c.submitHTTP(input, "")
}

// SubmitHTTPFormat is SubmitHTTP with an optional structured-output format
// ("json_object") applied to the turn's completion requests. Empty format
// behaves exactly like SubmitHTTP. A format attached to a slash command,
// or other non-turn input is discarded; @reference turns preserve it because
// the format is bound to every submitted turn rather than a global slot.
func (c *Controller) SubmitHTTPFormat(input, format string) {
	c.SubmitHTTPFrom(input, format, nil)
}

// SubmitHTTPFrom is SubmitHTTPFormat for input relayed from a paired device:
// the turn's message is landed and announced as that device's. A nil via is
// the window's own input.
func (c *Controller) SubmitHTTPFrom(input, format string, via *provider.Via) {
	// format 绑定到本次提交的 turn（随请求参数传递），不再写入 Controller
	// 全局一次性槽——评审 #7234 第 2 点：全局槽存在跨请求串用的逻辑竞态
	// （后提交的 JSON 请求先写槽，更早的普通请求先启动消费掉）。
	f := strings.TrimSpace(format)
	if f != "" && isNonTurnHTTPInput(input) {
		f = "" // 非 turn 输入（slash 命令/! 前缀）不携带 format
	}
	// @ 引用 turn（FileRefLine/SlashPathLineRef 等）同样绑定 format——
	// runRefTurnWithFormat 族 wrapper 注入 ctx（review fix7234and7168：
	// format 是每个被接纳 turn 的属性，统一架构）。
	c.submitHTTPWithFormat(input, "", turnTags{format: f, via: via})
}

// SubmitOptions are what a submission asks beyond its text.
type SubmitOptions struct {
	Format string
	Via    *provider.Via
	// RefuseUnknownSlash keeps a slash command no registry answers off the
	// model: the line gets an unknown-command notice and starts no turn.
	RefuseUnknownSlash bool
}

// SubmitHTTPOptions is SubmitHTTPFrom with every per-submission option spelled
// out, for a frontend that asks for more than a format and a device.
func (c *Controller) SubmitHTTPOptions(input string, opts SubmitOptions) {
	f := strings.TrimSpace(opts.Format)
	if f != "" && isNonTurnHTTPInput(input) {
		f = ""
	}
	c.submitHTTPWithFormat(input, "", turnTags{format: f, via: opts.Via, refuseUnknownSlash: opts.RefuseUnknownSlash})
}

// SubmitDisplay runs input as a turn while remembering the user-facing display
// text for transcript replay when controller-side composition expands input.
func (c *Controller) SubmitDisplay(display, input string) {
	c.submit(input, display, "")
}

// SubmitDeliveryRecovery runs the same visible prompt path as SubmitDisplay but
// first authorizes the executor to retain the immediately preceding exhausted
// delivery ledger. The agent consumes that authorization once; if the card came
// from an older/reloaded session this safely degrades to an ordinary turn.
func (c *Controller) SubmitDeliveryRecovery(display, input string) {
	c.runGuarded(func(ctx context.Context) error {
		if c.executor != nil {
			c.executor.PrepareDeliveryRecovery()
		}
		return c.runTurnLoop(ctx, orchestratedTurn{input: input, raw: input, display: display})
	})
}

// SubmitInvocationDisplay executes composer-selected invocation entities
// independently of slash-command parsing. Plain string submit entry points keep
// their existing behavior for CLI, HTTP, and backward-compatible clients.
func (c *Controller) SubmitInvocationDisplay(display, input string, invocations []InvocationRequest) {
	c.submitInvocations(input, display, invocations)
}

func (c *Controller) submitInvocations(input, display string, requests []InvocationRequest) {
	if len(requests) == 0 {
		c.SubmitDisplay(display, input)
		return
	}
	prepared, err := c.prepareInvocationTurn(input, requests)
	if err != nil {
		c.notice(err.Error())
		return
	}
	c.runGuarded(func(ctx context.Context) error {
		return c.runPreparedInvocationTurn(ctx, prepared, input, input, display, nil)
	})
}

func (c *Controller) prepareInvocationTurn(input string, requests []InvocationRequest) (preparedInvocationTurn, error) {
	ordered := append([]InvocationRequest(nil), requests...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Offset < ordered[j].Offset })
	inline := make([]skill.Skill, 0, len(ordered))
	subagents := make([]skill.Skill, 0, len(ordered))
	for _, request := range ordered {
		sk, _, ok := c.resolveSkillInvocation("/" + strings.TrimSpace(request.Name))
		if !ok {
			return preparedInvocationTurn{}, fmt.Errorf("unknown invocation: /%s", strings.TrimSpace(request.Name))
		}
		kind := "skill"
		if sk.RunAs == skill.RunSubagent {
			kind = "subagent"
		}
		if strings.TrimSpace(request.Kind) != "" && request.Kind != kind {
			return preparedInvocationTurn{}, fmt.Errorf("invocation /%s is %s, not %s", sk.SlashName(), kind, request.Kind)
		}
		if sk.RunAs == skill.RunSubagent {
			subagents = append(subagents, sk)
		} else {
			inline = append(inline, sk)
		}
	}

	if strings.TrimSpace(input) == "" && len(subagents) > 0 {
		return preparedInvocationTurn{}, fmt.Errorf("subagent invocation requires a task")
	}
	inlineSkillNames := make([]string, 0, len(inline))
	for _, sk := range inline {
		inlineSkillNames = append(inlineSkillNames, sk.Name)
	}
	// A lone inline skill takes the typed text as its arguments, as "/name task" does.
	if len(inline) == 1 && len(subagents) == 0 {
		return preparedInvocationTurn{composed: c.skills.renderInvocation(inline[0], strings.TrimSpace(input)), inlineSkillNames: inlineSkillNames}, nil
	}
	parts := make([]string, 0, len(inline)+1)
	for _, sk := range inline {
		parts = append(parts, c.skills.renderInvocation(sk, ""))
	}
	if strings.TrimSpace(input) != "" {
		parts = append(parts, input)
	}
	return preparedInvocationTurn{composed: strings.Join(parts, "\n\n"), subagents: subagents, inlineSkillNames: inlineSkillNames}, nil
}

func (c *Controller) runPreparedInvocationTurn(
	ctx context.Context,
	prepared preparedInvocationTurn,
	input, raw, display string,
	frozenImages []string,
) error {
	ctx = withInvokedSkills(ctx, prepared.inlineSkillNames)
	if len(prepared.subagents) == 0 {
		return c.runTurnLoop(ctx, orchestratedTurn{
			input: prepared.composed, raw: raw, display: display, images: c.frozenTurnImages(frozenImages),
		})
	}
	runner := c.skillRunner
	if runner == nil {
		return fmt.Errorf("subagent skill runner is unavailable")
	}
	return newTurnOrchestrator(c).runSubagentSkillTurnsGoalLoop(
		ctx,
		prepared.subagents,
		prepared.composed,
		input,
		display,
		runner,
		c.PlanMode(),
	)
}

// SubmitEditedDisplay is SubmitDisplay for an inline-edited prompt. The model
// sees input; the saved user message also keeps the pre-edit prompt as local UI
// metadata so the edit survives session rewrites.
func (c *Controller) SubmitEditedDisplay(display, input, original string) {
	c.submit(input, display, original)
}

// SubmitUserTurn starts a normal model turn without interpreting shell or slash
// commands. It still resolves references, so callers can submit trusted
// user-authored prompt text without expanding the command surface.
func (c *Controller) SubmitUserTurn(display, input string) {
	c.runRefTurn(refTurn{input: input, display: display})
}

func (c *Controller) submit(input, display, editedOriginal string) {
	trimmed := strings.TrimSpace(input)
	if note, ok := MemoryQuickAddNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if note, ok := RememberCommandNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if c.applyGoalCommand(trimmed, display) {
		return
	}
	if strings.HasPrefix(trimmed, "!") {
		c.RunShell(trimmed[1:])
		return
	}
	c.submitCommandOrTurn(trimmed, input, display, false, editedOriginal, turnTags{})
}

func (c *Controller) submitHTTP(input, display string) {
	c.submitHTTPWithFormat(input, display, turnTags{})
}

func (c *Controller) submitHTTPWithFormat(input, display string, tags turnTags) {
	trimmed := strings.TrimSpace(input)
	if note, ok := MemoryQuickAddNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if note, ok := RememberCommandNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if c.applyGoalCommand(trimmed, display) {
		return
	}
	if strings.HasPrefix(trimmed, "!") {
		c.notice("shell commands are unavailable from this frontend")
		return
	}
	c.submitCommandOrTurn(trimmed, input, display, true, "", tags)
}

// refTurnBase is the shape every ref turn from one submitted line shares. An
// edited resubmit resolves against the whole workspace and rides the
// edited-goal loop, so it overrides scopedRefsOnly rather than combining with
// it.
func (c *Controller) refTurnBase(display, editedOriginal string, tags turnTags, scopedRefsOnly bool) refTurn {
	base := refTurn{display: display, original: editedOriginal, tags: tags}
	if scopedRefsOnly && strings.TrimSpace(editedOriginal) == "" {
		base.resolve = c.ResolveScopedRefs
	}
	return base
}

// turnLoopRunner is the loop that same line runs in: an edited resubmit rides
// the edited-goal loop, everything else the plain one with its format bound.
func (c *Controller) turnLoopRunner(editedOriginal string, tags turnTags) func(context.Context, string, string, string) error {
	if strings.TrimSpace(editedOriginal) != "" {
		return func(ctx context.Context, input, raw, display string) error {
			return c.runTurnLoop(withTurnVia(ctx, tags.via), orchestratedTurn{
				input: input, raw: raw, display: display, editedOriginal: editedOriginal,
			})
		}
	}
	return func(ctx context.Context, input, raw, display string) error {
		return c.runTurnLoop(c.withTurnTags(ctx, tags), orchestratedTurn{input: input, raw: raw, display: display})
	}
}

func (c *Controller) submitCommandOrTurnReady(trimmed, input, display string, scopedRefsOnly bool, editedOriginal string, tags turnTags) {
	base := c.refTurnBase(display, editedOriginal, tags, scopedRefsOnly)
	runRefTurn := func(input, display string) {
		r := base
		r.input, r.display = input, display
		c.runRefTurn(r)
	}
	runRefTurnWithRefs := func(input, refLine, display string) {
		r := base
		r.input, r.refLine, r.display = input, refLine, display
		c.runRefTurn(r)
	}
	runTurnLoop := c.turnLoopRunner(editedOriginal, tags)
	switch {
	case trimmed == "/compact" || strings.HasPrefix(trimmed, "/compact "):
		go c.compactAndReport(strings.TrimSpace(strings.TrimPrefix(trimmed, "/compact")))
	case slashWord(trimmed) == "/context":
		c.reportContext()
	case slashWord(trimmed) == "/new":
		c.runSessionVerb(c.NewSession, i18n.M.SlashNewDone, i18n.M.SlashNewFailed+": ")
	case slashWord(trimmed) == "/clear":
		c.runSessionVerb(c.ClearSession, i18n.M.SlashClearDone, i18n.M.SlashClearFailed+": ")
	case strings.HasPrefix(trimmed, "/mcp__"):
		c.runGuarded(func(ctx context.Context) error {
			sent, found, err := c.MCPPrompt(ctx, trimmed)
			if err != nil {
				return err
			}
			if !found {
				c.notice("unknown command: " + trimmed)
				return nil
			}
			return runTurnLoop(ctx, sent, sent, display)
		})
	case SlashCodeCommentLine(trimmed):
		// Slash-prefixed code comments are prompt text, not slash commands.
		runRefTurn(input, display)
	case strings.HasPrefix(trimmed, "/"):
		if ref, ok := FileRefLine(trimmed); ok {
			runRefTurn(ref, display)
			return
		}
		if ref, ok := SlashPathLineRef(trimmed, c.workspaceRoot); ok {
			runRefTurnWithRefs(input, ref, display)
			return
		}
		if SlashPathLikeLine(trimmed) {
			runRefTurn(input, display)
			return
		}
		// Management verbs (/model /memory /skills /hooks /mcp) emit a Notice, so
		// Submit-based frontends (desktop, HTTP) get them with no extra wiring.
		// The chat TUI handles these itself with richer output.
		fields := strings.Fields(trimmed)
		switch fields[0] {
		case "/tree":
			c.notice(c.BranchTreeText())
			return
		case "/branch":
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			if _, err := c.Branch(name); err != nil {
				c.notice(err.Error())
			}
			return
		case "/switch":
			ref := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			if _, err := c.SwitchBranch(ref); err != nil {
				c.notice(err.Error())
			}
			return
		case "/rewind":
			args := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			turn, scope, err := parseRewind(args, c.Checkpoints())
			if err != nil {
				c.notice("usage: /rewind [turn] [code|conversation|both]")
				return
			}
			if err := c.Rewind(turn, scope); err != nil {
				c.notice(err.Error())
			}
			return
		case "/plan-exec":
			c.applyPlanExec(trimmed, display)
			return
		case "/prometheus":
			c.applyPrometheus(trimmed, display)
			return
		}
		if c.managementNotice(trimmed) {
			return
		}
		if IsBuiltinDocsSlash(fields[0], c.Commands(), c.SlashSkills()) {
			query := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			if query == "" {
				text, err := DocsCommandOverviewFor(fields[0])
				if err != nil {
					c.notice("docs: " + err.Error())
				} else {
					c.notice(text)
				}
				return
			}
			c.runGuarded(func(ctx context.Context) error {
				sent, err := docsCommandPrompt(ctx, query)
				if err != nil {
					return fmt.Errorf("docs: %w", err)
				}
				return runTurnLoop(ctx, sent, sent, display)
			})
			return
		}
		// A custom command wins over a skill of the same name; both resolve to a
		// turn. Built-ins and their explicit Reasonix namespace are handled above.
		if sent, ok := c.CustomCommand(trimmed); ok {
			c.runGuarded(func(ctx context.Context) error {
				return runTurnLoop(ctx, sent, sent, display)
			})
			return
		}
		if sk, task, ok := c.resolveSkillInvocation(trimmed); ok {
			if sk.RunAs == skill.RunSubagent {
				if strings.TrimSpace(task) == "" {
					c.notice("usage: /" + sk.Name + " <task>")
					return
				}
				c.runSubagentSkillSlash(sk, task, trimmed, display)
				return
			}
			sent := c.skills.renderInvocation(sk, task)
			c.runGuarded(func(ctx context.Context) error {
				return runTurnLoop(withInvokedSkills(ctx, []string{sk.Name}), sent, input, display)
			})
			return
		}
		c.answerUnresolvedSlash(fields[0], tags.refuseUnknownSlash, func() { runRefTurn(input, display) })
	default:
		runRefTurn(input, display)
	}
}

// slashWord is the command a line names: its first space-delimited word, which
// is all a built-in's identity rests on whatever arguments follow.
func slashWord(line string) string {
	word, _, _ := strings.Cut(line, " ")
	return word
}

// answerUnresolvedSlash settles a slash line nothing resolved. Unknown slash
// input is prose more often than a typo ("/etc/hosts looks wrong", pasted paths,
// half-remembered commands), so it is sent as a regular message with a notice
// that keeps real typos visible (#5756) — unless the submitter asked to refuse.
func (c *Controller) answerUnresolvedSlash(cmd string, refuse bool, send func()) {
	if refuse {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeUnknownCommand,
			Text: i18n.M.SlashUnknown + ": " + cmd})
		return
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeUnknownCommand,
		Text: i18n.M.SlashUnknown + ": " + cmd + " — " + i18n.M.SlashUnknownSentAsMessage})
	send()
}

func (c *Controller) applyGoalCommand(input, display string) bool {
	cmd, ok := ParseGoalCommand(input)
	if !ok {
		return false
	}
	if cmd.DeprecatedBudgetFlag {
		c.notice(GoalBudgetFlagDeprecatedNotice)
	}
	switch cmd.Action {
	case GoalCommandSet:
		c.SetPlanMode(false)
		c.SetGoalWithResearchMode(cmd.Text, cmd.ResearchMode)
		c.GoalStrict(cmd.Strict)
		c.startGoalCommandTurn(cmd, display)
	case GoalCommandClear:
		c.ClearGoal()
		c.notice(i18n.M.GoalCleared)
	case GoalCommandPause:
		if !c.PauseGoal() {
			c.notice(i18n.M.GoalNotRunning)
		}
	case GoalCommandResume:
		if !c.ResumeGoal() {
			c.notice(i18n.M.GoalNotPaused)
		}
	default:
		goal := c.Goal()
		if strings.TrimSpace(goal) == "" {
			c.notice(i18n.M.GoalEmpty)
			break
		}
		rt := c.GoalRuntime()
		c.notice(fmt.Sprintf(i18n.M.GoalCurrentFmt, goal))
		c.notice(fmt.Sprintf(i18n.M.GoalRuntimeFmt,
			rt.TurnsUsed, rt.RequestsUsed, rt.TokensUsed,
			GoalWorkDurationText(rt.WorkDurationMs)))
		if rt.LastReason != "" {
			c.noticeDetail(i18n.M.GoalRuntimeLastReason, rt.LastReason)
		}
		if rt.StopCause != "" {
			c.notice(fmt.Sprintf(i18n.M.GoalPausedFmt, rt.StopCause))
		}
	}
	return true
}

// applyPlanExec reads the current canonical todo list and starts a goal that
// analyzes and dispatches independent steps concurrently via parallel_tasks.
// Supports --strict flag: /plan-exec --strict enables strict goal mode.
func (c *Controller) applyPlanExec(input, display string) {
	todos := c.executor.CanonicalTodoState()
	if len(todos) == 0 {
		c.notice("no active plan with todos to execute")
		return
	}

	// Parse --strict flag.
	strict := slices.Contains(strings.Fields(input), "--strict")

	// Count completion status.
	total := len(todos)
	done := 0
	for _, t := range todos {
		if t.Status == "completed" {
			done++
		}
	}

	var b strings.Builder
	b.WriteString("You are the execution conductor. Route each step to the right sub-agent by module.\n\n")

	// Detect project structure for module-aware routing.
	modules := c.detectProjectModules()
	if len(modules) > 0 {
		b.WriteString("## Project modules detected\n\n")
		for _, m := range modules {
			fmt.Fprintf(&b, "- %s/", m)
		}
		b.WriteString("\n\nRoute steps to the module they belong to. Steps in different modules can run in parallel.\n\n")
	}

	b.WriteString("## Plan steps\n\n")
	for _, t := range todos {
		status := t.Status
		if status == "" {
			status = "pending"
		}
		mark := " "
		if status == "completed" {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s (%s)\n", mark, t.Content, status)
	}
	b.WriteString("\n## Routing rules\n")
	b.WriteString("1. Group steps by MODULE \u2014 same module = serial, different modules = parallel batches\n")
	b.WriteString("2. Research/exploration across modules = use parallel_tasks\n")
	b.WriteString("3. Dispatch each batch via parallel_tasks \u2014 each sub-agent gets one module\u2019s context\n")
	b.WriteString("4. Verify each batch before the next\n")
	b.WriteString("5. Failures: fix before moving on\n")
	b.WriteString("\nGoal: each sub-agent focuses on one module and does not carry irrelevant context.\n")
	if done > 0 {
		fmt.Fprintf(&b, "\nNote: %d/%d steps are already completed. Focus on the remaining %d steps.\n", done, total, total-done)
	}
	prompt := b.String()

	// Show module preview.
	if len(modules) > 0 {
		c.notice(fmt.Sprintf("plan-exec: detected %d modules — %s", len(modules), strings.Join(modules, ", ")))
	}

	c.SetPlanMode(false)
	c.SetGoal("execute plan: " + ShortGoalForNotice(todos[0].Content))
	c.GoalStrict(strict)
	c.notice(fmt.Sprintf("plan-exec: dispatching %d plan steps (strict=%v)", total, strict))
	if c.runner != nil {
		c.runGuarded(func(ctx context.Context) error {
			return c.runTurnLoop(ctx, orchestratedTurn{input: prompt, raw: prompt, display: display})
		})
	}
}
