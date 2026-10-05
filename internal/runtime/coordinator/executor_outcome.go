package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

// The planner's session never holds executor turns, so what the executor did
// reaches it as a projection on its next planning input. Only the newest
// outcomes are kept, each bounded, and every cut says what it dropped.
const (
	maxOwedExecutorOutcomes = 4
	maxExecutorOutcomeRunes = 2000
	maxExecutorTaskRunes    = 400
)

// executorOutcome is one executor run the planner has not been shown. task is
// set only when the planner never saw that turn's input; reply is empty when
// no reply of this run's own could be identified.
type executorOutcome struct {
	task    string
	reply   string
	stopped bool
}

// owedOutcomes holds the newest outcomes and counts the ones trimmed away.
type owedOutcomes struct {
	outcomes []executorOutcome
	dropped  int
}

func (o *owedOutcomes) add(out executorOutcome) {
	out.task = boundedRunes(out.task, maxExecutorTaskRunes)
	out.reply = boundedRunes(out.reply, maxExecutorOutcomeRunes)
	o.outcomes = append(o.outcomes, out)
	if excess := len(o.outcomes) - maxOwedExecutorOutcomes; excess > 0 {
		o.outcomes = append([]executorOutcome(nil), o.outcomes[excess:]...)
		o.dropped += excess
	}
}

// runExecutor runs the executor and owes the planner what it answered.
func (c *Coordinator) runExecutor(ctx context.Context, input string, plannerSawTask bool) error {
	sess := c.executor.Session()
	var before, rewriteBefore int
	stampFloor := time.Now().UnixMilli()
	if sess != nil {
		prior := sess.Snapshot()
		before = len(prior)
		rewriteBefore = sess.RewriteVersion()
		for _, m := range prior {
			stampFloor = max(stampFloor, m.CreatedAt+1)
		}
	}
	err := c.executor.Run(ctx, input)
	out := executorOutcome{stopped: err != nil}
	if !plannerSawTask {
		out.task = agent.RawUserInput(ctx, input)
	}
	if sess != nil {
		out.reply = runReply(sess.Snapshot(), before, sess.RewriteVersion() != rewriteBefore, stampFloor)
	}
	c.owed.add(out)
	return err
}

// runReply finds the last reply this run produced. Without a rewrite the run's
// messages start at before; after one, only a user message stamped later than
// the run's start and every earlier message marks where they begin.
func runReply(msgs []provider.Message, before int, rewritten bool, stampFloor int64) string {
	floor := before
	if rewritten {
		floor = -1
		for i, m := range msgs {
			if m.Role == provider.RoleUser && m.CreatedAt >= stampFloor {
				floor = i
				break
			}
		}
		if floor < 0 {
			return ""
		}
	}
	for i := len(msgs) - 1; i >= floor; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return agent.DisplayAssistantText(msgs[i].Content)
		}
	}
	return ""
}

// withExecutorOutcomes appends the owed outcomes to a planning input.
func (c *Coordinator) withExecutorOutcomes(plannerInput string) string {
	if len(c.owed.outcomes) == 0 {
		return plannerInput
	}
	var b strings.Builder
	b.WriteString(plannerInput)
	b.WriteString("\n\n<executor-since-last-plan>\n")
	if c.owed.dropped > 0 {
		fmt.Fprintf(&b, "(%d earlier executor turn(s) omitted)\n", c.owed.dropped)
	}
	for i, o := range c.owed.outcomes {
		fmt.Fprintf(&b, "## Executor turn %d", i+1)
		if o.stopped {
			b.WriteString(" (stopped before finishing)")
		}
		b.WriteString("\n")
		if o.task != "" {
			fmt.Fprintf(&b, "User asked the executor directly: %s\n", o.task)
		}
		reply := o.reply
		if reply == "" {
			reply = "(no reply captured)"
		}
		fmt.Fprintf(&b, "Executor reply: %s\n", reply)
	}
	b.WriteString("</executor-since-last-plan>")
	return b.String()
}

func boundedRunes(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= limit {
		return string(r)
	}
	return fmt.Sprintf("%s … [cut by the host: %d more characters]", string(r[:limit]), len(r)-limit)
}
