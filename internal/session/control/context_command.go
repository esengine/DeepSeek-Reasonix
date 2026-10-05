package control

import (
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
)

// ContextReport renders the current context state as a one-line summary plus
// detail lines. The window is the denominator of every compaction decision, so
// a wrong one looks exactly like a full context until the number is visible.
func (c *Controller) ContextReport() (summary, detail string) {
	if c.executor == nil {
		return "context: unavailable", ""
	}
	return renderContextReport(c.executor.ContextReport())
}

// reportContext answers /context: the summary line, with the breakdown as its
// detail under a code a frontend can open it by.
func (c *Controller) reportContext() {
	summary, detail := c.ContextReport()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeContextReport, Text: summary, Detail: detail})
}

// ContextBreakdown says where the prompt's tokens went. The gauge answers how
// full the window is; this answers what is filling it, which is the half a user
// can act on.
func (c *Controller) ContextBreakdown() agent.ContextBreakdown {
	if c.executor == nil {
		return agent.ContextBreakdown{}
	}
	return c.executor.ContextBreakdown()
}

// runSessionVerb runs a session mutation off the dispatch path and reports the
// outcome, so the slash switch holds routing rather than goroutine plumbing.
func (c *Controller) runSessionVerb(run func() error, done, failPrefix string) {
	go func() {
		if err := run(); err != nil {
			c.notice(failPrefix + err.Error())
			return
		}
		c.notice(done)
	}()
}

func renderContextReport(rep agent.ContextReport) (summary, detail string) {
	if rep.Window <= 0 {
		return "context: maintenance disabled (context_window = 0)",
			fmt.Sprintf("%-18s%s\n%-18s%s", "latest prompt",
				thousands(rep.LatestPrompt), "canonical", thousands(rep.CanonicalTokens))
	}

	var b strings.Builder
	line := func(label string, value string) {
		fmt.Fprintf(&b, "%-18s%s\n", label, value)
	}
	hard := ""
	if rep.HardCeiling > 0 && rep.HardCeiling < rep.Window {
		hard = fmt.Sprintf("  (usable %s after output reserve)", thousands(rep.HardCeiling))
	}
	line("window", thousands(rep.Window)+hard)
	line("latest prompt", fmt.Sprintf("%s  (%s of window)", thousands(rep.LatestPrompt), percentOf(rep.LatestPrompt, rep.Window)))
	line("canonical", thousands(rep.CanonicalTokens))
	visible := thousands(rep.ProjectionTokens)
	if rep.Projected {
		visible += "  (projected)"
	}
	line("model-visible", visible)
	line("thresholds", fmt.Sprintf("fold %s · hard ceiling %s",
		thousands(rep.FoldThreshold), thousands(rep.HardCeiling)))
	if rep.LastMode != "" {
		last := rep.LastMode
		if rep.LastTrigger != "" {
			last += " · " + rep.LastTrigger
		}
		if rep.LastSource > 0 && rep.LastResult > 0 {
			last += fmt.Sprintf(" · %s -> %s", thousands(rep.LastSource), thousands(rep.LastResult))
		}
		line("last maintenance", last)
	} else {
		line("last maintenance", "none this session")
	}
	if rep.UserTurnKeepBudget > 0 {
		yours := fmt.Sprintf("budget %s per fold", thousands(rep.UserTurnKeepBudget))
		if rep.UserTurnsKept > 0 || rep.UserTurnsDropped > 0 {
			yours = fmt.Sprintf("%d kept · %d dropped (~%s tokens) · %s",
				rep.UserTurnsKept, rep.UserTurnsDropped, thousands(rep.UserTurnsDroppedToks), yours)
		}
		line("your turns", yours)
	}
	if rep.CacheState != "" {
		line("cache", rep.CacheState)
	}
	if rep.BlockedReason != "" {
		line("blocked", rep.BlockedReason)
	}
	return contextSummaryLine(rep), strings.TrimRight(b.String(), "\n")
}

// contextSummaryLine names the next thing that will happen, since "70% full" on
// its own does not say whether that is close to anything.
func contextSummaryLine(rep agent.ContextReport) string {
	next := "fold at " + percentOf(rep.FoldThreshold, rep.Window)
	switch {
	case rep.HardCeiling > 0 && rep.LatestPrompt >= rep.HardCeiling:
		next = "at the hard ceiling"
	case rep.FoldThreshold > 0 && rep.LatestPrompt >= rep.FoldThreshold:
		next = "folding"
	}
	s := fmt.Sprintf("context %s / %s (%s) · next: %s",
		thousands(rep.LatestPrompt), thousands(rep.Window),
		percentOf(rep.LatestPrompt, rep.Window), next)
	if rep.BlockedReason != "" {
		s += " · maintenance blocked"
	}
	return s
}

func percentOf(n, total int) string {
	if total <= 0 {
		return "n/a"
	}
	return strconv.Itoa((n*100+total/2)/total) + "%"
}

// thousands groups digits so a six-digit token count is readable at a glance.
func thousands(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}
