package control

import (
	"strings"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/memory"
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

// turnBlocksFor is the whole set a turn can carry, in projection order. Adding
// a kind of dynamic state means one entry here, not a new prepend site — which
// is what this replaced: eight of them, whose order was the order they happened
// to be written in, reversed.
func (c *Controller) turnBlocksFor(source string, includeOwed bool, notes []string, plan bool, goal, goalStatus, responseLanguage, reasoningLanguage string) []turnBlock {
	var blocks []turnBlock
	if includeOwed {
		// Drained for a real turn only: a synthetic one must not consume what
		// the user's next message is owed.
		blocks = append(blocks,
			turnBlock{hookContextTag, c.drainHookContextBlock()},
			turnBlock{"available-skills", wrapTurnBlock("available-skills", c.skills.owedCatalog())},
			turnBlock{"project-instructions", wrapTurnBlock("project-instructions", c.memory.drainInstructions())},
		)
	}
	return append(blocks,
		turnBlock{"background-jobs", c.backgroundJobsBlock()},
		turnBlock{"memory-update", memoryUpdateBlock(notes)},
		turnBlock{"reasoning-language", agent.ReasoningLanguageBlock(agent.ResolveReasoningLanguage(reasoningLanguage, source))},
		turnBlock{"response-language", agent.ResponseLanguageBlock(responseLanguage)},
		turnBlock{"", planModeMarkerBlock(plan)},
		turnBlock{"active-goal", c.activeGoalTurnBlock(goal, goalStatus)},
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

func (c *Controller) activeGoalTurnBlock(goal, goalStatus string) string {
	if strings.TrimSpace(goal) == "" || goalStatus != GoalStatusRunning {
		return ""
	}
	return c.activeGoalBlockForTurn(goal)
}

// recallTail is the one block that follows the user's text rather than
// preceding it: retrieved facts answer the request, so they sit with it. An
// out-of-band write already supplied the new fact verbatim, so that turn
// records why it did not retrieve instead of retrieving again.
func (c *Controller) recallTail(source string, hasNotes bool) string {
	if hasNotes {
		c.memory.recordRecall(memory.RecallResult{
			Query:      strings.TrimSpace(source),
			Suppressed: "memory update already supplies the new fact",
		})
		return ""
	}
	if c.ablation.Off(ablation.Retrieval) {
		return ""
	}
	result := c.memory.recall(source)
	event.RecordMemoryRecall(c.sink, memoryRecallAudit(result))
	return result.Block()
}
