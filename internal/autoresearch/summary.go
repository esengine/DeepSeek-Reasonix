package autoresearch

import "path/filepath"

func (s *Store) Summary(taskID string) (*Summary, error) {
	task, err := s.LoadTask(taskID)
	if err != nil {
		return nil, err
	}
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return nil, err
	}
	defer storeRoot.Close()
	var progress Progress
	if err := readJSONFile(storeRoot, filepath.Join(taskRel, "state", "progress.json"), &progress); err != nil {
		return nil, err
	}
	findings, err := s.Findings(taskID, 0)
	if err != nil {
		return nil, err
	}
	lastHeartbeat, _, err := s.LastHeartbeat(taskID)
	if err != nil {
		return nil, err
	}
	accepted := acceptedFindingIDs(findings)
	openCriteria := make([]CriterionSummary, 0)
	for _, criterion := range task.Spec.SuccessCriteria {
		count := countAcceptedEvidence(criterion.EvidenceIDs, accepted)
		status := "satisfied"
		if criterion.Required && count == 0 {
			status = "open"
		}
		if status == "open" {
			openCriteria = append(openCriteria, CriterionSummary{
				ID:            criterion.ID,
				Description:   criterion.Description,
				Required:      criterion.Required,
				EvidenceCount: count,
				Status:        status,
			})
		}
	}
	summary := &Summary{
		TaskID:             task.ID,
		Goal:               task.Spec.Goal,
		Status:             progress.Status,
		Iteration:          progress.Iteration,
		CurrentDirection:   progress.CurrentDirection,
		StaleCount:         progress.StaleCount,
		PivotCount:         progress.PivotCount,
		PivotRequired:      progress.StaleCount >= 2,
		LastHeartbeatAt:    lastHeartbeat.CreatedAt,
		FindingCount:       len(findings),
		OpenCriteria:       openCriteria,
		Blocker:            progress.BlockedReason,
		TaskPath:           task.Root,
		NextRequiredAction: nextRequiredAction(progress),
	}
	return summary, nil
}

func nextRequiredAction(progress Progress) string {
	if progress.Status == StatusBlocked {
		return "resolve blocker before continuing"
	}
	if progress.StaleCount >= 4 {
		return "ask for the smallest external input needed"
	}
	if progress.StaleCount >= 2 {
		return "make a structural pivot before continuing"
	}
	return "continue with the next evidence-producing step"
}
