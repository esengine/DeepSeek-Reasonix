package control

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"reasonix/internal/evidence"
	"reasonix/internal/store"
)

const (
	maxGoalAutoTurns   = 50
	maxGoalIdleTurns   = 2
	goalContinueTurn   = "Continue pursuing the active goal under its task contract. If it is complete, provide the concise final result and end with [goal:complete]. If progress genuinely requires user-only information, an irreversible or externally visible operation, or a changed scope, end with [goal:blocked:<short reason>]. Otherwise use sensible defaults, do the next useful work, and end with [goal:continue]."
	goalSelfCheckTurn  = "The agent signaled goal completion and all tasks are marked done. Before finalizing, perform a brief quality self-check:\n1. Verify any changed files compile or parse correctly\n2. Run the relevant tests if applicable\n3. Confirm the original request, output format, constraints, and success criteria are met\nIf everything checks out, signal [goal:complete]. If issues are found, fix them and signal [goal:complete] when done."
	goalCompleteNotice = "goal complete"
)

// goalMachine owns the active goal's finite-state machine and its persistence.
// It is a strict leaf: its methods take only the machine's own locks and never
// call back into the Controller, so the controller may hold c.mu while invoking
// a getter without risking lock inversion. The FSM is pure — advance() takes
// already-gathered inputs (the parsed marker, the executor's todo snapshot and
// readiness, whether a tool ran) and returns what to persist plus a notice, so
// no disk or executor work happens under mu.
type goalMachine struct {
	// mu guards the FSM fields below; every critical section under it is short
	// and non-blocking (no disk I/O, no executor calls).
	mu                 sync.Mutex
	goal               string
	status             string
	researchMode       GoalResearchMode
	autoResearchTaskID string
	turns              int
	blocks             int
	block              string
	interceptMsg       string
	intercepts         int
	strict             bool
	selfCheckDone      bool
	idleTurns          int

	// statePath is the persisted goal-state sidecar; empty disables persistence.
	statePath string
	// writeMu serializes goal-state disk writes so concurrent saves don't
	// interleave or land out of order. Taken OFF mu by writeState.
	writeMu sync.Mutex
}

// goalState is the serializable form of a running goal.
type goalState struct {
	Goal               string              `json:"goal,omitempty"`
	Status             string              `json:"status,omitempty"`
	ResearchMode       GoalResearchMode    `json:"researchMode,omitempty"`
	AutoResearchTaskID string              `json:"autoResearchTaskID,omitempty"`
	Turns              int                 `json:"turns,omitempty"`
	Blocks             int                 `json:"blocks,omitempty"`
	Block              string              `json:"block,omitempty"`
	Strict             bool                `json:"strict,omitempty"`
	Todos              []evidence.TodoItem `json:"todos,omitempty"`
}

// goalAdvanceInput carries everything the FSM needs for one continuation step,
// gathered by the caller off the machine's lock.
type goalAdvanceInput struct {
	status     string // parsed marker status ("" when the turn carried no marker)
	reason     string // blocked reason from the marker, if any
	toolCalled bool   // whether the last turn made any tool call
	todos      []evidence.TodoItem
	readiness  string // executor.GoalReadinessFailure()
}

// goalAdvanceResult reports the FSM step's outcome. data/path/ok describe the
// state to persist (built under mu when something changed); notice is surfaced
// to the user; cont reports whether the goal loop should continue.
type goalAdvanceResult struct {
	notice string
	cont   bool
	path   string
	data   []byte
	ok     bool
}

// goalStatePath derives a session's persisted goal-state sidecar.
func goalStatePath(sessionPath string) string {
	return store.SessionGoalState(sessionPath)
}

func (g *goalMachine) setStatePath(path string) {
	g.mu.Lock()
	g.statePath = path
	g.mu.Unlock()
}

