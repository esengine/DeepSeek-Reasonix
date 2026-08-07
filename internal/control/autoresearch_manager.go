package control

// AutoResearch task lifecycle: creating/resuming tasks for research-mode
// goals, recording per-turn evidence and direction, and reporting readiness.
// autoResearchManager is a strict leaf over the workspace autoresearch.Store —
// it never touches Controller state; the Controller wrappers below resolve the
// active task from the goal machine and own notices.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/autoresearch"
)

type autoResearchSetup struct {
	taskID      string
	createToken string
	blockReason string
	notice      string
	created     bool
}

// autoResearchManager wraps the optional workspace autoresearch.Store. The
// zero value (no store) disables the subsystem; every method is nil-safe.
type autoResearchManager struct {
	store *autoresearch.Store
}

func (m autoResearchManager) enabled() bool {
	return m.store != nil
}

// prepare resumes the task matching goal text or creates a fresh one. The
// caller has already decided AutoResearch applies and that no running goal
// owns the task.
func (m autoResearchManager) prepare(goal, createToken string) autoResearchSetup {
	if m.store == nil {
		return autoResearchSetup{}
	}
	if task, ok, err := m.store.ResumeFromGoalText(goal); err != nil {
		slog.Warn("controller: resume autoresearch task", "err", err)
		if ok {
			return autoResearchSetup{blockReason: err.Error()}
		}
	} else if ok {
		return autoResearchSetup{taskID: task.ID, notice: "autoresearch task resumed: " + task.ID}
	}
	task, err := m.store.CreateTask(goal, autoresearch.CreateOptions{
		CreateToken: createToken,
		AllowedOperations: autoresearch.AllowedOperations{
			Write:   true,
			Network: false,
			Publish: false,
		},
		SuccessCriteria: defaultAutoResearchSuccessCriteria(),
	})
	if err != nil {
		slog.Warn("controller: create autoresearch task", "err", err)
		return autoResearchSetup{}
	}
	return autoResearchSetup{
		taskID:      task.ID,
		createToken: task.CreateToken,
		notice:      "autoresearch task created: " + task.ID,
		created:     true,
	}
}

func (m autoResearchManager) removeTask(taskID, createToken string) error {
	if m.store == nil {
		return nil
	}
	return m.store.RemoveTask(taskID, createToken)
}

