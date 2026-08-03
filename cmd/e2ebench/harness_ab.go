package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const abSchemaVersion = 1

const abHarnessProtocol = "reasonix-e2ebench-ab-v1"

const (
	abEventAdmissionStarted = "admission_started"
	abEventAttemptFinished  = "attempt_finished"

	abOutcomePassed               = "passed"
	abOutcomeVerificationFailed   = "verification_failed"
	abOutcomeAgentError           = "agent_error"
	abOutcomeTimeout              = "timeout"
	abOutcomeSuiteBudgetExhausted = "suite_budget_exhausted"
	abOutcomeInfraFailed          = "infra_failed"
)

type harnessABOpts struct {
	suite, model, runDir, runID, environmentID string
	baselineBin, candidateBin                  string
	baselineProfile, candidateProfile          string
	repetitions, infraRetries, tokenBudget     int
}

type abManifest struct {
	SchemaVersion   int              `json:"schema_version"`
	HarnessProtocol string           `json:"harness_protocol"`
	RunID           string           `json:"run_id"`
	CreatedAt       string           `json:"created_at"`
	Suite           abSuiteManifest  `json:"suite"`
	Model           string           `json:"model"`
	EnvironmentID   string           `json:"environment_id"`
	Repetitions     int              `json:"repetitions"`
	InfraRetries    int              `json:"infra_retries"`
	TokenBudget     int              `json:"token_budget_per_arm"`
	Arms            []abArmManifest  `json:"arms"`
	Tasks           []abTaskManifest `json:"tasks"`
}

type abSuiteManifest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type abArmManifest struct {
	ID           string `json:"id"`
	Binary       string `json:"binary"`
	BinarySHA256 string `json:"binary_sha256"`
	Profile      string `json:"profile"`
	binaryPath   string `json:"-"`
}

type abTaskManifest struct {
	ID         string `json:"id"`
	SHA256     string `json:"sha256"`
	MaxSteps   int    `json:"max_steps"`
	TimeoutSec int    `json:"timeout_sec"`
}

type abRecord struct {
	SchemaVersion    int        `json:"schema_version"`
	Event            string     `json:"event"`
	RunID            string     `json:"run_id"`
	ArmID            string     `json:"arm_id"`
	TaskID           string     `json:"task_id"`
	Repetition       int        `json:"repetition"`
	Attempt          int        `json:"attempt"`
	StartedAt        string     `json:"started_at"`
	FinishedAt       string     `json:"finished_at"`
	DurationMS       int64      `json:"duration_ms"`
	Outcome          string     `json:"outcome"`
	Passed           bool       `json:"passed"`
	Scored           bool       `json:"scored"`
	MetricsAvailable bool       `json:"metrics_available"`
	Metrics          runMetrics `json:"metrics"`
	Note             string     `json:"note,omitempty"`
}

type abCellKey struct {
	armID      string
	taskID     string
	repetition int
}

