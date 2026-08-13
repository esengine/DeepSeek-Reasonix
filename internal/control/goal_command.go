package control

import (
	"context"
	"fmt"

	"reasonix/internal/i18n"
)

func (c *Controller) startGoalCommandTurn(cmd GoalCommand, display string) {
	if !c.goals.active() {
		return
	}
	c.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ShortGoalForNotice(c.Goal())))
	if c.runner != nil {
		c.runGuarded(func(ctx context.Context) error {
			return c.runGoalLoopWithRawDisplay(ctx, "Start pursuing the active goal now.", cmd.Text, display)
		})
	}
}
