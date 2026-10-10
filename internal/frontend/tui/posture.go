package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// cycleMode steps read only, ask, auto, YOLO, plan and back to read only, the
// 1.x order with the Ask posture Studio adds between read only and auto.
func (m *model) cycleMode() tea.Cmd {
	s := &m.status
	switch {
	case s.Plan:
		return m.setPosture(false, "readOnly")
	case s.ToolApprovalMode == "readOnly":
		return m.setPosture(false, "ask")
	case s.ToolApprovalMode == "auto":
		m.yoloRestore = "auto"
		return m.setPosture(false, "yolo")
	case s.ToolApprovalMode == "yolo":
		m.yoloRestore = ""
		return m.setPosture(true, "ask")
	default:
		return m.setPosture(false, "auto")
	}
}

// toggleYolo enters YOLO, remembering the posture it left, and a second press
// goes back to that posture.
func (m *model) toggleYolo() tea.Cmd {
	if m.status.ToolApprovalMode == "yolo" {
		restore := m.yoloRestore
		if restore == "" || restore == "yolo" {
			restore = "ask"
		}
		m.yoloRestore = ""
		return m.setPosture(m.status.Plan, restore)
	}
	m.yoloRestore = m.status.ToolApprovalMode
	return m.setPosture(m.status.Plan, "yolo")
}

// setPosture moves the kernel to plan and mode together, plan first so a turn
// submitted right after never sees the new mode under the old workflow.
func (m *model) setPosture(plan bool, mode string) tea.Cmd {
	s := &m.status
	planChanged := s.Plan != plan
	s.Plan, s.ToolApprovalMode = plan, mode
	step := func(ctx context.Context) error {
		if planChanged {
			if err := m.client.SetPlan(ctx, plan); err != nil {
				return err
			}
		}
		return m.client.SetApprovalMode(ctx, mode)
	}
	return tea.Sequence(m.call("mode", step), m.fetchStatus())
}