func runHarnessAB(o harnessABOpts) error {
	if strings.TrimSpace(o.runDir) == "" {
		return errors.New("-run-dir is required")
	}
	if o.repetitions < 1 {
		return errors.New("-repetitions must be at least 1")
	}
	if o.infraRetries < 0 {
		return errors.New("-infra-retries cannot be negative")
	}
	if o.tokenBudget < 0 {
		return errors.New("-budget cannot be negative")
	}

	tasks, err := loadTasks(o.suite)
	if err != nil {
		return fmt.Errorf("load suite: %w", err)
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no tasks found under %s", filepath.Join(o.suite, "tasks"))
	}
	if err := os.MkdirAll(o.runDir, 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}

	manifest, err := prepareABManifest(o, tasks)
	if err != nil {
		return err
	}
	walPath := filepath.Join(o.runDir, "attempts.jsonl")
	records, err := loadABRecords(walPath, manifest)
	if err != nil {
		return err
	}
	latest := latestABRecords(records)
	if err := closeInterruptedABAdmissions(o.runDir, walPath, manifest, &records, &latest); err != nil {
		return err
	}
	if err := writeABProjections(o.runDir, manifest, records); err != nil {
		return err
	}
	spentTokens := abSpentTokens(records)
	maxAttempts := 1 + o.infraRetries
	for repetition := 1; repetition <= manifest.Repetitions; repetition++ {
		for taskIndex, t := range tasks {
			arms := append([]abArmManifest(nil), manifest.Arms...)
			if (taskIndex+repetition-1)%2 == 1 {
				arms[0], arms[1] = arms[1], arms[0]
			}
			for _, arm := range arms {
				key := abCellKey{armID: arm.ID, taskID: t.ID, repetition: repetition}
				previous, exists := latest[key]
				if exists && (previous.Scored || previous.Attempt >= maxAttempts) {
					continue
				}

				nextAttempt := 1
				if exists {
					nextAttempt = previous.Attempt + 1
				}
				for attempt := nextAttempt; attempt <= maxAttempts; attempt++ {
					if o.tokenBudget > 0 && spentTokens[arm.ID] >= o.tokenBudget {
						now := time.Now().UTC()
						r := abRecord{
							SchemaVersion: abSchemaVersion, Event: abEventAttemptFinished, RunID: manifest.RunID,
							ArmID: arm.ID, TaskID: t.ID, Repetition: repetition, Attempt: attempt,
							StartedAt: now.Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano),
							Outcome: abOutcomeSuiteBudgetExhausted, Scored: true,
							MetricsAvailable: true,
							Note:             fmt.Sprintf("per-arm token budget %d reached", o.tokenBudget),
						}
						if err := persistABRecord(o.runDir, walPath, manifest, &records, &latest, r); err != nil {
							return err
						}
						break
					}

					fmt.Fprintf(os.Stderr, "harness-ab: %s / %s / repetition %d / attempt %d\n", arm.ID, t.ID, repetition, attempt)
					admitted := time.Now().UTC()
					admission := abRecord{
						SchemaVersion: abSchemaVersion, Event: abEventAdmissionStarted, RunID: manifest.RunID,
						ArmID: arm.ID, TaskID: t.ID, Repetition: repetition, Attempt: attempt,
						StartedAt: admitted.Format(time.RFC3339Nano),
					}
					if err := persistABRecord(o.runDir, walPath, manifest, &records, &latest, admission); err != nil {
						return err
					}
					r := executeABTask(manifest.RunID, arm, t, repetition, attempt, o.model)
					if err := persistABRecord(o.runDir, walPath, manifest, &records, &latest, r); err != nil {
						return err
					}
					spentTokens[arm.ID] += r.Metrics.PromptTokens + r.Metrics.CompletionTokens
					if r.Scored {
						break
					}
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "harness-ab: report written to %s\n", filepath.Join(o.runDir, "report.md"))
	return nil
}

func prepareABManifest(o harnessABOpts, tasks []task) (abManifest, error) {
	manifestPath := filepath.Join(o.runDir, "manifest.json")
	existing, hasExisting, err := readABManifest(manifestPath)
	if err != nil {
		return abManifest{}, err
	}
	runID := strings.TrimSpace(o.runID)
	if hasExisting {
		if runID != "" && runID != existing.RunID {
			return abManifest{}, fmt.Errorf("run ID %q does not match frozen manifest run ID %q", runID, existing.RunID)
		}
		runID = existing.RunID
	}
	if runID == "" {
		runID = "reasonix-ab-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}

	manifest, err := buildABManifest(o, tasks, runID)
	if err != nil {
		return abManifest{}, err
	}
	if hasExisting {
		manifest.CreatedAt = existing.CreatedAt
		frozen := manifest
		frozen.Arms = append([]abArmManifest(nil), manifest.Arms...)
		for i := range frozen.Arms {
			frozen.Arms[i].binaryPath = ""
		}
		if !reflect.DeepEqual(existing, frozen) {
			return abManifest{}, errors.New("frozen manifest mismatch: suite, binary, model/environment, profile, repetitions, retry policy, or budget changed; use a new -run-dir")
		}
		return manifest, nil
	}
	if err := writeJSONAtomic(manifestPath, manifest); err != nil {
		return abManifest{}, fmt.Errorf("write manifest: %w", err)
	}
	return manifest, nil
}

func buildABManifest(o harnessABOpts, tasks []task, runID string) (abManifest, error) {
	suitePath, err := filepath.Abs(o.suite)
	if err != nil {
		return abManifest{}, fmt.Errorf("resolve suite path: %w", err)
	}
	suiteHash, err := hashTree(filepath.Join(suitePath, "tasks"))
	if err != nil {
		return abManifest{}, fmt.Errorf("fingerprint suite: %w", err)
	}
	baseline, err := buildABArm("baseline", o.baselineBin, o.baselineProfile)
	if err != nil {
		return abManifest{}, err
	}
	candidate, err := buildABArm("candidate", o.candidateBin, o.candidateProfile)
	if err != nil {
		return abManifest{}, err
	}
	taskManifests := make([]abTaskManifest, 0, len(tasks))
	for _, t := range tasks {
		hash, err := hashTree(t.dir)
		if err != nil {
			return abManifest{}, fmt.Errorf("fingerprint task %s: %w", t.ID, err)
		}
		taskManifests = append(taskManifests, abTaskManifest{
			ID: t.ID, SHA256: hash, MaxSteps: t.MaxSteps, TimeoutSec: t.TimeoutSec,
		})
	}
	return abManifest{
		SchemaVersion:   abSchemaVersion,
		HarnessProtocol: abHarnessProtocol,
		RunID:           runID,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		Suite:           abSuiteManifest{Path: publicABPathLabel(o.suite, suitePath), SHA256: suiteHash},
		Model:           o.model,
		EnvironmentID:   strings.TrimSpace(o.environmentID),
		Repetitions:     o.repetitions,
		InfraRetries:    o.infraRetries,
		TokenBudget:     o.tokenBudget,
		Arms:            []abArmManifest{baseline, candidate},
		Tasks:           taskManifests,
	}, nil
}

func buildABArm(id, binary, profile string) (abArmManifest, error) {
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return abArmManifest{}, fmt.Errorf("resolve %s binary %q: %w", id, binary, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return abArmManifest{}, fmt.Errorf("resolve %s binary path: %w", id, err)
	}
	if evaluated, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
		resolved = evaluated
	}
	hash, err := hashFile(resolved)
	if err != nil {
		return abArmManifest{}, fmt.Errorf("fingerprint %s binary: %w", id, err)
	}
	return abArmManifest{ID: id, Binary: filepath.Base(resolved), BinarySHA256: hash, Profile: profile, binaryPath: resolved}, nil
}

func publicABPathLabel(input, absolute string) string {
	cleaned := filepath.Clean(input)
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return filepath.Base(absolute)
	}
	return filepath.ToSlash(cleaned)
}

func readABManifest(path string) (abManifest, bool, error) {
	var manifest abManifest
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return manifest, false, nil
	}
	if err != nil {
		return manifest, false, fmt.Errorf("read manifest: %w", err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return manifest, false, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.SchemaVersion != abSchemaVersion {
		return manifest, false, fmt.Errorf("unsupported manifest schema_version %d", manifest.SchemaVersion)
	}
	if err := validateABManifest(manifest); err != nil {
		return manifest, false, fmt.Errorf("invalid manifest: %w", err)
	}
	return manifest, true, nil
}

func validateABManifest(manifest abManifest) error {
	if manifest.RunID == "" {
		return errors.New("run_id is empty")
	}
	if manifest.HarnessProtocol != abHarnessProtocol {
		return fmt.Errorf("unsupported harness_protocol %q", manifest.HarnessProtocol)
	}
	if manifest.Repetitions < 1 || manifest.InfraRetries < 0 || manifest.TokenBudget < 0 {
		return errors.New("invalid repetitions, infra_retries, or token_budget_per_arm")
	}
	if manifest.Suite.Path == "" || manifest.Suite.SHA256 == "" {
		return errors.New("suite label or digest is empty")
	}
	if len(manifest.Arms) != 2 || manifest.Arms[0].ID != "baseline" || manifest.Arms[1].ID != "candidate" {
		return errors.New("arms must be baseline followed by candidate")
	}
	for _, arm := range manifest.Arms {
		if arm.Binary == "" || arm.BinarySHA256 == "" {
			return fmt.Errorf("arm %q has an empty binary label or digest", arm.ID)
		}
		if _, err := normalizeBenchmarkProfile(arm.Profile); err != nil {
			return fmt.Errorf("arm %q: %w", arm.ID, err)
		}
	}
	seen := make(map[string]bool, len(manifest.Tasks))
	for _, task := range manifest.Tasks {
		if task.ID == "" || task.SHA256 == "" || task.TimeoutSec < 1 || seen[task.ID] {
			return fmt.Errorf("task %q has an empty ID/digest, invalid timeout, or duplicate ID", task.ID)
		}
		seen[task.ID] = true
	}
	if len(seen) == 0 {
		return errors.New("task list is empty")
	}
	return nil
}

func executeABTask(runID string, arm abArmManifest, t task, repetition, attempt int, model string) (r abRecord) {
	started := time.Now().UTC()
	r = abRecord{
		SchemaVersion: abSchemaVersion, Event: abEventAttemptFinished, RunID: runID, ArmID: arm.ID, TaskID: t.ID,
		Repetition: repetition, Attempt: attempt, StartedAt: started.Format(time.RFC3339Nano),
		Outcome: abOutcomeInfraFailed,
	}
	defer func() {
		finished := time.Now().UTC()
		r.FinishedAt = finished.Format(time.RFC3339Nano)
		r.DurationMS = finished.Sub(started).Milliseconds()
	}()

	verify := filepath.Join(t.dir, "verify.sh")
	if !fileExists(verify) {
		r.Note = "grader missing for task " + t.ID
		return r
	}
	work, err := os.MkdirTemp("", "e2ebench-ab-"+t.ID+"-")
	if err != nil {
		r.Note = "mktemp: " + err.Error()
		return r
	}
	defer os.RemoveAll(work)
	if seed := filepath.Join(t.dir, "workdir"); dirExists(seed) {
		if err := copyDir(seed, work); err != nil {
			r.Note = "copy seed: " + sanitizeABError(err, t.dir, "<task>", work, "<workdir>")
			return r
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.TimeoutSec)*time.Second)
	defer cancel()
	metricsPath := filepath.Join(work, ".run-metrics.json")
	args := []string{"run", "--metrics", metricsPath}
	if model != "" {
		args = append(args, "--model", model)
	}
	if t.MaxSteps > 0 {
		args = append(args, "--max-steps", fmt.Sprint(t.MaxSteps))
	}
	args = appendBenchmarkProfileArgs(args, arm.Profile)
	args = append(args, t.Prompt)

	cmd := exec.CommandContext(ctx, arm.binaryPath, args...)
	cmd.Dir = work
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 10 * time.Second
	runErr := cmd.Run()
	metrics, metricsErr := readMetrics(metricsPath)
	r.MetricsAvailable = metricsErr == nil
	if r.MetricsAvailable {
		r.Metrics = metrics
	}

	passed, gradeErr := gradeAB(work, t.dir)
	if gradeErr != nil {
		r.Note = "grader infrastructure: " + sanitizeABError(gradeErr, t.dir, "<task>", work, "<workdir>")
		return r
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		r.Outcome = abOutcomeTimeout
		r.Scored = true
		r.Note = "agent deadline exceeded"
		if metricsErr != nil {
			r.Note += "; metrics unavailable: " + sanitizeABError(metricsErr, metricsPath, "<workdir>/.run-metrics.json", work, "<workdir>")
		}
		return r
	}
	var startErr *exec.Error
	if errors.As(runErr, &startErr) {
		r.Note = "start agent: " + sanitizeABError(startErr, arm.binaryPath, "<binary>")
		return r
	}
	if metricsErr != nil {
		r.Note = "metrics infrastructure: " + sanitizeABError(metricsErr, metricsPath, "<workdir>/.run-metrics.json", work, "<workdir>")
		return r
	}
	r.Scored = true
	r.Passed = passed
	if passed {
		r.Outcome = abOutcomePassed
		if runErr != nil {
			r.Note = "agent exited non-zero but grader passed: " + runErr.Error()
		}
		return r
	}
	if runErr != nil {
		r.Outcome = abOutcomeAgentError
		r.Note = "agent: " + runErr.Error()
		return r
	}
	r.Outcome = abOutcomeVerificationFailed
	return r
}

func sanitizeABError(err error, replacements ...string) string {
	message := err.Error()
	for i := 0; i+1 < len(replacements); i += 2 {
		if replacements[i] != "" {
			message = strings.ReplaceAll(message, replacements[i], replacements[i+1])
		}
	}
	return message
}

func gradeAB(work, taskDir string) (bool, error) {
	verify := filepath.Join(taskDir, "verify.sh")
	dst := filepath.Join(work, "verify.sh")
	if err := copyFile(verify, dst); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "verify.sh")
	cmd.Dir = work
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return false, errors.New("grader deadline exceeded")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}

func persistABRecord(runDir, walPath string, manifest abManifest, records *[]abRecord, latest *map[abCellKey]abRecord, r abRecord) error {
	if err := appendABRecord(walPath, r); err != nil {
		return fmt.Errorf("append attempt: %w", err)
	}
	*records = append(*records, r)
	(*latest)[abCellKey{armID: r.ArmID, taskID: r.TaskID, repetition: r.Repetition}] = r
	if err := writeABProjections(runDir, manifest, *records); err != nil {
		return fmt.Errorf("refresh projections: %w", err)
	}
	return nil
}

func closeInterruptedABAdmissions(runDir, walPath string, manifest abManifest, records *[]abRecord, latest *map[abCellKey]abRecord) error {
	var dangling []abRecord
	for _, r := range *latest {
		if r.Event == abEventAdmissionStarted {
			dangling = append(dangling, r)
		}
	}
	sort.Slice(dangling, func(i, j int) bool {
		if dangling[i].TaskID != dangling[j].TaskID {
			return dangling[i].TaskID < dangling[j].TaskID
		}
		if dangling[i].Repetition != dangling[j].Repetition {
			return dangling[i].Repetition < dangling[j].Repetition
		}
		return dangling[i].ArmID < dangling[j].ArmID
	})
	for _, admission := range dangling {
		finished := time.Now().UTC()
		closed := admission
		closed.Event = abEventAttemptFinished
		closed.FinishedAt = finished.Format(time.RFC3339Nano)
		closed.Outcome = abOutcomeInfraFailed
		closed.Note = "interrupted after durable admission and before durable result"
		if started, err := time.Parse(time.RFC3339Nano, admission.StartedAt); err == nil {
			closed.DurationMS = finished.Sub(started).Milliseconds()
		}
		if err := persistABRecord(runDir, walPath, manifest, records, latest, closed); err != nil {
			return err
		}
	}
	return nil
}

func appendABRecord(path string, r abRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	syncParentBestEffort(path)
	return nil
}

func loadABRecords(path string, manifest abManifest) ([]abRecord, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read attempts: %w", err)
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		lastNewline := bytes.LastIndexByte(b, '\n')
		tail := b[lastNewline+1:]
		var candidate abRecord
		if json.Unmarshal(tail, &candidate) == nil {
			if err := appendWALNewline(path); err != nil {
				return nil, fmt.Errorf("repair attempts newline: %w", err)
			}
			b = append(b, '\n')
		} else {
			validLength := int64(lastNewline + 1)
			if err := truncateWAL(path, validLength); err != nil {
				return nil, fmt.Errorf("truncate torn attempt: %w", err)
			}
			b = b[:validLength]
		}
	}

	lines := bytes.Split(b, []byte{'\n'})
	records := make([]abRecord, 0, len(lines))
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r abRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("decode attempts line %d: %w", i+1, err)
		}
		if r.SchemaVersion != abSchemaVersion {
			return nil, fmt.Errorf("attempts line %d has unsupported schema_version %d", i+1, r.SchemaVersion)
		}
		if r.RunID != manifest.RunID {
			return nil, fmt.Errorf("attempts line %d belongs to run %q, want %q", i+1, r.RunID, manifest.RunID)
		}
		if err := validateABRecord(r, manifest); err != nil {
			return nil, fmt.Errorf("invalid attempts line %d: %w", i+1, err)
		}
		records = append(records, r)
	}
	return records, nil
}

