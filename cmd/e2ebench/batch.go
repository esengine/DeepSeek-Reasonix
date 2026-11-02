package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/contract/config"
)

const (
	batchSchemaVersion = 1
	batchResultsName   = "results.jsonl"
	batchManifestName  = "batch.json"
	// auditToolVersion is the version of the code below; every successful
	// audit stamps it into the manifest, so a later reader can tell which
	// verifier checked the batch.
	auditToolVersion = 1
	auditToolName    = "e2ebench-audit"
	// batchScope names the claim: an aggregation audit, not a replay.
	batchScope = "aggregation audit: the claimed figures are recomputed from the retained rows; the model, tools and environment are not re-executed, so this is not a reproduction of the experiment"
	// denominatorUnit and denominatorDefinition say what pass@1 divides by.
	denominatorUnit       = "task"
	denominatorDefinition = "first attempts of solvable, non-skipped tasks: pass@1 divides passes by this count, never by the retained row count"
	// batchRetention is the one line a reader needs before sharing the batch.
	batchRetention = "results.jsonl is retained unredacted beside this manifest (task prompts and agent notes included) and is not public; the manifest carries a task-id digest, no prompt text, no absolute paths, no endpoints or keys and no model output"
)

// batchOver is what the figures are over: the command, the task set identity,
// the filters, and the denominator the accuracy is divided by.
type batchOver struct {
	Command     batchCommand     `json:"command"`
	Tasks       batchTaskSet     `json:"tasks"`
	Filters     batchFilters     `json:"filters"`
	Denominator batchDenominator `json:"denominator"`
}

// batchCommand records the mode and every flag that changes what a figure
// means. Output paths are deliberately absent: they are not evidence, and an
// absolute path is the operator's, not the batch's.
type batchCommand struct {
	Mode string      `json:"mode"`
	Axes []batchFlag `json:"axes"`
}

type batchFlag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// batchTaskSet identifies the corpus without carrying its text: a stable
// digest over the sorted task ids and how many there were.
type batchTaskSet struct {
	Count  int    `json:"count"`
	Digest string `json:"digest"`
}

type batchFilters struct {
	// Task is -task as given; empty means no filter.
	Task string `json:"task,omitempty"`
}

// batchDenominator names the unit pass@1 divides by, so a row count can never
// stand in for it.
type batchDenominator struct {
	Unit       string `json:"unit"`
	Definition string `json:"definition"`
	Value      int    `json:"value"`
}

// batchProducer is which run produced the figures. Absent optional fields are
// unknown, never zero: a missing seed is not seed 0.
type batchProducer struct {
	Commit         string `json:"commit"`
	CommitSource   string `json:"commit_source,omitempty"`
	Dirty          *bool  `json:"dirty,omitempty"`
	Model          string `json:"model,omitempty"`
	Provider       string `json:"provider,omitempty"`
	Seed           *int64 `json:"seed,omitempty"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at"`
	HarnessVersion string `json:"harness_version"`
	GoVersion      string `json:"go_version"`
}

// batchTrust is why the figures can still be checked: the digest of the rows,
// the headline figures as published, and the ledger every planned run is
// accounted for in.
type batchTrust struct {
	ResultsSHA256 string      `json:"results_sha256"`
	Claimed       batchClaims `json:"claimed"`
	Ledger        batchLedger `json:"ledger"`
	Retention     string      `json:"retention"`
}

