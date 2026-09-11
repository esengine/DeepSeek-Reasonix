package agent

import (
	"strings"

	"reasonix/internal/provider"
)

// rollbackPlannerTurn restores the snapshot unless compaction rewrote it.
// After a rewrite it keeps the compacted history but removes a failed tail:
// plain user nudges and empty or unpaired tool-call assistant turns.
func (c *Coordinator) rollbackPlannerTurn(before []provider.Message, rewriteBefore int) {
	if c.plannerSess.RewriteVersion() == rewriteBefore {
		c.plannerSess.Replace(before)
		return
	}
	msgs := c.plannerSess.Snapshot()
	for len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == provider.RoleAssistant &&
			(len(last.ToolCalls) > 0 || strings.TrimSpace(last.Content) == "") {
			msgs = msgs[:len(msgs)-1]
			continue
		}
		if last.Role != provider.RoleUser || isCompactionSummary(last) {
			break
		}
		msgs = msgs[:len(msgs)-1]
	}
	c.plannerSess.Replace(msgs)
}