func validateABRecord(r abRecord, manifest abManifest) error {
	if r.ArmID != "baseline" && r.ArmID != "candidate" {
		return fmt.Errorf("unknown arm_id %q", r.ArmID)
	}
	knownTask := false
	for _, task := range manifest.Tasks {
		if r.TaskID == task.ID {
			knownTask = true
			break
		}
	}
	if !knownTask {
		return fmt.Errorf("unknown task_id %q", r.TaskID)
	}
	if r.Repetition < 1 || r.Repetition > manifest.Repetitions || r.Attempt < 1 || r.Attempt > 1+manifest.InfraRetries {
		return errors.New("repetition or attempt is outside the frozen policy")
	}
	if r.Event == abEventAdmissionStarted {
		if r.StartedAt == "" || r.Outcome != "" || r.Scored || r.Passed || r.FinishedAt != "" {
			return errors.New("admission_started cannot contain a terminal result")
		}
		if _, err := time.Parse(time.RFC3339Nano, r.StartedAt); err != nil {
			return fmt.Errorf("invalid started_at: %w", err)
		}
		return nil
	}
	if r.Event != abEventAttemptFinished {
		return fmt.Errorf("unknown event %q", r.Event)
	}
	if r.StartedAt == "" || r.FinishedAt == "" {
		return errors.New("attempt_finished requires started_at and finished_at")
	}
	started, startErr := time.Parse(time.RFC3339Nano, r.StartedAt)
	finished, finishErr := time.Parse(time.RFC3339Nano, r.FinishedAt)
	if startErr != nil || finishErr != nil || finished.Before(started) || r.DurationMS < 0 {
		return errors.New("attempt_finished has invalid timestamps or duration")
	}
	if r.Metrics.PromptTokens < 0 || r.Metrics.CompletionTokens < 0 || r.Metrics.CacheHitTokens < 0 ||
		r.Metrics.CacheMissTokens < 0 || r.Metrics.Cost < 0 {
		return errors.New("attempt_finished has negative metrics")
	}
	validOutcome := r.Outcome == abOutcomePassed || r.Outcome == abOutcomeVerificationFailed ||
		r.Outcome == abOutcomeAgentError || r.Outcome == abOutcomeTimeout ||
		r.Outcome == abOutcomeSuiteBudgetExhausted || r.Outcome == abOutcomeInfraFailed
	if !validOutcome {
		return fmt.Errorf("unknown outcome %q", r.Outcome)
	}
	if r.Scored == (r.Outcome == abOutcomeInfraFailed) {
		return errors.New("infra_failed must be unscored and all other outcomes must be scored")
	}
	if r.Passed != (r.Outcome == abOutcomePassed) {
		return errors.New("passed must match the passed outcome")
	}
	return nil
}