// batchClaims are the headline figures the report publishes. A figure the run
// could not measure is absent, not zero.
type batchClaims struct {
	Solved           *int     `json:"solved"`
	Pass1            *int     `json:"pass1"`
	PromptTokens     *int     `json:"prompt_tokens,omitempty"`
	CompletionTokens *int     `json:"completion_tokens,omitempty"`
	CacheHitTokens   *int     `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens  *int     `json:"cache_miss_tokens,omitempty"`
	Cost             *float64 `json:"cost,omitempty"`
	Currency         string   `json:"currency,omitempty"`
	Accounted        *int     `json:"accounted"`
	Unaccounted      *int     `json:"unaccounted"`
	Partial          *int     `json:"partial"`
	TCTSMedianMs     *int64   `json:"tcts_median_ms,omitempty"`
}

// batchLedger accounts for every planned run. Rows is the retained record, not
// a denominator; Scored is the denominator.
type batchLedger struct {
	Planned     *int              `json:"planned"`
	Attempted   *int              `json:"attempted"`
	Completed   *int              `json:"completed"`
	Scored      *int              `json:"scored"`
	Failed      *int              `json:"failed"`
	Unmeasured  *int              `json:"unmeasured"`
	Excluded    *int              `json:"excluded"`
	Rows        *int              `json:"rows,omitempty"`
	Skipped     *int              `json:"skipped,omitempty"`
	Definitions map[string]string `json:"definitions,omitempty"`
}

// batchAuditReceipt is what a successful audit leaves behind.
type batchAuditReceipt struct {
	VerifierVersion int    `json:"verifier_version"`
	VerifiedAt      string `json:"verified_at"`
	ResultsSHA256   string `json:"results_sha256"`
}

// batchManifest is written beside results.jsonl so the report never has to be
// reconstructed from a terminal scrollback.
type batchManifest struct {
	SchemaVersion int                `json:"schema_version"`
	Scope         string             `json:"scope"`
	Over          batchOver          `json:"over"`
	Producer      batchProducer      `json:"producer"`
	Trust         batchTrust         `json:"trust"`
	Audit         *batchAuditReceipt `json:"audit,omitempty"`
}

// ledgerDefinitions explains each count in the manifest itself, so the
// denominator cannot be reinterpreted after the fact.
var ledgerDefinitions = map[string]string{
	"planned":    "graded runs the batch set out to attempt: filtered tasks x trials",
	"attempted":  "retained rows for runs that started (skipped rows are not attempts)",
	"completed":  "attempts that ended and left a row; this harness writes a row only after an attempt ends",
	"scored":     "completed first attempts of solvable, non-skipped tasks: the pass@1 denominator",
	"failed":     "scored attempts the grader did not pass",
	"unmeasured": "attempts whose cost and tokens are unknown because no metrics landed; kept out of spend totals",
	"excluded":   "rows kept out of the accuracy denominator: skipped, no_solution, and retry entries",
	"rows":       "rows retained in results.jsonl; a record of what was kept, never the denominator",
	"skipped":    "planned runs deliberately not started (anchor has no seed, or the token budget ran out)",
}

// batchProvenance is what the batch knows before any run happens. Everything
// else in the manifest is derived from the rows that were written.
type batchProvenance struct {
	dir      string
	started  time.Time
	planned  int
	axes     []batchFlag
	tasks    batchTaskSet
	filters  batchFilters
	commit   string
	dirty    *bool
	model    string
	provider string
}

// captureBatchProvenance reads the checkout and model identity before the
// batch runs: git cannot describe a tree the agent has since changed.
func captureBatchProvenance(dir string, cfg suiteConfig, tasks []task, taskFilter string) batchProvenance {
	commit, dirty := checkoutCommit(".")
	model, provider := modelIdentity(cfg.model)
	count, digest := taskSetDigest(tasks)
	return batchProvenance{
		dir: dir, started: time.Now().UTC(), planned: count * max(cfg.trials, 1),
		axes: batchAxes(cfg), tasks: batchTaskSet{Count: count, Digest: digest},
		filters: batchFilters{Task: taskFilter},
		commit:  commit, dirty: dirty, model: model, provider: provider,
	}
}

// writeBatch writes the rows first and the manifest second, so a manifest
// never describes rows that are not beside it.
func writeBatch(p batchProvenance, results []result) error {
	if err := os.MkdirAll(p.dir, 0o750); err != nil {
		return fmt.Errorf("create batch directory: %w", err)
	}
	var raw, canonical bytes.Buffer
	for _, r := range results {
		line, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("encode row: %w", err)
		}
		raw.Write(line)
		raw.WriteByte('\n')
		canon, err := canonicalJSON(line)
		if err != nil {
			return err
		}
		canonical.Write(canon)
		canonical.WriteByte('\n')
	}
	// Rows carry task prompts and agent notes, so they stay the operator's.
	if err := os.WriteFile(filepath.Join(p.dir, batchResultsName), raw.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", batchResultsName, err)
	}
	manifest := buildBatchManifest(p, results, canonical.Bytes())
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(p.dir, batchManifestName), append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", batchManifestName, err)
	}
	return nil
}

func buildBatchManifest(p batchProvenance, results []result, canonical []byte) batchManifest {
	stats := gatherSuiteStats(results)
	harness, goVersion := harnessIdentity()
	source := ""
	if p.commit != "unknown" {
		source = "checkout"
	}
	return batchManifest{
		SchemaVersion: batchSchemaVersion,
		Scope:         batchScope,
		Over: batchOver{
			Command:     batchCommand{Mode: "suite", Axes: p.axes},
			Tasks:       p.tasks,
			Filters:     p.filters,
			Denominator: batchDenominator{Unit: denominatorUnit, Definition: denominatorDefinition, Value: stats.ran},
		},
		Producer: batchProducer{
			Commit: p.commit, CommitSource: source, Dirty: p.dirty,
			Model: p.model, Provider: p.provider,
			StartedAt: p.started.Format(time.RFC3339), FinishedAt: time.Now().UTC().Format(time.RFC3339),
			HarnessVersion: harness, GoVersion: goVersion,
		},
		Trust: batchTrust{
			ResultsSHA256: hashOf(canonical),
			Claimed:       claimsFor(stats),
			Ledger:        ledgerFor(results, p.planned),
			Retention:     batchRetention,
		},
	}
}

// claimsFor publishes the report's headline figures. Without a single
// accounted run there is no measured spend: those fields stay absent.
func claimsFor(s suiteStats) batchClaims {
	c := batchClaims{
		Solved: new(s.passed), Pass1: new(s.pass1),
		Accounted: new(s.accounted), Unaccounted: new(s.unaccounted), Partial: new(s.partial),
	}
	if s.accounted > 0 {
		c.PromptTokens, c.CompletionTokens = new(s.pTok), new(s.cTok)
		c.Cost, c.Currency = new(s.cost), s.currency
		if s.hit+s.miss > 0 {
			c.CacheHitTokens, c.CacheMissTokens = new(s.hit), new(s.miss)
		}
	}
	if s.passed > 0 {
		c.TCTSMedianMs = new(median(s.ttcs))
	}
	return c
}

// ledgerFor accounts for every planned run. A retry is a row, not a planned
// unit: the plan is tasks x trials, which is why Scored and Attempted differ.
func ledgerFor(results []result, planned int) batchLedger {
	l := ledgerFromRows(results)
	l.Planned = new(planned)
	l.Definitions = ledgerDefinitions
	return l
}

func ledgerFromRows(results []result) batchLedger {
	var attempted, completed, scored, failed, unmeasured, excluded, skipped int
	for _, r := range results {
		inDenominator := !r.Skipped && !r.NoSolution && r.Attempt <= 1
		if r.Skipped {
			skipped++
			excluded++
			continue
		}
		attempted++
		completed++
		if r.Unaccounted {
			unmeasured++
		}
		if !inDenominator {
			excluded++
			continue
		}
		scored++
		if !r.Passed {
			failed++
		}
	}
	return batchLedger{
		Attempted: new(attempted), Completed: new(completed), Scored: new(scored),
		Failed: new(failed), Unmeasured: new(unmeasured), Excluded: new(excluded),
		Rows: new(len(results)), Skipped: new(skipped),
	}
}

// canonicalJSON is what results_sha256 covers: line endings, indentation and
// key order are dropped because none of them is evidence, while a changed value
// or a missing row still changes the digest. Number spelling is kept, so a
// rewritten 1 is not silently equal to 1.0.
func canonicalJSON(line []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var row any
	if err := dec.Decode(&row); err != nil {
		return nil, fmt.Errorf("row is not valid JSON: %w", err)
	}
	return json.Marshal(row)
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// checkoutCommit reports the checkout the batch runs from. Unknown is written
// explicitly rather than guessed: a directory git cannot describe is not a
// commit, and a dirty tree is not the commit it names.
func checkoutCommit(dir string) (string, *bool) {
	commit := "unknown"
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err == nil {
		if sha := strings.TrimSpace(string(out)); sha != "" {
			commit = sha
		}
	}
	var dirty *bool
	out, err = exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err == nil {
		state := strings.TrimSpace(string(out)) != ""
		dirty = &state
	}
	return commit, dirty
}

// modelIdentity resolves the model and provider names the agent will run
// under. Names only: no endpoint, key or credential reaches the manifest.
func modelIdentity(flagModel string) (model, provider string) {
	ref := strings.TrimSpace(flagModel)
	cfg, err := config.Load()
	if err == nil && cfg != nil {
		if resolved, _, ok := cfg.ResolveModelWithFallback(ref); ok {
			if name, rest, found := strings.Cut(resolved, "/"); found {
				return rest, name
			}
			return resolved, ""
		}
		if ref == "" {
			ref = strings.TrimSpace(cfg.DefaultModel)
		}
	}
	if name, rest, found := strings.Cut(ref, "/"); found {
		return rest, name
	}
	return ref, ""
}

func harnessIdentity() (string, string) {
	version := "(devel)"
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		version = bi.Main.Version
	}
	return version, runtime.Version()
}

// taskSetDigest identifies the corpus by sorted id, not by name order: a
// reordered -task list is the same set. Ids are NUL-separated because a path
// name cannot contain NUL, so no two id sets share a digest string.
func taskSetDigest(tasks []task) (int, string) {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	return len(ids), "sha256:" + hex.EncodeToString(sum[:])
}

// batchAxes lists the flags that change what a figure means, with their
// effective values. Paths and free text stay out: -steer records its count.
func batchAxes(cfg suiteConfig) []batchFlag {
	tape := "off"
	switch cfg.tape.mode {
	case tapeRecord:
		tape = "record"
	case tapeReplay:
		tape = "replay"
	}
	return []batchFlag{
		{"profile", cfg.profile},
		{"ablate", cfg.arm.Arm()},
		{"cache", cfg.cacheArm},
		{"anchor", cfg.anchor},
		{"effort", valueOr(cfg.effort, "default")},
		{"policy", valueOr(cfg.policy, "baseline")},
		{"force-planner", formatBool(cfg.forcePlanner)},
		{"checkpoints", formatBool(cfg.checkpoints)},
		{"attempts", strconv.Itoa(max(cfg.attempts, 1))},
		{"trials", strconv.Itoa(max(cfg.trials, 1))},
		{"budget", strconv.Itoa(cfg.budget)},
		{"segments", strconv.Itoa(max(cfg.segments, 1))},
		{"steers", strconv.Itoa(len(cfg.steers))},
		{"meter", onOff(cfg.meterConfig != "")},
		{"faults", onOff(!cfg.meterFaults.empty())},
		{"tape", tape},
		{"reply-language", valueOr(cfg.replyLanguage, "auto")},
	}
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func formatBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