func (m autoResearchManager) heartbeat(taskID, status, message string) {
	if m.store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	iteration := 0
	if summary, err := m.store.Summary(taskID); err == nil {
		iteration = summary.Iteration
	}
	if err := m.store.AppendHeartbeat(taskID, autoresearch.Heartbeat{
		Status:    status,
		Iteration: iteration,
		Message:   message,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		slog.Warn("controller: append autoresearch heartbeat", "task_id", taskID, "status", status, "err", err)
	}
}

func (m autoResearchManager) acceptedEvidenceIDs(taskID string) map[string]bool {
	if m.store == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	findings, err := m.store.Findings(taskID, 0)
	if err != nil {
		slog.Warn("controller: read autoresearch findings", "task_id", taskID, "err", err)
		return nil
	}
	accepted := make(map[string]bool, len(findings))
	for _, finding := range findings {
		if finding.Accepted {
			accepted[finding.ID] = true
		}
	}
	return accepted
}

// recordTurnProgress records the turn's direction: which evidence IDs became
// accepted since acceptedBefore, summarized from the assistant's final text.
func (m autoResearchManager) recordTurnProgress(taskID string, acceptedBefore map[string]bool, assistantText string) {
	if m.store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	acceptedAfter := m.acceptedEvidenceIDs(taskID)
	newAccepted := make([]string, 0)
	for id := range acceptedAfter {
		if acceptedBefore == nil || !acceptedBefore[id] {
			newAccepted = append(newAccepted, id)
		}
	}
	sort.Strings(newAccepted)
	if _, err := m.store.RecordDirection(taskID, autoresearch.Direction{
		Summary:             autoResearchDirectionSummary(assistantText),
		AcceptedEvidenceIDs: newAccepted,
		Now:                 time.Now().UTC(),
	}); err != nil {
		slog.Warn("controller: record autoresearch direction", "task_id", taskID, "err", err)
	}
}

func (m autoResearchManager) recordEvidenceFromAssistant(taskID, text string) {
	if m.store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	for _, item := range parseAutoResearchEvidenceBlocks(text) {
		if err := m.recordEvidence(taskID, item.CriterionID, AutoResearchEvidenceInput{
			ID:       item.ID,
			Kind:     item.Kind,
			Summary:  item.Summary,
			Source:   item.Source,
			Command:  item.Command,
			Paths:    append([]string(nil), item.Paths...),
			Accepted: item.Accepted,
		}); err != nil {
			slog.Warn("controller: record autoresearch evidence block", "task_id", taskID, "criterion_id", item.CriterionID, "err", err)
		}
	}
}

func (m autoResearchManager) recordEvidence(taskID, criterionID string, input AutoResearchEvidenceInput) error {
	if m.store == nil || strings.TrimSpace(taskID) == "" {
		return errors.New("autoresearch: no active task")
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = m.nextFindingID(taskID)
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = autoresearch.FindingKindManual
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = autoresearch.FindingSourceManual
	}
	finding := autoresearch.Finding{
		ID:        id,
		Kind:      kind,
		Summary:   strings.TrimSpace(input.Summary),
		Source:    source,
		Command:   strings.TrimSpace(input.Command),
		Paths:     append([]string(nil), input.Paths...),
		Accepted:  input.Accepted,
		CreatedAt: time.Now().UTC(),
	}
	return m.store.RecordEvidence(taskID, criterionID, finding)
}

func (m autoResearchManager) nextFindingID(taskID string) string {
	findings, err := m.store.Findings(taskID, 0)
	if err != nil {
		return fmt.Sprintf("f%d", time.Now().UTC().UnixNano())
	}
	used := make(map[string]bool, len(findings))
	for _, finding := range findings {
		used[finding.ID] = true
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("f%d", len(findings)+i)
		if !used[id] {
			return id
		}
	}
}

func (m autoResearchManager) readinessFailure(taskID string) string {
	if m.store == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	report, err := m.store.Readiness(taskID)
	if err != nil {
		return "AutoResearch readiness check failed: " + err.Error()
	}
	if report.Ready {
		return ""
	}
	var parts []string
	if len(report.MissingCriteria) > 0 {
		parts = append(parts, "missing criteria: "+strings.Join(report.MissingCriteria, ", "))
	}
	if report.BlockedReason != "" {
		parts = append(parts, "blocked: "+report.BlockedReason)
	}
	if len(report.Errors) > 0 {
		parts = append(parts, "state errors: "+strings.Join(report.Errors, "; "))
	}
	if len(parts) == 0 {
		parts = append(parts, "task is not ready")
	}
	return "AutoResearch readiness check failed: " + strings.Join(parts, "; ")
}

func (m autoResearchManager) summary(taskID string) (*autoresearch.Summary, error) {
	if m.store == nil {
		return nil, errors.New("autoresearch: disabled")
	}
	return m.store.Summary(taskID)
}

func (m autoResearchManager) listSummaries() ([]autoresearch.Summary, error) {
	if m.store == nil {
		return nil, errors.New("autoresearch: disabled")
	}
	return m.store.ListSummaries()
}

func (m autoResearchManager) findings(taskID string, limit int) ([]autoresearch.Finding, error) {
	if m.store == nil {
		return nil, errors.New("autoresearch: disabled")
	}
	return m.store.Findings(taskID, limit)
}

func (m autoResearchManager) updateProgress(taskID string, patch autoresearch.ProgressPatch) error {
	if m.store == nil {
		return nil
	}
	_, err := m.store.UpdateProgress(taskID, patch)
	return err
}

func defaultAutoResearchSuccessCriteria() []autoresearch.SuccessCriterion {
	return []autoresearch.SuccessCriterion{
		{
			ID:          "objective_evidence",
			Description: "The goal outcome is supported by direct evidence, such as inspected code, reproduced behavior, source material, or concrete findings.",
			Required:    true,
		},
		{
			ID:          "verification",
			Description: "The result has relevant verification evidence, such as tests, commands, benchmarks, manual checks, or a documented reason why verification is not applicable.",
			Required:    true,
		},
	}
}

type autoResearchEvidenceBlock struct {
	CriterionID string   `json:"criterion_id"`
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Summary     string   `json:"summary"`
	Source      string   `json:"source"`
	Command     string   `json:"command"`
	Paths       []string `json:"paths"`
	Accepted    bool     `json:"accepted"`
}

const (
	autoResearchEvidenceOpen  = "<autoresearch-evidence>"
	autoResearchEvidenceClose = "</autoresearch-evidence>"
)

func parseAutoResearchEvidenceBlocks(text string) []autoResearchEvidenceBlock {
	var out []autoResearchEvidenceBlock
	rest := text
	for {
		start := strings.Index(rest, autoResearchEvidenceOpen)
		if start < 0 {
			return out
		}
		rest = rest[start+len(autoResearchEvidenceOpen):]
		end := strings.Index(rest, autoResearchEvidenceClose)
		if end < 0 {
			return out
		}
		raw := strings.TrimSpace(rest[:end])
		rest = rest[end+len(autoResearchEvidenceClose):]
		if raw == "" {
			continue
		}
		var many []autoResearchEvidenceBlock
		if err := json.Unmarshal([]byte(raw), &many); err == nil {
			out = append(out, many...)
			continue
		}
		var one autoResearchEvidenceBlock
		if err := json.Unmarshal([]byte(raw), &one); err == nil {
			out = append(out, one)
		}
	}
}

func autoResearchDirectionSummary(text string) string {
	text = agent.StripAutoResearchEvidenceBlocks(text)
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if line == "" || strings.HasPrefix(lower, "[goal:") {
			continue
		}
		if len(line) > 160 {
			line = line[:160]
		}
		return line
	}
	return "turn completed"
}

