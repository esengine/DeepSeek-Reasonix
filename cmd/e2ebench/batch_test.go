package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// fixtureRows is a synthetic batch: three solvable tasks, one of them solved on
// a retry. Numbers are chosen so every headline figure is non-zero and the
// ledger has one of each interesting category.
func fixtureRows() []result {
	return []result{
		{
			task:       task{ID: "task-a", Prompt: "fixture prompt a"},
			runMetrics: runMetrics{PromptTokens: 1000, CompletionTokens: 100, CacheHitTokens: 600, CacheMissTokens: 400, Cost: 0.01, Currency: "USD"},
			Attempt:    1, Passed: true, WallMs: 1200, TTCSMs: 1200,
		},
		{
			task:       task{ID: "task-b", Prompt: "fixture prompt b"},
			runMetrics: runMetrics{PromptTokens: 2000, CompletionTokens: 200, CacheHitTokens: 1500, CacheMissTokens: 500, Cost: 0.02, Currency: "USD"},
			Attempt:    1, Passed: true, WallMs: 800, TTCSMs: 800,
		},
		{
			task:       task{ID: "task-c", Prompt: "fixture prompt c"},
			runMetrics: runMetrics{PromptTokens: 3000, CompletionTokens: 300, CacheHitTokens: 0, CacheMissTokens: 3000, Cost: 0.03, Currency: "USD"},
			Attempt:    1, Passed: false, WallMs: 500,
		},
		{
			task:       task{ID: "task-c"},
			runMetrics: runMetrics{PromptTokens: 500, CompletionTokens: 50, Cost: 0.005, Currency: "USD"},
			Attempt:    2, Passed: true, WallMs: 400, TTCSMs: 900,
		},
	}
}

func fixtureProvenance(dir string) batchProvenance {
	count, digest := taskSetDigest([]task{{ID: "task-a"}, {ID: "task-b"}, {ID: "task-c"}})
	return batchProvenance{
		dir:     dir,
		started: timeAt("2026-10-28T00:00:00Z"),
		planned: count,
		axes:    []batchFlag{{"profile", "baseline"}, {"trials", "1"}},
		tasks:   batchTaskSet{Count: count, Digest: digest},
		commit:  "0123456789abcdef0123456789abcdef01234567",
		dirty:   new(false),
		model:   "deepseek-flash",
	}
}

func timeAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func writeFixtureBatch(t *testing.T) (string, batchManifest) {
	t.Helper()
	dir := t.TempDir()
	if err := writeBatch(fixtureProvenance(dir), fixtureRows()); err != nil {
		t.Fatalf("write batch: %v", err)
	}
	return dir, readManifest(t, dir)
}

func readManifest(t *testing.T, dir string) batchManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, batchManifestName))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m batchManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return m
}

func writeManifest(t *testing.T, dir string, m batchManifest) {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, batchManifestName), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// writeRows rewrites results.jsonl without touching the manifest, which is what
// a tampered batch looks like.
func writeRows(t *testing.T, dir string, rows []result) {
	t.Helper()
	var b bytes.Buffer
	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("encode row: %v", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, batchResultsName), b.Bytes(), 0o600); err != nil {
		t.Fatalf("write rows: %v", err)
	}
}

