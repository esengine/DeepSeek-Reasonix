package autoresearch

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

var safeTaskID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var explicitTaskPath = regexp.MustCompile(`\.reasonix/autoresearch/([A-Za-z0-9][A-Za-z0-9._-]*)/?`)

type Store struct {
	workspaceRoot string
	root          string
	mu            sync.Mutex
	taskLocks     map[string]*sync.Mutex
}

func NewStore(workspaceRoot string) *Store {
	if resolved, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		workspaceRoot = resolved
	}
	return &Store{
		workspaceRoot: workspaceRoot,
		root:          filepath.Join(workspaceRoot, ".reasonix", "autoresearch"),
		taskLocks:     map[string]*sync.Mutex{},
	}
}

func (s *Store) lockTask(taskID string) func() {
	s.mu.Lock()
	lock := s.taskLocks[taskID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.taskLocks[taskID] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (s *Store) CreateTask(goal string, opts CreateOptions) (*Task, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil, errors.New("autoresearch: goal is required")
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	id, err := s.nextTaskID(now, goal)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, fmt.Errorf("autoresearch: create root dir: %w", err)
	}
	storeRoot, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, fmt.Errorf("autoresearch: open root dir: %w", err)
	}
	defer storeRoot.Close()
	taskRel, err := s.taskRel(id)
	if err != nil {
		return nil, err
	}
	if err := storeRoot.MkdirAll(filepath.Join(taskRel, "state"), 0o755); err != nil {
		return nil, fmt.Errorf("autoresearch: create state dir: %w", err)
	}
	if err := storeRoot.MkdirAll(filepath.Join(taskRel, "logs"), 0o755); err != nil {
		return nil, fmt.Errorf("autoresearch: create logs dir: %w", err)
	}

	spec := TaskSpec{
		TaskID:            id,
		Goal:              goal,
		Scope:             append([]string(nil), opts.Scope...),
		NonGoals:          append([]string(nil), opts.NonGoals...),
		AllowedOperations: opts.AllowedOperations,
		SuccessCriteria:   cloneCriteria(opts.SuccessCriteria),
	}
	progress := Progress{
		Status:    StatusRunning,
		UpdatedAt: now,
	}

	if err := writeJSONFile(storeRoot, filepath.Join(taskRel, "state", "task_spec.json"), spec); err != nil {
		return nil, err
	}
	if err := writeJSONFile(storeRoot, filepath.Join(taskRel, "state", "progress.json"), progress); err != nil {
		return nil, err
	}
	for _, path := range []string{
		filepath.Join(taskRel, "state", "directions_tried.json"),
		filepath.Join(taskRel, "state", "findings.jsonl"),
		filepath.Join(taskRel, "state", "iteration_log.jsonl"),
		filepath.Join(taskRel, "logs", "heartbeat.jsonl"),
	} {
		if err := storeRoot.WriteFile(path, nil, 0o644); err != nil {
			return nil, fmt.Errorf("autoresearch: initialize %s: %w", path, err)
		}
	}
	return &Task{ID: id, Root: s.taskRoot(id), Spec: spec}, nil
}

func (s *Store) ListSummaries() ([]Summary, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return []Summary{}, nil
		}
		return nil, fmt.Errorf("autoresearch: list tasks: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if validateTaskID(id) != nil {
			continue
		}
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	out := make([]Summary, 0, len(ids))
	for _, id := range ids {
		summary, err := s.Summary(id)
		if err != nil {
			return nil, err
		}
		out = append(out, *summary)
	}
	return out, nil
}

func (s *Store) LoadTask(taskID string) (*Task, error) {
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return nil, err
	}
	defer storeRoot.Close()
	info, err := storeRoot.Lstat(taskRel)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("autoresearch: task %s not found", taskID)
		}
		return nil, fmt.Errorf("autoresearch: stat task %s: %w", taskID, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("autoresearch: task %s is a symlink", taskID)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("autoresearch: task %s is not a directory", taskID)
	}
	var spec TaskSpec
	if err := readJSONFile(storeRoot, filepath.Join(taskRel, "state", "task_spec.json"), &spec); err != nil {
		return nil, err
	}
	return &Task{ID: taskID, Root: s.taskRoot(taskID), Spec: spec}, nil
}

