package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"reasonix/internal/evidence"
	"reasonix/internal/i18n"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
)

// goalStatePath derives a session's persisted goal-state sidecar.
func goalStatePath(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return strings.TrimSuffix(sessionPath, ".jsonl") + ".goal-state.json"
}

func (c *Controller) runGoalLoopWithRaw(ctx context.Context, input, raw string) error {
	return c.runGoalLoopWithRawDisplay(ctx, input, raw, "")
}

func (c *Controller) runGoalLoopWithRawDisplay(ctx context.Context, input, raw, display string) error {
	if err := c.runTurnWithRawDisplay(ctx, input, raw, display); err != nil {
		if ctx.Err() != nil {
			c.stopGoal(GoalStatusStopped)
		}
		return err
	}
	return c.continueGoal(ctx)
}

func (c *Controller) continueGoal(ctx context.Context) error {
	for {
		cont := c.advanceGoalAfterTurn()
		if !cont {
			return nil
		}
		if err := ctx.Err(); err != nil {
			c.stopGoal(GoalStatusStopped)
			return err
		}
		turn := goalContinueTurn
		c.mu.Lock()
		if c.goalInterceptMsg != "" {
			turn = c.goalInterceptMsg
			c.goalInterceptMsg = ""
			c.mu.Unlock()
			c.notice("goal intercept: incomplete todos remain (override with a second [goal:complete])")
		} else {
			c.mu.Unlock()
		}
		if err := c.runTurnWithRawDisplay(ctx, turn, turn, ""); err != nil {
			if ctx.Err() != nil {
				c.stopGoal(GoalStatusStopped)
			}
			return err
		}
	}
}

func (c *Controller) advanceGoalAfterTurn() bool {
	reply := lastAssistantText(c.History())
	status, reason, _ := parseGoalStatusMarker(reply)
	var notice string
	c.mu.Lock()
	if strings.TrimSpace(c.goal) == "" || c.goalStatus != GoalStatusRunning {
		c.mu.Unlock()
		return false
	}
	c.goalTurns++
	switch status {
	case GoalStatusComplete:
		if incomplete := c.incompleteGoalTodos(); len(incomplete) > 0 && (c.goalStrict || c.goalIntercepts == 0) {
			// In strict mode every claim is blocked until todos are done;
			// otherwise only the first consecutive claim is intercepted.
			c.goalIntercepts++
			c.goalInterceptMsg = incomplete
			break
		}
		// Todos are all done — in strict mode run self-check before final
		// completion. Non-strict mode completes immediately.
		if c.goalStrict && !c.goalSelfCheckDone {
			c.goalSelfCheckDone = true
			c.goalInterceptMsg = goalSelfCheckTurn
			break
		}
		// Self-check passed — complete the goal.
		c.goalIntercepts = 0
		c.goalSelfCheckDone = false
		c.goalIdleTurns = 0
		c.saveGoalState()
		c.goal = ""
		c.goalStatus = GoalStatusComplete
		c.goalBlocks = 0
		c.goalBlock = ""
		c.goalInterceptMsg = ""
		notice = "goal complete"
	case GoalStatusBlocked:
		reason = cleanGoalBlockReason(reason)
		if reason == "" {
			reason = "blocked"
		}
		if sameGoalBlock(c.goalBlock, reason) {
			c.goalBlocks++
		} else {
			c.goalBlocks = 1
			c.goalBlock = reason
		}
		if c.goalBlocks >= 3 {
			c.goalStatus = GoalStatusBlocked
			notice = "goal blocked: " + reason
		}
	default:
		c.goalBlocks = 0
		c.goalBlock = ""
		c.goalIntercepts = 0
		c.goalSelfCheckDone = false
		c.goalIdleTurns = 0
	}
	// Idle detection: if the agent went multiple turns without any tool
	// calls, inject a reminder to make progress (unless the goal is already
	// completing or hitting the auto-turn limit).
	if notice == "" && c.goalInterceptMsg == "" {
		if c.toolWasCalledLastTurn() {
			c.goalIdleTurns = 0
		} else {
			c.goalIdleTurns++
			if c.goalIdleTurns >= maxGoalIdleTurns {
				c.goalIdleTurns = 0
				c.goalInterceptMsg = "No tool calls in recent turns. Either make progress with tools or signal [goal:blocked:<reason>]."
			}
		}
	}
	if notice == "" && c.goalTurns >= maxGoalAutoTurns {
		c.goalStatus = GoalStatusBlocked
		c.goalBlock = "goal continuation limit reached"
		c.goalIntercepts = 0
		c.goalSelfCheckDone = false
		c.goalInterceptMsg = ""
		c.goalIdleTurns = 0
		notice = c.goalBlock
	}
	if notice != "" {
		c.saveGoalState()
	}
	cont := notice == ""
	c.mu.Unlock()
	if notice != "" {
		c.notice(notice)
	}
	return cont
}