// A batch written by the producer audits clean: the figures the manifest
// published are the figures the retained rows give.
func TestBatchAuditRoundTrip(t *testing.T) {
	dir, m := writeFixtureBatch(t)
	if m.SchemaVersion != batchSchemaVersion || m.Scope == "" {
		t.Fatalf("manifest schema = %d scope = %q, want v%d with a named claim", m.SchemaVersion, m.Scope, batchSchemaVersion)
	}
	if m.Over.Denominator.Value != 3 || m.Over.Denominator.Unit != denominatorUnit || m.Over.Denominator.Definition != denominatorDefinition {
		t.Fatalf("denominator = %+v, want 3 tasks under the harness definition", m.Over.Denominator)
	}
	if m.Producer.Commit != "0123456789abcdef0123456789abcdef01234567" || m.Producer.CommitSource != "checkout" {
		t.Fatalf("producer = %+v, want the checkout commit recorded", m.Producer)
	}
	if m.Trust.Ledger.Planned == nil || *m.Trust.Ledger.Planned != 3 {
		t.Fatalf("planned = %v, want 3", m.Trust.Ledger.Planned)
	}
	if m.Trust.Ledger.Scored == nil || *m.Trust.Ledger.Scored != 3 {
		t.Fatalf("scored = %v, want 3", m.Trust.Ledger.Scored)
	}
	if m.Trust.Ledger.Attempted == nil || *m.Trust.Ledger.Attempted != 4 || m.Trust.Ledger.Excluded == nil || *m.Trust.Ledger.Excluded != 1 {
		t.Fatalf("ledger attempted/excluded = %v/%v, want 4/1", m.Trust.Ledger.Attempted, m.Trust.Ledger.Excluded)
	}

	// The manifest must not carry the raw task text the rows legitimately do.
	raw, err := os.ReadFile(filepath.Join(dir, batchManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("fixture prompt")) {
		t.Fatalf("the manifest carries raw task text:\n%s", raw)
	}

	out, err := auditBatch(dir)
	if err != nil {
		t.Fatalf("a producer-written batch did not audit: %v\n%s", err, out)
	}
	for _, want := range []string{
		"| solved | 3 | 3 |",
		"| pass1 | 2 | 2 |",
		"| prompt_tokens | 6500 | 6500 |",
		"| cost | 0.065 | 0.065 |",
		"| scored | 3 | 3 |",
		"| failed | 1 | 1 |",
		"| planned | 3 | n/a |",
		"results_sha256: ok",
		"the published figures follow from the retained rows.",
		"aggregation audit, not a reproduction",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("audit output is missing %q:\n%s", want, out)
		}
	}
	receipt := readManifest(t, dir).Audit
	if receipt == nil || receipt.VerifierVersion != auditToolVersion || receipt.ResultsSHA256 != m.Trust.ResultsSHA256 {
		t.Fatalf("audit receipt = %+v, want verifier v%d over the same digest", receipt, auditToolVersion)
	}
}

// The axes carry what changes a figure's meaning, never the operator's machine:
// meter/tape/trajectory paths and steer text stay out of the manifest.
func TestBatchAxesKeepPathsAndFreeTextOut(t *testing.T) {
	dir := t.TempDir()
	prov := fixtureProvenance(dir)
	prov.axes = batchAxes(suiteConfig{
		profile: "delivery", cacheArm: "warm", effort: "high", anchor: "blind",
		attempts: 1, trials: 2, budget: 0, segments: 3,
		meterConfig: "/Users/someone/.reasonix/config.toml",
		meterFaults: faultScript{at: map[int]int{3: 429}},
		tape:        tapeConfig{mode: tapeRecord, root: "/tmp/tapes"},
		steers:      map[int]string{2: "secret steer text"},
		trajDir:     "/tmp/traj",
	})
	if err := writeBatch(prov, fixtureRows()); err != nil {
		t.Fatalf("write batch: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, batchManifestName))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/Users", "/tmp", "secret steer text", ".reasonix"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("the manifest carries %q:\n%s", forbidden, raw)
		}
	}
	axes := map[string]string{}
	for _, a := range readManifest(t, dir).Over.Command.Axes {
		axes[a.Name] = a.Value
	}
	want := map[string]string{
		"profile": "delivery", "effort": "high", "trials": "2", "segments": "3",
		"meter": "on", "faults": "on", "tape": "record", "steers": "1", "ablate": "full",
	}
	for name, value := range want {
		if axes[name] != value {
			t.Fatalf("axis %s = %q, want %q (axes: %v)", name, axes[name], value, axes)
		}
	}
}

// The corpus digest is a set digest: order must not move it, and ids that only
// differ by a separator must not collide.
func TestTaskSetDigestIsSeparatedAndOrderIndependent(t *testing.T) {
	n1, d1 := taskSetDigest([]task{{ID: "b"}, {ID: "a"}})
	n2, d2 := taskSetDigest([]task{{ID: "a"}, {ID: "b"}})
	if n1 != 2 || n2 != 2 || d1 != d2 {
		t.Fatalf("order moved the corpus digest: %d/%s vs %d/%s", n1, d1, n2, d2)
	}
	_, joined := taskSetDigest([]task{{ID: "a\nb"}})
	if joined == d1 {
		t.Fatal("a newline inside one id collides with two ids")
	}
}

