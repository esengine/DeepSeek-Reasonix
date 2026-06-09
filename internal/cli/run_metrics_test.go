package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

func TestMetricsSinkAccumulatesReadinessAudit(t *testing.T) {
	s := &metricsSink{inner: event.Discard}

	s.RecordReadinessAudit(evidence.ReadinessAudit{
		Result:                 evidence.ReadinessBlocked,
		MissingProjectChecks:   2,
		IncompleteTodos:        3,
		CommandMismatchMissing: 2,
	})
	s.RecordReadinessAudit(evidence.ReadinessAudit{
		Result:    evidence.ReadinessAllowed,
		Recovered: true,
	})
	s.RecordReadinessAudit(evidence.ReadinessAudit{
		Result: evidence.ReadinessErrored,
	})

	if s.m.ReadinessChecks != 3 {
		t.Fatalf("readiness checks = %d, want 3", s.m.ReadinessChecks)
	}
	if s.m.ReadinessAllowed != 1 {
		t.Fatalf("readiness allowed = %d, want 1", s.m.ReadinessAllowed)
	}
	if s.m.ReadinessBlocks != 1 {
		t.Fatalf("readiness blocks = %d, want 1", s.m.ReadinessBlocks)
	}
	if s.m.ReadinessRecoveries != 1 {
		t.Fatalf("readiness recoveries = %d, want 1", s.m.ReadinessRecoveries)
	}
	if s.m.ReadinessErrors != 1 {
		t.Fatalf("readiness errors = %d, want 1", s.m.ReadinessErrors)
	}
	if s.m.ReadinessMissingProjectChecks != 2 {
		t.Fatalf("missing project checks = %d, want 2", s.m.ReadinessMissingProjectChecks)
	}
	if s.m.ReadinessIncompleteTodos != 3 {
		t.Fatalf("incomplete todos = %d, want 3", s.m.ReadinessIncompleteTodos)
	}
	if s.m.ReadinessCommandMismatches != 2 {
		t.Fatalf("command mismatches = %d, want 2", s.m.ReadinessCommandMismatches)
	}
}

func TestWriteMetricsIncludesReadinessFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	if err := writeMetrics(path, RunMetrics{
		PromptTokens:                  10,
		CompletionTokens:              3,
		CacheHitTokens:                7,
		CacheMissTokens:               3,
		Steps:                         2,
		ReadinessChecks:               1,
		ReadinessAllowed:              1,
		ReadinessBlocks:               0,
		ReadinessRecoveries:           1,
		ReadinessErrors:               0,
		ReadinessMissingProjectChecks: 0,
		ReadinessIncompleteTodos:      0,
		ReadinessCommandMismatches:    0,
	}); err != nil {
		t.Fatalf("writeMetrics: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{
		"readiness_checks",
		"readiness_allowed",
		"readiness_blocks",
		"readiness_recoveries",
		"readiness_errors",
		"readiness_missing_project_checks",
		"readiness_incomplete_todos",
		"readiness_command_mismatches",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("metrics JSON missing %q: %s", key, string(b))
		}
	}
}
