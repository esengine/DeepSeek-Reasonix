package autoresearch

import "strings"

func (r *ValidationReport) add(file, field, msg string) {
	r.Errors = append(r.Errors, ValidationError{File: file, Field: field, Error: msg})
}

func validateTaskSpec(report *ValidationReport, taskID string, spec TaskSpec) {
	if strings.TrimSpace(spec.TaskID) == "" {
		report.add("task_spec.json", "task_id", "task id is required")
	} else if spec.TaskID != taskID {
		report.add("task_spec.json", "task_id", "task id must match directory")
	}
	if strings.TrimSpace(spec.Goal) == "" {
		report.add("task_spec.json", "goal", "goal is required")
	}
	seenCriteria := map[string]bool{}
	for i, c := range spec.SuccessCriteria {
		fieldPrefix := "success_criteria"
		if strings.TrimSpace(c.ID) == "" {
			report.add("task_spec.json", fieldPrefix, "criterion id is required")
		} else if seenCriteria[c.ID] {
			report.add("task_spec.json", fieldPrefix, "criterion id must be unique")
		}
		seenCriteria[c.ID] = true
		if strings.TrimSpace(c.Description) == "" {
			report.add("task_spec.json", fieldPrefix, "criterion description is required")
		}
		_ = i
	}
}

func validateProgress(report *ValidationReport, progress Progress) {
	switch progress.Status {
	case StatusRunning, StatusBlocked, StatusComplete, StatusStopped, StatusInvalid:
	default:
		report.add("progress.json", "status", "status is invalid")
	}
	if progress.Iteration < 0 {
		report.add("progress.json", "iteration", "iteration must not be negative")
	}
	if progress.StaleCount < 0 {
		report.add("progress.json", "stale_count", "stale count must not be negative")
	}
	if progress.PivotCount < 0 {
		report.add("progress.json", "pivot_count", "pivot count must not be negative")
	}
	if progress.UpdatedAt.IsZero() {
		report.add("progress.json", "updated_at", "updated_at is required")
	}
}