func (s *Store) ResumeFromGoalText(goal string) (*Task, bool, error) {
	match := explicitTaskPath.FindStringSubmatch(goal)
	if len(match) < 2 {
		return nil, false, nil
	}
	task, err := s.LoadTask(match[1])
	if err != nil {
		return nil, true, err
	}
	if report, err := s.ValidateTask(task.ID); err != nil {
		return nil, true, err
	} else if !report.Valid {
		return nil, true, fmt.Errorf("autoresearch: task %s is invalid: %v", task.ID, report.Errors)
	}
	return task, true, nil
}

func (s *Store) AppendFinding(taskID string, f Finding) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return err
	}
	defer storeRoot.Close()
	if err := validateFinding(f); err != nil {
		return err
	}
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("autoresearch: marshal finding: %w", err)
	}
	return appendJSONL(storeRoot, filepath.Join(taskRel, "state", "findings.jsonl"), data)
}

func (s *Store) RecordEvidence(taskID, criterionID string, f Finding) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}
	criterionID = strings.TrimSpace(criterionID)
	if criterionID == "" {
		return errors.New("autoresearch: criterion id is required")
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return err
	}
	defer storeRoot.Close()
	if err := validateFinding(f); err != nil {
		return err
	}
	specPath := filepath.Join(taskRel, "state", "task_spec.json")
	var spec TaskSpec
	if err := readJSONFile(storeRoot, specPath, &spec); err != nil {
		return err
	}
	found := false
	for i := range spec.SuccessCriteria {
		if spec.SuccessCriteria[i].ID != criterionID {
			continue
		}
		found = true
		if !stringSliceContains(spec.SuccessCriteria[i].EvidenceIDs, f.ID) {
			spec.SuccessCriteria[i].EvidenceIDs = append(spec.SuccessCriteria[i].EvidenceIDs, f.ID)
		}
		break
	}
	if !found {
		return fmt.Errorf("autoresearch: criterion %q not found", criterionID)
	}
	if err := writeJSONFile(storeRoot, specPath, spec); err != nil {
		return err
	}
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("autoresearch: marshal finding: %w", err)
	}
	return appendJSONL(storeRoot, filepath.Join(taskRel, "state", "findings.jsonl"), data)
}

func (s *Store) Findings(taskID string, limit int) ([]Finding, error) {
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return nil, err
	}
	defer storeRoot.Close()
	var findings []Finding
	if err := readJSONL(storeRoot, filepath.Join(taskRel, "state", "findings.jsonl"), func(data []byte) error {
		var f Finding
		if err := json.Unmarshal(data, &f); err != nil {
			return err
		}
		findings = append(findings, f)
		return nil
	}); err != nil {
		return nil, err
	}
	for i, j := 0, len(findings)-1; i < j; i, j = i+1, j-1 {
		findings[i], findings[j] = findings[j], findings[i]
	}
	if limit > 0 && len(findings) > limit {
		findings = findings[:limit]
	}
	return findings, nil
}

func (s *Store) AppendHeartbeat(taskID string, h Heartbeat) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return err
	}
	defer storeRoot.Close()
	if err := validateHeartbeat(h); err != nil {
		return err
	}
	data, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("autoresearch: marshal heartbeat: %w", err)
	}
	return appendJSONL(storeRoot, filepath.Join(taskRel, "logs", "heartbeat.jsonl"), data)
}

func (s *Store) Heartbeats(taskID string, limit int) ([]Heartbeat, error) {
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return nil, err
	}
	defer storeRoot.Close()
	var heartbeats []Heartbeat
	if err := readJSONL(storeRoot, filepath.Join(taskRel, "logs", "heartbeat.jsonl"), func(data []byte) error {
		var h Heartbeat
		if err := json.Unmarshal(data, &h); err != nil {
			return err
		}
		heartbeats = append(heartbeats, h)
		return nil
	}); err != nil {
		return nil, err
	}
	if limit > 0 && len(heartbeats) > limit {
		heartbeats = heartbeats[len(heartbeats)-limit:]
	}
	return heartbeats, nil
}

func (s *Store) LastHeartbeat(taskID string) (Heartbeat, bool, error) {
	heartbeats, err := s.Heartbeats(taskID, 1)
	if err != nil {
		return Heartbeat{}, false, err
	}
	if len(heartbeats) == 0 {
		return Heartbeat{}, false, nil
	}
	return heartbeats[0], true, nil
}