// snapshot returns the fields Compose injects into outgoing turns.
func (g *goalMachine) snapshot() (goal, status string, mode GoalResearchMode, autoResearchTaskID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.goal, g.status, g.researchMode, g.autoResearchTaskID
}

func (g *goalMachine) goalText() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.goal
}

func (g *goalMachine) currentAutoResearchTaskID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(g.goal) == "" || g.status != GoalStatusRunning {
		return ""
	}
	return g.autoResearchTaskID
}

// active reports whether a goal is currently running.
func (g *goalMachine) active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return strings.TrimSpace(g.goal) != "" && g.status == GoalStatusRunning
}

// statusForDisplay maps the empty zero status to "stopped" for frontends.
func (g *goalMachine) statusForDisplay() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.status == "" {
		return GoalStatusStopped
	}
	return g.status
}

// set installs a session-scoped goal (or clears it when goal is empty), resets
// the per-goal counters, and returns the state to persist. ok is false (no
// persistence) when the goal is unchanged or no state path is configured.
func (g *goalMachine) set(goal string, mode GoalResearchMode, autoResearchTaskID string, todos []evidence.TodoItem) (string, []byte, bool) {
	goal = strings.TrimSpace(goal)
	g.mu.Lock()
	defer g.mu.Unlock()
	if goal != "" && g.goal == goal && g.status == GoalStatusRunning && g.researchMode == mode && g.autoResearchTaskID == autoResearchTaskID {
		return "", nil, false
	}
	g.turns, g.blocks, g.block = 0, 0, ""
	g.interceptMsg, g.intercepts = "", 0
	g.selfCheckDone, g.idleTurns, g.strict = false, 0, false
	if goal == "" {
		g.goal, g.status, g.researchMode, g.autoResearchTaskID = "", GoalStatusStopped, GoalResearchAuto, ""
	} else {
		g.goal, g.status, g.researchMode, g.autoResearchTaskID = goal, GoalStatusRunning, mode, autoResearchTaskID
	}
	return g.buildStateLocked(todos)
}

func (g *goalMachine) setStrict(strict bool, todos []evidence.TodoItem) (string, []byte, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.strict = strict
	return g.buildStateLocked(todos)
}

// stop transitions a running goal to the given terminal status and clears the
// transient intercept/idle bookkeeping.
func (g *goalMachine) stop(status string, todos []evidence.TodoItem) (string, []byte, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(g.goal) != "" && g.status == GoalStatusRunning {
		g.status = status
	}
	g.interceptMsg = ""
	g.intercepts = 0
	g.selfCheckDone = false
	g.idleTurns = 0
	return g.buildStateLocked(todos)
}

// takeIntercept consumes a pending continuation-turn override, if any.
func (g *goalMachine) takeIntercept() (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.interceptMsg == "" {
		return "", false
	}
	msg := g.interceptMsg
	g.interceptMsg = ""
	return msg, true
}

