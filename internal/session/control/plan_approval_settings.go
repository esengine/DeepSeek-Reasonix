package control

import (
	"context"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

// PlanApprovalSettings is whether a submitted plan starts executing without
// the user's click, as the user file records it. It is the user's alone: a
// project file cannot set it, so there is no second effective value.
type PlanApprovalSettings struct {
	Auto bool   `json:"auto"`
	Path string `json:"path"`
}

// PlanApprovalSettings reads [agent] plan_approval from the user file.
func (c *Controller) PlanApprovalSettings() PlanApprovalSettings {
	path := config.UserConfigPath()
	return PlanApprovalSettings{Auto: config.LoadForEdit(path).PlanApprovalAuto(), Path: path}
}

// SavePlanApprovalSettings persists the choice to the user file; the next plan
// submitted in any session reads it.
func (c *Controller) SavePlanApprovalSettings(auto bool) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	cfg.SetPlanApproval(auto)
	return cfg.SaveTo(path)
}

// planApproved answers the plan card for these steps: the user's standing choice
// when they made one, the card otherwise. An automatic start leaves a notice in
// the transcript.
func (c *Controller) planApproved(ctx context.Context, steps string) (bool, error) {
	if c.PlanApprovalSettings().Auto {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodePlanAutoApproved, Text: i18n.M.PlanAutoApproved})
		return true, nil
	}
	allow, _, err := c.requestApproval(ctx, planApprovalRequest(steps, ""))
	return allow, err
}