func appendWALNewline(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func truncateWAL(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func latestABRecords(records []abRecord) map[abCellKey]abRecord {
	latest := make(map[abCellKey]abRecord)
	for _, r := range records {
		key := abCellKey{armID: r.ArmID, taskID: r.TaskID, repetition: r.Repetition}
		if previous, ok := latest[key]; !ok || r.Attempt >= previous.Attempt {
			latest[key] = r
		}
	}
	return latest
}

func abSpentTokens(records []abRecord) map[string]int {
	spent := make(map[string]int)
	for _, r := range records {
		if r.Event != abEventAttemptFinished {
			continue
		}
		spent[r.ArmID] += r.Metrics.PromptTokens + r.Metrics.CompletionTokens
	}
	return spent
}

func hashTree(root string) (string, error) {
	type entry struct {
		path  string
		mode  fs.FileMode
		isDir bool
	}
	var entries []entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel != "." {
			entries = append(entries, entry{path: filepath.ToSlash(rel), mode: info.Mode().Perm(), isDir: d.IsDir()})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	h := sha256.New()
	for _, e := range entries {
		kind := "file"
		if e.isDir {
			kind = "dir"
		}
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%04o\x00", e.path, kind, e.mode)
		if e.isDir {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.path)))
		if err != nil {
			return "", err
		}
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func writeJSONAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFileAtomic(path, b, 0o644)
}

func writeFileAtomic(path string, body []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".e2ebench-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	syncParentBestEffort(path)
	return nil
}

func syncParentBestEffort(path string) {
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
}
