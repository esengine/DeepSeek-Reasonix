package control

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/i18n"
)

func (c *Controller) startGoalCommandTurnWithAdmission(cmd GoalCommand, display string, admission turnAdmission) {
	if c.GoalStatus() != GoalStatusRunning {
		return
	}
	if !c.sessionEngineEnabled() {
		c.goals.markExplicitStart()
	}
	c.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ShortGoalForNotice(c.Goal())))
	if c.runner != nil {
		c.runGuardedWithAdmission(func(ctx context.Context) error {
			return c.runGoalLoopWithRawDisplay(ctx, "Start pursuing the active goal now.", cmd.Text, display)
		}, admission)
	}
}

func (c *Controller) applyGoalCommand(input, display string) bool {
	return c.applyGoalCommandWithAdmission(input, display, turnAdmission{})
}

func (c *Controller) applyGoalCommandWithAdmission(input, display string, admission turnAdmission) bool {
	cmd, ok := ParseGoalCommand(input)
	if !ok {
		return false
	}
	if cmd.DeprecatedBudgetFlag {
		c.notice(GoalBudgetFlagDeprecatedNotice)
	}
	switch cmd.Action {
	case GoalCommandSet:
		if c.sessionEngineEnabled() {
			if err := c.SetGoalDurable(cmd.Text); err != nil {
				c.notice("goal: " + err.Error())
				break
			}
		} else {
			c.SetGoalWithResearchMode(cmd.Text, cmd.ResearchMode)
		}
		c.SetPlanMode(false)
		c.GoalStrict(cmd.Strict)
		c.startGoalCommandTurnWithAdmission(cmd, display, admission)
	case GoalCommandClear:
		if c.sessionEngineEnabled() {
			if err := c.SetGoalDurable(""); err != nil {
				c.notice("goal: " + err.Error())
				break
			}
		} else {
			c.ClearGoal()
		}
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