// Controller-side glue: resolve the active task via the goal machine, then
// delegate to the leaf manager.

func (c *Controller) prepareAutoResearchTask(goal string, researchMode GoalResearchMode, createToken string) autoResearchSetup {
	goal = strings.TrimSpace(goal)
	if goal == "" || !c.autoResearch.enabled() || !shouldUseAutoResearch(goal, researchMode) {
		return autoResearchSetup{}
	}
	currentGoal, currentStatus, _, currentTaskID := c.goals.snapshot()
	if strings.TrimSpace(currentGoal) == goal && currentStatus == GoalStatusRunning && strings.TrimSpace(currentTaskID) != "" {
		return autoResearchSetup{taskID: currentTaskID}
	}
	return c.autoResearch.prepare(goal, createToken)
}

func (c *Controller) autoResearchReadinessFailure() string {
	return c.autoResearch.readinessFailure(c.goals.currentAutoResearchTaskID())
}

func (c *Controller) AutoResearchSummary() (*autoresearch.Summary, bool) {
	taskID := c.goals.currentAutoResearchTaskID()
	if !c.autoResearch.enabled() || strings.TrimSpace(taskID) == "" {
		return nil, false
	}
	summary, err := c.autoResearch.summary(taskID)
	if err != nil {
		return &autoresearch.Summary{
			TaskID:  taskID,
			Status:  autoresearch.StatusInvalid,
			Blocker: err.Error(),
		}, true
	}
	return summary, true
}

func (c *Controller) AutoResearchList() ([]autoresearch.Summary, bool) {
	if !c.autoResearch.enabled() {
		return nil, false
	}
	summaries, err := c.autoResearch.listSummaries()
	if err != nil {
		slog.Warn("controller: list autoresearch tasks", "err", err)
		return nil, true
	}
	return summaries, true
}

func (c *Controller) AutoResearchFindings(limit int) ([]autoresearch.Finding, bool) {
	taskID := c.goals.currentAutoResearchTaskID()
	if !c.autoResearch.enabled() || strings.TrimSpace(taskID) == "" {
		return nil, false
	}
	findings, err := c.autoResearch.findings(taskID, limit)
	if err != nil {
		return nil, true
	}
	return findings, true
}

func (c *Controller) RecordAutoResearchEvidence(criterionID string, input AutoResearchEvidenceInput) error {
	taskID := c.goals.currentAutoResearchTaskID()
	if !c.autoResearch.enabled() || strings.TrimSpace(taskID) == "" {
		return errors.New("autoresearch: no active task")
	}
	return c.autoResearch.recordEvidence(taskID, criterionID, input)
}
