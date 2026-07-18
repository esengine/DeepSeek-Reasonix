package runtimefactory

import (
	"reasonix/internal/autoresearch"
	"reasonix/internal/control"
)

// leasedController forwards the optional explicit-task interface while keeping
// Controller ownership and the Session writer lease in the existing wrapper.
func (c *leasedController) CurrentAutoResearchTask() (*autoresearch.Summary, bool, error) {
	access, ok := c.SessionAPI.(control.AutoResearchTaskAccess)
	if !ok {
		return nil, false, nil
	}
	return access.CurrentAutoResearchTask()
}

func (c *leasedController) ListAutoResearchTasks() ([]autoresearch.Summary, bool, error) {
	access, ok := c.SessionAPI.(control.AutoResearchTaskAccess)
	if !ok {
		return nil, false, nil
	}
	return access.ListAutoResearchTasks()
}

func (c *leasedController) AutoResearchTaskSummary(taskID string) (*autoresearch.Summary, bool, error) {
	access, ok := c.SessionAPI.(control.AutoResearchTaskAccess)
	if !ok {
		return nil, false, nil
	}
	return access.AutoResearchTaskSummary(taskID)
}

func (c *leasedController) AutoResearchTaskFindings(taskID string) ([]autoresearch.Finding, bool, error) {
	access, ok := c.SessionAPI.(control.AutoResearchTaskAccess)
	if !ok {
		return nil, false, nil
	}
	return access.AutoResearchTaskFindings(taskID)
}

func (c *leasedController) RecordAutoResearchTaskEvidence(taskID, criterionID string, finding autoresearch.Finding) error {
	access, ok := c.SessionAPI.(control.AutoResearchTaskAccess)
	if !ok {
		return control.ErrAutoResearchTaskAccessUnavailable
	}
	return access.RecordAutoResearchTaskEvidence(taskID, criterionID, finding)
}

var _ control.AutoResearchTaskAccess = (*leasedController)(nil)