// incompleteGoalTodos checks the executor's canonical todo state and evidence
// readiness (project checks) for anything that should block [goal:complete].
// Returns a formatted reminder string, or empty if nothing is blocking.
func (c *Controller) incompleteGoalTodos() string {
	if c.executor == nil {
		return ""
	}
	var parts []string

	// 1. Check canonical todos.
	todos := c.executor.CanonicalTodoState()
	if len(todos) > 0 {
		incomplete := evidence.IncompleteTodos(todos)
		if len(incomplete) > 0 {
			var b strings.Builder
			b.WriteString("the following tasks are still incomplete:")
			for _, t := range incomplete {
				fmt.Fprintf(&b, "\n  - %s (%s)", t.Content, t.Status)
			}
			parts = append(parts, b.String())
		}
	}

	// 2. Check evidence readiness (project checks from AGENTS.md).
	if reason := c.executor.GoalReadinessFailure(); reason != "" {
		parts = append(parts, reason)
	}

	if len(parts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Goal signaled complete but issues remain:\n")
	for _, p := range parts {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("Fix or use todo_write/complete_step to mark done, then [goal:complete] again.")
	return b.String()
}

// toolWasCalledLastTurn reports whether the most recent assistant message
// contained any tool calls, indicating the agent made observable progress.
func (c *Controller) toolWasCalledLastTurn() bool {
	msgs := c.History()
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == provider.RoleAssistant {
			return len(m.ToolCalls) > 0
		}
		if m.Role == provider.RoleUser {
			return false
		}
	}
	return false
}

func parseGoalStatusMarker(text string) (status, reason string, ok bool) {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch lower {
		case "[goal:complete]":
			return GoalStatusComplete, "", true
		case "[goal:continue]":
			return GoalStatusRunning, "", true
		}
		const blockedPrefix = "[goal:blocked:"
		if strings.HasPrefix(lower, blockedPrefix) && strings.HasSuffix(line, "]") {
			return GoalStatusBlocked, strings.TrimSpace(line[len(blockedPrefix) : len(line)-1]), true
		}
		return "", "", false
	}
	return "", "", false
}

func sameGoalBlock(a, b string) bool {
	return normalizeGoalBlockReason(a) == normalizeGoalBlockReason(b)
}

func cleanGoalBlockReason(reason string) string {
	return strings.Trim(strings.TrimSpace(reason), " \t\r\n:：,，.。;；!！?？-—_[]()（）")
}

func normalizeGoalBlockReason(reason string) string {
	reason = strings.ToLower(cleanGoalBlockReason(reason))
	var b strings.Builder
	lastSpace := true
	for _, r := range reason {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (c *Controller) stopGoal(status string) {
	c.mu.Lock()
	if strings.TrimSpace(c.goal) != "" && c.goalStatus == GoalStatusRunning {
		c.goalStatus = status
	}
	c.goalInterceptMsg = ""
	c.goalIntercepts = 0
	c.goalSelfCheckDone = false
	c.goalIdleTurns = 0
	c.saveGoalState()
	c.mu.Unlock()
}

// saveGoalState persists the current goal state to disk for session continuity.
func (c *Controller) saveGoalState() {
	if c.goalStatePath == "" || c.executor == nil {
		return
	}
	todos := c.executor.CanonicalTodoState()
	state := goalState{
		Goal:         c.goal,
		Status:       c.goalStatus,
		ResearchMode: c.goalResearchMode,
		Turns:        c.goalTurns,
		Blocks:       c.goalBlocks,
		Block:        c.goalBlock,
		Strict:       c.goalStrict,
		Todos:        todos,
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(c.goalStatePath), 0o755)
	_ = os.WriteFile(c.goalStatePath, data, 0o644)
}

// goalState is the serializable form of a running goal.
type goalState struct {
	Goal         string              `json:"goal,omitempty"`
	Status       string              `json:"status,omitempty"`
	ResearchMode GoalResearchMode    `json:"researchMode,omitempty"`
	Turns        int                 `json:"turns,omitempty"`
	Blocks       int                 `json:"blocks,omitempty"`
	Block        string              `json:"block,omitempty"`
	Strict       bool                `json:"strict,omitempty"`
	Todos        []evidence.TodoItem `json:"todos,omitempty"`
}

// lastAssistantText returns the content of the most recent assistant message with
// non-empty text — the model's final answer for the turn (its plan, in plan mode).
func lastAssistantText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func (c *Controller) maybeAutoStartResearchGoal(input, display string) bool {
	goal, ok := c.autoStartResearchGoalCandidate(input)
	if !ok {
		return false
	}
	if c.runner != nil {
		displayText := display
		if strings.TrimSpace(displayText) == "" {
			displayText = goal
		}
		c.runGuarded(func(ctx context.Context) error {
			c.SetGoalWithResearchMode(goal, GoalResearchOn)
			c.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ShortGoalForNotice(goal)))
			block, errs := c.ResolveRefs(ctx, goal)
			for _, e := range errs {
				c.notice(e)
			}
			sent := "Start pursuing the active goal now."
			if block != "" {
				sent = "Referenced context:\n\n" + block + "\n\n" + sent
			}
			return c.runGoalLoopWithRawDisplay(ctx, sent, goal, displayText)
		})
	}
	return true
}