func (s *Store) RecordDirection(taskID string, d Direction) (*Progress, error) {
	if err := validateTaskID(taskID); err != nil {
		return nil, err
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return nil, err
	}
	defer storeRoot.Close()
	d.Summary = strings.TrimSpace(d.Summary)
	if d.Summary == "" {
		return nil, errors.New("autoresearch: direction summary is required")
	}
	now := d.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var progress Progress
	if err := readJSONFile(storeRoot, filepath.Join(taskRel, "state", "progress.json"), &progress); err != nil {
		return nil, err
	}
	progress.Iteration++
	progress.CurrentDirection = d.Summary
	progress.UpdatedAt = now

	directions, err := s.loadDirections(storeRoot, taskRel)
	if err != nil {
		return nil, err
	}
	fp := slugify(d.Summary)
	repeated := false
	for i := range directions {
		if directions[i].Fingerprint == fp {
			repeated = true
			directions[i].Count++
			directions[i].LastSeenIteration = progress.Iteration
			break
		}
	}
	if !repeated {
		directions = append(directions, DirectionTried{
			Fingerprint:        fp,
			Summary:            d.Summary,
			FirstSeenIteration: progress.Iteration,
			LastSeenIteration:  progress.Iteration,
			Count:              1,
		})
	}
	if repeated || len(d.AcceptedEvidenceIDs) == 0 {
		before := progress.StaleCount
		progress.StaleCount++
		if before < 2 && progress.StaleCount >= 2 {
			progress.PivotCount++
		}
	} else {
		progress.StaleCount = 0
	}
	if err := writeJSONFile(storeRoot, filepath.Join(taskRel, "state", "directions_tried.json"), directions); err != nil {
		return nil, err
	}
	if err := writeJSONFile(storeRoot, filepath.Join(taskRel, "state", "progress.json"), progress); err != nil {
		return nil, err
	}
	return &progress, nil
}

func (s *Store) UpdateProgress(taskID string, patch ProgressPatch) (*Progress, error) {
	if err := validateTaskID(taskID); err != nil {
		return nil, err
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return nil, err
	}
	defer storeRoot.Close()
	var progress Progress
	if err := readJSONFile(storeRoot, filepath.Join(taskRel, "state", "progress.json"), &progress); err != nil {
		return nil, err
	}
	if patch.Status != nil {
		progress.Status = strings.TrimSpace(*patch.Status)
	}
	if patch.CurrentDirection != nil {
		progress.CurrentDirection = strings.TrimSpace(*patch.CurrentDirection)
	}
	if patch.BlockedReason != nil {
		progress.BlockedReason = strings.TrimSpace(*patch.BlockedReason)
	}
	progress.UpdatedAt = time.Now().UTC()
	report := &ValidationReport{Valid: true}
	validateProgress(report, progress)
	if len(report.Errors) > 0 {
		return nil, fmt.Errorf("autoresearch: invalid progress patch: %v", report.Errors)
	}
	if err := writeJSONFile(storeRoot, filepath.Join(taskRel, "state", "progress.json"), progress); err != nil {
		return nil, err
	}
	return &progress, nil
}

func (s *Store) ValidateTask(taskID string) (*ValidationReport, error) {
	storeRoot, taskRel, err := s.openTaskRoot(taskID)
	if err != nil {
		return nil, err
	}
	defer storeRoot.Close()
	report := &ValidationReport{Valid: true}
	info, err := storeRoot.Lstat(taskRel)
	if err != nil {
		report.add("task", "", err.Error())
		report.Valid = false
		return report, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		report.add("task", "", "task directory must not be a symlink")
		report.Valid = false
		return report, nil
	}
	if !info.IsDir() {
		report.add("task", "", "task path is not a directory")
		report.Valid = false
		return report, nil
	}
	var spec TaskSpec
	if err := readJSONFile(storeRoot, filepath.Join(taskRel, "state", "task_spec.json"), &spec); err != nil {
		report.add("task_spec.json", "", err.Error())
	} else {
		validateTaskSpec(report, taskID, spec)
	}
	var progress Progress
	if err := readJSONFile(storeRoot, filepath.Join(taskRel, "state", "progress.json"), &progress); err != nil {
		report.add("progress.json", "", err.Error())
	} else {
		validateProgress(report, progress)
	}
	for _, rel := range []string{
		"state/directions_tried.json",
		"state/findings.jsonl",
		"state/iteration_log.jsonl",
		"logs/heartbeat.jsonl",
	} {
		if _, err := storeRoot.Stat(filepath.Join(taskRel, rel)); err != nil {
			report.add(filepath.Base(rel), "", err.Error())
		}
	}
	report.Valid = len(report.Errors) == 0
	return report, nil
}

func (s *Store) taskRoot(taskID string) string {
	return filepath.Join(s.root, taskID)
}

