package autoresearch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const maxBoundedSummaryFindings = 10001

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
	return buildSummary(task, progress, findings, lastHeartbeat), nil
}

// SummaryBounded is the Remote/catalog summary path. It reads a coherent task
// snapshot under the task lock, enforces a total source-byte budget, and
// rejects more than 10k finding records instead of returning a partial count.
func (s *Store) SummaryBounded(taskID string, maxBytes int64) (*Summary, error) {
	summary, _, err := s.summaryBounded(taskID, maxBytes)
	return summary, err
}

func (s *Store) summaryBounded(taskID string, maxBytes int64) (*Summary, int64, error) {
	if maxBytes <= 0 {
		return nil, 0, errors.New("autoresearch: bounded summary requires a positive byte limit")
	}
	if err := validateTaskID(taskID); err != nil {
		return nil, 0, err
	}
	unlock := s.lockTask(taskID)
	defer unlock()

	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return nil, 0, err
	}
	defer storeRoot.Close()
	info, err := storeRoot.Lstat(taskRel)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("autoresearch: task %s not found", taskID)
		}
		return nil, 0, fmt.Errorf("autoresearch: stat task %s: %w", taskID, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, 0, fmt.Errorf("autoresearch: task %s is not a regular directory", taskID)
	}

	paths := []string{
		filepath.Join(taskRel, "state", "task_spec.json"),
		filepath.Join(taskRel, "state", "progress.json"),
		filepath.Join(taskRel, "state", "findings.jsonl"),
		filepath.Join(taskRel, "logs", "heartbeat.jsonl"),
	}
	var sourceBytes int64
	for _, path := range paths {
		fileInfo, statErr := storeRoot.Stat(path)
		if statErr != nil {
			return nil, 0, fmt.Errorf("autoresearch: stat %s: %w", path, statErr)
		}
		if !fileInfo.Mode().IsRegular() || fileInfo.Size() < 0 || fileInfo.Size() > maxBytes-sourceBytes {
			return nil, sourceBytes, fmt.Errorf("autoresearch: task %s exceeds bounded source-byte limit", taskID)
		}
		sourceBytes += fileInfo.Size()
	}

	var spec TaskSpec
	if err := readJSONFile(storeRoot, paths[0], &spec); err != nil {
		return nil, sourceBytes, err
	}
	var progress Progress
	if err := readJSONFile(storeRoot, paths[1], &progress); err != nil {
		return nil, sourceBytes, err
	}
	findings, err := s.FindingsBounded(taskID, maxBoundedSummaryFindings, maxBytes)
	if err != nil {
		return nil, sourceBytes, err
	}
	if len(findings) >= maxBoundedSummaryFindings {
		return nil, sourceBytes, fmt.Errorf("autoresearch: task %s exceeds bounded finding-item limit", taskID)
	}
	lastHeartbeat, _, err := s.LastHeartbeat(taskID)
	if err != nil {
		return nil, sourceBytes, err
	}
	task := &Task{ID: taskID, Root: s.taskRoot(taskID), Spec: spec}
	return buildSummary(task, progress, findings, lastHeartbeat), sourceBytes, nil
}

func buildSummary(task *Task, progress Progress, findings []Finding, lastHeartbeat Heartbeat) *Summary {
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
	return &Summary{
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
