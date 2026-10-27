package control

import (
	"context"
	"errors"
	"reasonix/internal/contract/config"
	"strings"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/langpref"
	"reasonix/internal/state/memory"
)

// turnBlock is one host-owned block on a user turn: the transient tag it is
// registered under, and the rendered body — empty when this turn owes none.
// An empty tag marks a marker that is not an XML block (the plan-mode line,
// which StripComposePrefixes recognises on its own).
type turnBlock struct {
	tag  string
	body string
}

// projectTurn renders what the provider sees for one turn: the owed blocks in
// order, then the user's text, then the tail. The order is the contract — a
// block nearer the text is read closer to the request, so the more specific
// its authority, the later it belongs.
func projectTurn(blocks []turnBlock, text, tail string) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.body == "" {
			continue
		}
		b.WriteString(blk.body)
		b.WriteString("\n\n")
	}
	b.WriteString(text)
	if tail == "" {
		return b.String()
	}
	return strings.TrimRight(b.String(), "\n") + "\n\n" + tail
}

// Tags a once-delivered block is registered under. The visibility question and
// the renderer share them, so the two cannot drift apart.
const (
	availableSkillsTag     = "available-skills"
	projectInstructionsTag = "project-instructions"
	projectRulesTag        = "project-rules"
	scopedRulesTag         = "scoped-rules"
)

// turnBlocksFor is the whole set a turn can carry, in projection order. Adding
// a kind of dynamic state means one entry here, not a new prepend site — which
// is what this replaced: eight of them, whose order was the order they happened
// to be written in, reversed.
func (c *Controller) turnBlocksFor(source string, includeOwed bool, notes []string, plan bool, goal, goalStatus, goalStopCause, responseLanguage, reasoningLanguage string) []turnBlock {
	var blocks []turnBlock
	var always, scoped string
	if includeOwed {
		if res, ok := c.loadRules(); ok {
			always, scoped = c.rules.owedAlways(res), c.rules.owedScoped(res, c.skills.pathHits)
		}
		// Drained for a real turn only: a synthetic one must not consume what
		// the user's next message is owed.
		blocks = append(blocks,
			turnBlock{hookContextTag, c.drainHookContextBlock()},
			turnBlock{currentDateTag, wrapTurnBlock(currentDateTag, c.date.owed())},
			turnBlock{sessionScratchTag, wrapTurnBlock(sessionScratchTag, c.scratch.owed())},
			turnBlock{availableSkillsTag, wrapTurnBlock(availableSkillsTag, c.skills.owedCatalog())},
			turnBlock{projectInstructionsTag, wrapTurnBlock(projectInstructionsTag, c.memory.owedInstructions(c.WorkspaceTrust() != config.WorkspaceTrusted))},
			turnBlock{projectRulesTag, wrapTurnBlock(projectRulesTag, always)},
			turnBlock{scopedRulesTag, wrapTurnBlock(scopedRulesTag, scoped)},
			turnBlock{"mcp-prompt-failure", wrapTurnBlock("mcp-prompt-failure", c.mcp.promptFailures.owed())},
			turnBlock{"refused-calls", c.refusedCallsBlock()},
		)
	}
	return append(blocks,
		turnBlock{"scheduled-run", c.scheduledRunBlock()},
		turnBlock{"background-jobs", c.backgroundJobsBlock()},
		turnBlock{"memory-update", memoryUpdateBlock(notes)},
		turnBlock{"reasoning-language", langpref.ReasoningLanguageBlock(langpref.ResolveReasoningLanguage(reasoningLanguage, source))},
		turnBlock{"turn-narration", langpref.TurnNarrationBlock(c.TurnNarration() == config.TurnNarrationDetailed, langpref.ResolveResponseLanguage(responseLanguage, source))},
		turnBlock{"response-language", langpref.ResponseLanguageBlock(langpref.ResolveResponseLanguage(responseLanguage, source))},
		turnBlock{"", planModeMarkerBlock(plan)},
		turnBlock{activeGoalTag, c.activeGoalTurnBlock(goal, goalStatus, goalStopCause)},
	)
}

func wrapTurnBlock(tag, body string) string {
	if body == "" {
		return ""
	}
	return "<" + tag + ">\n" + body + "\n</" + tag + ">"
}

// memoryUpdateBlock reports changes made outside the conversation — a "#"
// quick-add, the memory panel — which nothing else tells a running session
// about. The model's own remember/forget queues nothing: its tool result is
// already here.
func memoryUpdateBlock(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The following project-memory changes were just made and apply from now on:\n")
	for _, n := range notes {
		b.WriteString("- " + n + "\n")
	}
	return wrapTurnBlock("memory-update", strings.TrimRight(b.String(), "\n"))
}

// backgroundJobsBlock carries completions the user-facing notices report but
// the model's context never sees.
func (c *Controller) backgroundJobsBlock() string {
	if c.jobs == nil {
		return ""
	}
	return wrapTurnBlock("background-jobs", c.jobs.DrainCompletedNoteForSession(c.parentSessionID()))
}

func planModeMarkerBlock(plan bool) string {
	if !plan {
		return ""
	}
	return PlanModeMarker
}

func (c *Controller) activeGoalTurnBlock(goal, goalStatus, goalStopCause string) string {
	if strings.TrimSpace(goal) != "" && goalStatus == GoalStatusRunning {
		return c.activeGoalBlockForTurn(goal)
	}
	return c.goalRetractionBlock(goal, goalStatus, goalStopCause)
}

// recallTail is the one block after the user's text: retrieved facts answer the
// request. A carried note already supplied the new fact verbatim, so every
// outcome — note, ablation switch, real search — records and audits one decision.
func (c *Controller) recallTail(source string, hasNotes bool) string {
	query := strings.TrimSpace(source)
	if hasNotes {
		suppressed := memory.RecallResult{Query: query}
		suppressed.Suppress(memory.SuppressCarriedNote, "memory update already supplies the new fact")
		c.memory.recordRecall(suppressed)
		event.RecordMemoryRecall(c.sink, memoryRecallAudit(suppressed))
		return ""
	}
	if c.ablation.Off(ablation.Retrieval) {
		ablated := memory.RecallResult{Query: query}
		ablated.Suppress(memory.SuppressRetrievalOff, "automatic recall is switched off for this turn")
		c.memory.recordRecall(ablated)
		event.RecordMemoryRecall(c.sink, memoryRecallAudit(ablated))
		return ""
	}
	result := c.memory.recall(source)
	event.RecordMemoryRecall(c.sink, memoryRecallAudit(result))
	return result.Block()
}

// ErrNoRunner reports a turn started on a controller assembled without a
// runner. Returned instead of calling through the nil interface: that fault is
// a hardware exception on Windows, recovered or not.
var ErrNoRunner = errors.New("controller has no runner")

// runWithRunner hands the composed turn to the runner. Whether the model holds a
// projected block is asked of the model-visible history, so a refusal, a cancel
// or a failed send needs no compensating bookkeeping. The exception is the
// opening restatement of a rebuilt runtime (see projection_debt.go): this
// hand-off ends that one-turn state.
func (c *Controller) runWithRunner(ctx context.Context, input string) error {
	if c.runner == nil {
		return ErrNoRunner
	}
	c.settleProjection()
	return c.runner.Run(ctx, input)
}