// AutoStartResearchGoal upgrades a strong long-horizon ordinary prompt into a
// Goal + AutoResearch run for frontends that already accepted an idle turn.
func (c *Controller) AutoStartResearchGoal(input string) (string, bool) {
	goal, ok := c.autoStartResearchGoalCandidate(input)
	if !ok {
		return "", false
	}
	c.SetGoalWithResearchMode(goal, GoalResearchOn)
	c.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ShortGoalForNotice(goal)))
	return goal, true
}

func (c *Controller) autoStartResearchGoalCandidate(input string) (string, bool) {
	goal := strings.TrimSpace(input)
	if !shouldAutoStartResearchGoal(goal) {
		return "", false
	}
	c.mu.Lock()
	plan := c.planMode
	running := c.running
	activeGoal := strings.TrimSpace(c.goal) != "" && c.goalStatus == GoalStatusRunning
	c.mu.Unlock()
	if plan || running || activeGoal {
		return "", false
	}
	return goal, true
}

func (c *Controller) rememberProjectNote(note string) {
	if note == "" {
		c.notice("nothing to remember")
		return
	}
	if path, err := c.QuickAdd(memory.ScopeProject, note); err != nil {
		c.notice("memory: " + err.Error())
	} else {
		c.notice("remembered → " + path)
	}
}

func (c *Controller) applyGoalCommand(input, display string) bool {
	cmd, ok := ParseGoalCommand(input)
	if !ok {
		return false
	}
	switch cmd.Action {
	case GoalCommandSet:
		c.SetPlanMode(false)
		c.SetGoalWithResearchMode(cmd.Text, cmd.ResearchMode)
		c.GoalStrict(cmd.Strict)
		c.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ShortGoalForNotice(cmd.Text)))
		if c.runner != nil {
			c.runGuarded(func(ctx context.Context) error {
				return c.runGoalLoopWithRawDisplay(ctx, "Start pursuing the active goal now.", cmd.Text, display)
			})
		}
	case GoalCommandClear:
		c.ClearGoal()
		c.notice(i18n.M.GoalCleared)
	default:
		goal := c.Goal()
		if strings.TrimSpace(goal) == "" {
			c.notice(i18n.M.GoalEmpty)
		} else {
			c.notice(fmt.Sprintf(i18n.M.GoalCurrentFmt, goal))
		}
	}
	return true
}

func ShortGoalForNotice(goal string) string {
	goal = strings.Join(strings.Fields(goal), " ")
	runes := []rune(goal)
	const max = 160
	if len(runes) <= max {
		return goal
	}
	return string(runes[:max]) + "..."
}

// GoalStrict enables or disables strict goal mode. In strict mode the agent
// cannot override an incomplete-todo intercept — it must actually finish or
// update all items before [goal:complete] is accepted.
func (c *Controller) GoalStrict(strict bool) {
	c.mu.Lock()
	c.goalStrict = strict
	c.saveGoalState()
	c.mu.Unlock()
}

// SetGoal stores a session-scoped active goal. Compose injects it into outgoing
// user turns, not the system prompt or tool schema, so it does not disturb the
// cache-stable prefix.
func (c *Controller) SetGoal(goal string) {
	c.SetGoalWithResearchMode(goal, GoalResearchAuto)
}

func (c *Controller) SetGoalWithResearchMode(goal string, researchMode GoalResearchMode) {
	goal = strings.TrimSpace(goal)
	c.mu.Lock()
	defer c.mu.Unlock()
	if goal == "" {
		c.goal = ""
		c.goalStatus = GoalStatusStopped
		c.goalResearchMode = GoalResearchAuto
		c.goalTurns = 0
		c.goalBlocks = 0
		c.goalBlock = ""
		c.goalInterceptMsg = ""
		c.goalIntercepts = 0
		c.goalSelfCheckDone = false
		c.goalIdleTurns = 0
		c.goalStrict = false
		c.saveGoalState()
		return
	}
	if c.goal == goal && c.goalStatus == GoalStatusRunning && c.goalResearchMode == researchMode {
		return
	}
	c.goal = goal
	c.goalStatus = GoalStatusRunning
	c.goalResearchMode = researchMode
	c.goalTurns = 0
	c.goalBlocks = 0
	c.goalBlock = ""
	c.goalInterceptMsg = ""
	c.goalIntercepts = 0
	c.goalSelfCheckDone = false
	c.goalIdleTurns = 0
	c.goalStrict = false
	c.saveGoalState()
}

func (c *Controller) ClearGoal() {
	c.SetGoal("")
}

func (c *Controller) Goal() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.goal
}

func (c *Controller) GoalStatus() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(c.goal) == "" && c.goalStatus == "" {
		return GoalStatusStopped
	}
	if c.goalStatus == "" {
		return GoalStatusStopped
	}
	return c.goalStatus
}
