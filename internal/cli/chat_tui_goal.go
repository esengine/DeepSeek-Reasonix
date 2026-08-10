package cli

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/i18n"
)

func (m *chatTUI) noticeDeprecatedGoalBudget(cmd control.GoalCommand) {
	if cmd.DeprecatedBudgetFlag {
		m.notice(control.GoalBudgetFlagDeprecatedNotice)
	}
}

func (m *chatTUI) setGoalCommand(cmd control.GoalCommand, input string) tea.Cmd {
	m.planMode = false
	m.ctrl.SetPlanMode(false)
	m.ctrl.SetGoalWithResearchMode(cmd.Text, cmd.ResearchMode)
	m.ctrl.GoalStrict(cmd.Strict)
	if m.ctrl.GoalStatus() != control.GoalStatusRunning {
		m.echoLocalCommand(input)
		return nil
	}
	m.notice(fmt.Sprintf(i18n.M.GoalSetFmt, control.ShortGoalForNotice(m.ctrl.Goal())))
	return m.startTurn("Start pursuing the active goal now.", input, input)
}
