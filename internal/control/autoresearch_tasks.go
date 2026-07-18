package control

import (
	"errors"
	"strings"

	"reasonix/internal/autoresearch"
)

var ErrAutoResearchTaskAccessUnavailable = errors.New("autoresearch task access unavailable")

// One above RuntimeAPI's 10k visited-item ceiling lets the projection reject
// overflow without an unbounded store read.
const autoResearchRemoteReadLimit = 10001

const autoResearchRemoteReadBytes = 8 << 20

// AutoResearchTaskAccess is an optional, explicit-task view used by shared
// RuntimeService adapters. The historical Goals port remains unchanged for
// terminal and Desktop callers, while Remote never has to infer task identity
// from whichever Goal happens to be active at the instant of a paged query.
type AutoResearchTaskAccess interface {
	CurrentAutoResearchTask() (*autoresearch.Summary, bool, error)
	ListAutoResearchTasks() ([]autoresearch.Summary, bool, error)
	AutoResearchTaskSummary(taskID string) (*autoresearch.Summary, bool, error)
	AutoResearchTaskFindings(taskID string) ([]autoresearch.Finding, bool, error)
	RecordAutoResearchTaskEvidence(taskID, criterionID string, finding autoresearch.Finding) error
}

func (c *Controller) CurrentAutoResearchTask() (*autoresearch.Summary, bool, error) {
	if c == nil || c.autoResearch == nil {
		return nil, false, nil
	}
	taskID := strings.TrimSpace(c.goals.currentAutoResearchTaskID())
	if taskID == "" {
		return nil, true, nil
	}
	summary, err := c.autoResearch.SummaryBounded(taskID, autoResearchRemoteReadBytes)
	return summary, true, err
}

func (c *Controller) ListAutoResearchTasks() ([]autoresearch.Summary, bool, error) {
	if c == nil || c.autoResearch == nil {
		return nil, false, nil
	}
	items, err := c.autoResearch.ListSummariesLimit(autoResearchRemoteReadLimit)
	return items, true, err
}

func (c *Controller) AutoResearchTaskSummary(taskID string) (*autoresearch.Summary, bool, error) {
	if c == nil || c.autoResearch == nil {
		return nil, false, nil
	}
	summary, err := c.autoResearch.SummaryBounded(strings.TrimSpace(taskID), autoResearchRemoteReadBytes)
	return summary, true, err
}

func (c *Controller) AutoResearchTaskFindings(taskID string) ([]autoresearch.Finding, bool, error) {
	if c == nil || c.autoResearch == nil {
		return nil, false, nil
	}
	items, err := c.autoResearch.FindingsBounded(strings.TrimSpace(taskID), autoResearchRemoteReadLimit, autoResearchRemoteReadBytes)
	return items, true, err
}

func (c *Controller) RecordAutoResearchTaskEvidence(taskID, criterionID string, finding autoresearch.Finding) error {
	if c == nil || c.autoResearch == nil {
		return ErrAutoResearchTaskAccessUnavailable
	}
	return c.autoResearch.RecordEvidence(strings.TrimSpace(taskID), strings.TrimSpace(criterionID), finding)
}

var _ AutoResearchTaskAccess = (*Controller)(nil)