// advance runs one continuation step of the goal FSM from already-gathered
// inputs. It mutates the machine, decides whether to keep looping, and builds
// the state to persist when the goal reached a terminal/notice point.
func (g *goalMachine) advance(in goalAdvanceInput) goalAdvanceResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(g.goal) == "" || g.status != GoalStatusRunning {
		return goalAdvanceResult{cont: false}
	}
	g.turns++
	var notice string
	switch in.status {
	case GoalStatusComplete:
		if incomplete := formatIncompleteTodos(in.todos, in.readiness); len(incomplete) > 0 && (g.strict || g.intercepts == 0) {
			// In strict mode every claim is blocked until todos are done;
			// otherwise only the first consecutive claim is intercepted.
			g.intercepts++
			g.interceptMsg = incomplete
			break
		}
		// Todos are all done — in strict mode run self-check before final
		// completion. Non-strict mode completes immediately.
		if g.strict && !g.selfCheckDone {
			g.selfCheckDone = true
			g.interceptMsg = goalSelfCheckTurn
			break
		}
		// Self-check passed — complete the goal.
		g.intercepts = 0
		g.selfCheckDone = false
		g.idleTurns = 0
		g.goal = ""
		g.status = GoalStatusComplete
		g.blocks = 0
		g.block = ""
		g.interceptMsg = ""
		notice = goalCompleteNotice
	case GoalStatusBlocked:
		reason := cleanGoalBlockReason(in.reason)
		if reason == "" {
			reason = "blocked"
		}
		if sameGoalBlock(g.block, reason) {
			g.blocks++
		} else {
			g.blocks = 1
			g.block = reason
		}
		if g.blocks >= 3 {
			g.status = GoalStatusBlocked
			notice = "goal blocked: " + reason
		}
	default:
		g.blocks = 0
		g.block = ""
		g.intercepts = 0
		g.selfCheckDone = false
		g.idleTurns = 0
	}
	// Idle detection: if the agent went multiple turns without any tool calls,
	// inject a reminder to make progress (unless the goal is already completing
	// or hitting the auto-turn limit).
	if notice == "" && g.interceptMsg == "" {
		if in.toolCalled {
			g.idleTurns = 0
		} else {
			g.idleTurns++
			if g.idleTurns >= maxGoalIdleTurns {
				g.idleTurns = 0
				g.interceptMsg = "No tool calls in recent turns. Either make progress with tools or signal [goal:blocked:<reason>]."
			}
		}
	}
	if notice == "" && g.turns >= maxGoalAutoTurns {
		g.status = GoalStatusBlocked
		g.block = "goal continuation limit reached"
		g.intercepts = 0
		g.selfCheckDone = false
		g.interceptMsg = ""
		g.idleTurns = 0
		notice = g.block
	}
	res := goalAdvanceResult{notice: notice, cont: notice == ""}
	if notice != "" {
		res.path, res.data, res.ok = g.buildStateLocked(in.todos)
	}
	return res
}

// buildStateLocked marshals the current goal state for persistence. The caller
// holds mu; this only reads in-memory state, never touching disk. Returns ok=false
// when persistence is disabled (no state path). The matching writeState does the
// disk write OFF mu so the per-turn save can't stall a status poll.
func (g *goalMachine) buildStateLocked(todos []evidence.TodoItem) (path string, data []byte, ok bool) {
	if g.statePath == "" {
		return "", nil, false
	}
	state := goalState{
		Goal:               g.goal,
		Status:             g.status,
		ResearchMode:       g.researchMode,
		AutoResearchTaskID: g.autoResearchTaskID,
		Turns:              g.turns,
		Blocks:             g.blocks,
		Block:              g.block,
		Strict:             g.strict,
		Todos:              todos,
	}
	b, err := json.Marshal(state)
	if err != nil {
		slog.Warn("controller: marshal goal state", "err", err)
		return "", nil, false
	}
	return g.statePath, b, true
}

// writeState persists pre-marshaled goal-state bytes to disk, OFF mu and
// serialized by writeMu so concurrent saves don't interleave or land out of
// order. Best-effort: failures are logged, not surfaced.
func (g *goalMachine) writeState(path string, data []byte) {
	if path == "" || data == nil {
		return
	}
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Warn("controller: goal state dir", "err", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Warn("controller: write goal state", "err", err)
	}
}

// persistWithTodos re-persists goal state with the given todos, without
// changing any in-memory goal fields. Used after force-completing todos on
// goal completion so a session reload does not revert to the old incomplete
// todo state.
func (g *goalMachine) persistWithTodos(todos []evidence.TodoItem) {
	g.mu.Lock()
	path, data, ok := g.buildStateLocked(todos)
	g.mu.Unlock()
	if ok {
		g.writeState(path, data)
	}
}