// One token count moved after publication: the audit must fail and name the
// figure that moved, not just say the digest differs.
func TestBatchAuditNamesTheFigureThatMoved(t *testing.T) {
	dir, m := writeFixtureBatch(t)
	tampered := fixtureRows()
	tampered[0].PromptTokens += 1000
	writeRows(t, dir, tampered)

	out, err := auditBatch(dir)
	if err == nil {
		t.Fatalf("a tampered row audited clean:\n%s", out)
	}
	for _, want := range []string{
		"prompt_tokens: manifest 6500, rows 7500",
		"| prompt_tokens | 6500 | 7500 | **moved** |",
		"MISMATCH",
		"the published figures do **not** follow from the retained rows",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("audit output is missing %q:\n%s", want, out)
		}
	}
	if readManifest(t, dir).Audit != nil {
		t.Fatal("a failed audit recorded a verification receipt")
	}
	if m.Trust.ResultsSHA256 == "" {
		t.Fatal("fixture manifest carries no digest")
	}
}

// The three ways a batch can be unauditable are three different messages: no
// manifest, no commit to tie the figures to, and rows cut short.
func TestBatchAuditRefusesUnverifiableBatches(t *testing.T) {
	t.Run("missing manifest", func(t *testing.T) {
		dir := t.TempDir()
		writeRows(t, dir, fixtureRows())
		out, err := auditBatch(dir)
		if err == nil {
			t.Fatalf("rows without a manifest audited clean:\n%s", out)
		}
		if !strings.Contains(err.Error(), "no batch.json") || !strings.Contains(err.Error(), "kept no manifest") {
			t.Fatalf("missing-manifest error = %v", err)
		}
	})

	t.Run("unknown commit", func(t *testing.T) {
		dir, m := writeFixtureBatch(t)
		m.Producer.Commit = "unknown"
		m.Producer.CommitSource = ""
		writeManifest(t, dir, m)
		out, err := auditBatch(dir)
		if err == nil {
			t.Fatalf("a manifest with an unknown commit audited clean:\n%s", out)
		}
		if !strings.Contains(err.Error(), "records no commit") {
			t.Fatalf("no-commit error = %v", err)
		}
	})

	t.Run("absent commit", func(t *testing.T) {
		dir, m := writeFixtureBatch(t)
		m.Producer.Commit = ""
		writeManifest(t, dir, m)
		if _, err := auditBatch(dir); err == nil || !strings.Contains(err.Error(), "records no commit") {
			t.Fatalf("an absent commit answered %v, want the no-commit refusal", err)
		}
	})

	t.Run("truncated rows", func(t *testing.T) {
		dir, _ := writeFixtureBatch(t)
		path := filepath.Join(dir, batchResultsName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data[:len(data)-40], 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := auditBatch(dir)
		if err == nil {
			t.Fatalf("truncated rows audited clean:\n%s", out)
		}
		if !strings.Contains(err.Error(), "truncated") {
			t.Fatalf("truncated-rows error = %v", err)
		}
	})

	t.Run("half a row with a newline", func(t *testing.T) {
		dir, _ := writeFixtureBatch(t)
		path := filepath.Join(dir, batchResultsName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cut := bytes.LastIndexByte(data[:len(data)-1], '\n')
		if cut < 0 {
			t.Fatal("fixture has fewer than two rows")
		}
		broken := append(append([]byte{}, data[:cut+1]...), data[cut+1:len(data)-20]...)
		broken = append(broken, '\n')
		if err := os.WriteFile(path, broken, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := auditBatch(dir); err == nil || !strings.Contains(err.Error(), "not a complete row") {
			t.Fatalf("a half-written row answered %v, want a truncation refusal", err)
		}
	})
}

// manifestWithoutOptionals is pinned JSON, not a re-marshal of the writer's
// struct: seed, provider, dirty and cost are absent, and the audit must read
// each as unknown rather than as zero. This is the record the contract is
// about, so it is written out in full.
const manifestWithoutOptionals = `{
  "schema_version": 1,
  "scope": %q,
  "over": {
    "command": {"mode": "suite", "axes": [{"name": "profile", "value": "baseline"}, {"name": "trials", "value": "1"}]},
    "tasks": {"count": 3, "digest": %q},
    "denominator": {"unit": "task", "definition": %q, "value": 3}
  },
  "producer": {
    "commit": "0123456789abcdef0123456789abcdef01234567",
    "commit_source": "checkout",
    "model": "deepseek-flash",
    "started_at": "2026-10-28T00:00:00Z",
    "finished_at": "2026-10-28T00:10:00Z",
    "harness_version": "(devel)",
    "go_version": "go1.26.5"
  },
  "trust": {
    "results_sha256": %q,
    "claimed": {
      "solved": 3,
      "pass1": 2,
      "prompt_tokens": 6500,
      "completion_tokens": 650,
      "cache_hit_tokens": 2100,
      "cache_miss_tokens": 3900,
      "accounted": 4,
      "unaccounted": 0,
      "partial": 0,
      "tcts_median_ms": 900
    },
    "ledger": {
      "planned": 3,
      "attempted": 4,
      "completed": 4,
      "scored": 3,
      "failed": 1,
      "unmeasured": 0,
      "excluded": 1,
      "rows": 4,
      "skipped": 0
    },
    "retention": %q
  }
}
`

func TestBatchAuditReadsAbsentFieldsAsUnknown(t *testing.T) {
	dir := t.TempDir()
	writeRows(t, dir, fixtureRows())
	_, canonical, err := readBatch(filepath.Join(dir, batchResultsName))
	if err != nil {
		t.Fatal(err)
	}
	_, digest := taskSetDigest([]task{{ID: "task-a"}, {ID: "task-b"}, {ID: "task-c"}})
	manifest := fmt.Sprintf(manifestWithoutOptionals, batchScope, digest, denominatorDefinition, hashOf(canonical), batchRetention)
	if err := os.WriteFile(filepath.Join(dir, batchManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := auditBatch(dir)
	if err != nil {
		t.Fatalf("a manifest with only optional fields absent did not audit: %v\n%s", err, out)
	}
	for _, want := range []string{
		"seed: unknown (absent from the manifest; an absent seed is not seed 0)",
		"provider: unknown (absent from the manifest; absent is not zero)",
		"| cost | unknown | 0.065 | not claimed |",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("audit output is missing %q:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"seed: 0", "| cost | 0 |", "provider ``"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("an absent field read as %q:\n%s", forbidden, out)
		}
	}
}

// The digest covers values, not spelling: CRLF line endings and reordered keys
// are the same rows, so the same manifest still audits.
func TestBatchDigestIgnoresLineEndingsAndKeyOrder(t *testing.T) {
	dir, m := writeFixtureBatch(t)
	path := filepath.Join(dir, batchResultsName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rewritten bytes.Buffer
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		rewritten.Write(reorderKeys(t, line))
	}
	if !bytes.Contains(rewritten.Bytes(), []byte("\r\n")) {
		t.Fatal("fixture rewrite did not use CRLF")
	}
	if bytes.Equal(rewritten.Bytes(), data) {
		t.Fatal("fixture rewrite left the bytes unchanged, so it proves nothing")
	}
	if err := os.WriteFile(path, rewritten.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	_, canonical, err := readBatch(path)
	if err != nil {
		t.Fatalf("re-spelled rows did not parse: %v", err)
	}
	if got := hashOf(canonical); got != m.Trust.ResultsSHA256 {
		t.Fatalf("digest moved with spelling: %s, want %s", got, m.Trust.ResultsSHA256)
	}
	if out, err := auditBatch(dir); err != nil {
		t.Fatalf("re-spelled rows did not audit: %v\n%s", err, out)
	}
}

// reorderKeys emits the row with its keys in reverse order and a CRLF ending:
// both differences are outside the values results_sha256 covers.
func reorderKeys(t *testing.T, line []byte) []byte {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(obj[k])
	}
	b.WriteString("}\r\n")
	return b.Bytes()
}

// A ledger is a funnel. Counts that cannot all be true together are rejected,
// so the published denominator cannot be larger than the runs behind it.
func TestBatchAuditRejectsALedgerThatDoesNotAddUp(t *testing.T) {
	t.Run("scored above completed", func(t *testing.T) {
		dir, m := writeFixtureBatch(t)
		m.Trust.Ledger.Scored = new(5)
		writeManifest(t, dir, m)
		out, err := auditBatch(dir)
		if err == nil {
			t.Fatalf("a ledger with scored > completed audited clean:\n%s", out)
		}
		if !strings.Contains(out, "ledger: scored 5 exceeds completed 4") {
			t.Fatalf("audit output does not name the broken invariant:\n%s", out)
		}
	})

	t.Run("denominator is not passes plus failures", func(t *testing.T) {
		dir, m := writeFixtureBatch(t)
		m.Trust.Ledger.Failed = new(0)
		writeManifest(t, dir, m)
		out, err := auditBatch(dir)
		if err == nil {
			t.Fatalf("a ledger whose denominator is not pass1+failed audited clean:\n%s", out)
		}
		if !strings.Contains(out, "ledger: scored 3 is not pass1 2 + failed 0") {
			t.Fatalf("audit output does not name the denominator split:\n%s", out)
		}
	})
}
