package autoresearch

import "path/filepath"

func (s *Store) Readiness(taskID string) (*ReadinessReport, error) {
	report := &ReadinessReport{}
	validation, err := s.ValidateTask(taskID)
	if err != nil {
		return nil, err
	}
	if !validation.Valid {
		for _, validationErr := range validation.Errors {
			report.Errors = append(report.Errors, validationErr.File+":"+validationErr.Field+": "+validationErr.Error)
		}
		return report, nil
	}
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
	if progress.Status == StatusBlocked {
		report.BlockedReason = progress.BlockedReason
		if report.BlockedReason == "" {
			report.BlockedReason = "task is blocked"
		}
		return report, nil
	}
	findings, err := s.Findings(taskID, 0)
	if err != nil {
		return nil, err
	}
	accepted := acceptedFindingIDs(findings)
	for _, criterion := range task.Spec.SuccessCriteria {
		if !criterion.Required {
			continue
		}
		if countAcceptedEvidence(criterion.EvidenceIDs, accepted) == 0 {
			report.MissingCriteria = append(report.MissingCriteria, criterion.ID)
		}
	}
	report.Ready = len(report.MissingCriteria) == 0 && report.BlockedReason == "" && len(report.Errors) == 0
	return report, nil
}

func acceptedFindingIDs(findings []Finding) map[string]bool {
	accepted := make(map[string]bool, len(findings))
	for _, finding := range findings {
		if finding.Accepted {
			accepted[finding.ID] = true
		}
	}
	return accepted
}

func countAcceptedEvidence(ids []string, accepted map[string]bool) int {
	count := 0
	for _, id := range ids {
		if accepted[id] {
			count++
		}
	}
	return count
}