// terminalTodosFromState reads the persisted goal-state sidecar and returns its
// todo snapshot only after the goal has reached a terminal state. Running goal
// state is not refreshed on every todo_write, so its todos may be older than the
// transcript rebuilt by Agent.SetSession.
func (g *goalMachine) terminalTodosFromState(sessionPath string) ([]evidence.TodoItem, bool) {
	if strings.TrimSpace(sessionPath) == "" {
		return nil, false
	}
	data, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("controller: read goal state", "err", err)
		}
		return nil, false
	}
	var state goalState
	if err := json.Unmarshal(data, &state); err != nil {
		slog.Warn("controller: parse goal state", "err", err)
		return nil, false
	}
	switch state.Status {
	case GoalStatusComplete, GoalStatusBlocked, GoalStatusStopped:
	default:
		return nil, false
	}
	if len(state.Todos) == 0 {
		return nil, false
	}
	return append([]evidence.TodoItem(nil), state.Todos...), true
}

// restoreRunningFromState reloads the active running goal from the persisted
// sidecar during cold resume. Terminal sidecar data is intentionally ignored:
// terminal todo repair is handled by terminalTodosFromState without reviving the
// goal loop.
func (g *goalMachine) restoreRunningFromState(sessionPath string) {
	if strings.TrimSpace(sessionPath) == "" {
		return
	}
	data, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("controller: read goal state", "err", err)
		}
		return
	}
	var state goalState
	if err := json.Unmarshal(data, &state); err != nil {
		slog.Warn("controller: parse goal state", "err", err)
		return
	}
	if state.Status != GoalStatusRunning || strings.TrimSpace(state.Goal) == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.goal = strings.TrimSpace(state.Goal)
	g.status = GoalStatusRunning
	g.researchMode = state.ResearchMode
	g.autoResearchTaskID = strings.TrimSpace(state.AutoResearchTaskID)
	g.turns = state.Turns
	g.blocks = state.Blocks
	g.block = state.Block
	g.strict = state.Strict
	g.interceptMsg, g.intercepts = "", 0
	g.selfCheckDone, g.idleTurns = false, 0
}

// formatIncompleteTodos renders the reminder shown when [goal:complete] arrives
// while the executor's canonical todos or project-readiness checks aren't done.
// Returns empty when nothing is blocking. Pure: the caller gathers todos and the
// readiness reason from the executor off the goal lock.
func formatIncompleteTodos(todos []evidence.TodoItem, readiness string) string {
	var parts []string
	if len(todos) > 0 {
		if incomplete := evidence.IncompleteTodos(todos); len(incomplete) > 0 {
			var b strings.Builder
			b.WriteString("the following tasks are still incomplete:")
			for _, t := range incomplete {
				fmt.Fprintf(&b, "\n  - %s (%s)", t.Content, t.Status)
			}
			parts = append(parts, b.String())
		}
	}
	if readiness != "" {
		parts = append(parts, readiness)
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

// ShortGoalForNotice collapses whitespace and truncates a goal for one-line UI.
func ShortGoalForNotice(goal string) string {
	goal = strings.Join(strings.Fields(goal), " ")
	runes := []rune(goal)
	const max = 160
	if len(runes) <= max {
		return goal
	}
	return string(runes[:max]) + "..."
}

// goalTodos snapshots the executor's canonical todos for goal-state persistence.
func (c *Controller) goalTodos() []evidence.TodoItem {
	if c.executor == nil {
		return nil
	}
	return c.executor.CanonicalTodoState()
}

// persistGoalState writes a freshly built goal state to disk, off c.mu. The
// executor guard preserves the original behavior of skipping persistence when
// no executor is attached.
func (c *Controller) persistGoalState(path string, data []byte, ok bool) {
	if !ok || c.executor == nil {
		return
	}
	c.goals.writeState(path, data)
}

func (c *Controller) restoreTerminalGoalTodos(sessionPath string) {
	if c.executor == nil {
		return
	}
	todos, ok := c.goals.terminalTodosFromState(sessionPath)
	if !ok {
		return
	}
	c.executor.ReplaceTodoState(todos)
}