func (s *Store) taskRel(taskID string, parts ...string) (string, error) {
	if err := validateTaskID(taskID); err != nil {
		return "", err
	}
	all := append([]string{taskID}, parts...)
	rel := filepath.Join(all...)
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("autoresearch: unsafe task-relative path %q", rel)
	}
	return rel, nil
}

func (s *Store) openTaskRoot(taskID string) (*os.Root, string, error) {
	taskRel, err := s.taskRel(taskID)
	if err != nil {
		return nil, "", err
	}
	storeRoot, err := os.OpenRoot(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("autoresearch: task %s not found", taskID)
		}
		return nil, "", fmt.Errorf("autoresearch: open root dir: %w", err)
	}
	return storeRoot, taskRel, nil
}

func (s *Store) nextTaskID(now time.Time, goal string) (string, error) {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return "", fmt.Errorf("autoresearch: create root dir: %w", err)
	}
	storeRoot, err := os.OpenRoot(s.root)
	if err != nil {
		return "", fmt.Errorf("autoresearch: open root dir: %w", err)
	}
	defer storeRoot.Close()
	base := now.Format("20060102-150405") + "-" + slugify(goal)
	if base == now.Format("20060102-150405")+"-" {
		base += "task"
	}
	id := base
	for i := 2; ; i++ {
		taskRel, err := s.taskRel(id)
		if err != nil {
			return "", err
		}
		if _, err := storeRoot.Lstat(taskRel); err != nil {
			if os.IsNotExist(err) {
				return id, nil
			}
			return "", fmt.Errorf("autoresearch: stat task id %s: %w", id, err)
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

func validateTaskID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("autoresearch: task id is required")
	}
	if !safeTaskID.MatchString(id) || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("autoresearch: unsafe task id %q", id)
	}
	return nil
}

func writeJSONFile(root *os.Root, path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("autoresearch: marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := root.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("autoresearch: write %s: %w", path, err)
	}
	return nil
}

func readJSONFile(root *os.Root, path string, out any) error {
	data, err := root.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func appendJSONL(root *os.Root, path string, data []byte) error {
	if err := root.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("autoresearch: create jsonl dir: %w", err)
	}
	f, err := root.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("autoresearch: open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("autoresearch: append %s: %w", path, err)
	}
	return nil
}

func readJSONL(root *os.Root, path string, each func([]byte) error) error {
	f, err := root.Open(path)
	if err != nil {
		return fmt.Errorf("autoresearch: open %s: %w", path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := each([]byte(line)); err != nil {
			return fmt.Errorf("autoresearch: parse %s: %w", path, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("autoresearch: scan %s: %w", path, err)
	}
	return nil
}

func (s *Store) loadDirections(root *os.Root, taskRel string) ([]DirectionTried, error) {
	path := filepath.Join(taskRel, "state", "directions_tried.json")
	data, err := root.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}
	var directions []DirectionTried
	if err := json.Unmarshal(data, &directions); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return directions, nil
}

func validateFinding(f Finding) error {
	if strings.TrimSpace(f.ID) == "" {
		return errors.New("autoresearch: finding id is required")
	}
	switch f.Kind {
	case FindingKindCommand, FindingKindFile, FindingKindTest, FindingKindBenchmark, FindingKindManual, FindingKindReview:
	default:
		return fmt.Errorf("autoresearch: finding kind %q is invalid", f.Kind)
	}
	if strings.TrimSpace(f.Summary) == "" {
		return errors.New("autoresearch: finding summary is required")
	}
	if f.CreatedAt.IsZero() {
		return errors.New("autoresearch: finding created_at is required")
	}
	return nil
}

func validateHeartbeat(h Heartbeat) error {
	switch h.Status {
	case HeartbeatStartingTurn, HeartbeatTurnDone, HeartbeatWarning:
	default:
		return fmt.Errorf("autoresearch: heartbeat status %q is invalid", h.Status)
	}
	if h.Iteration < 0 {
		return errors.New("autoresearch: heartbeat iteration must not be negative")
	}
	if h.CreatedAt.IsZero() {
		return errors.New("autoresearch: heartbeat created_at is required")
	}
	return nil
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cloneCriteria(in []SuccessCriterion) []SuccessCriterion {
	out := make([]SuccessCriterion, len(in))
	for i, c := range in {
		out[i] = c
		out[i].EvidenceIDs = append([]string(nil), c.EvidenceIDs...)
	}
	return out
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	const maxSlugLen = 56
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-")
	}
	if slug == "" {
		return "task"
	}
	return slug
}
