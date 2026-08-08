package agent

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"reasonix/internal/event"
)

// TestEmitCompactionTelemetryLogsStructuredAttrs guards the log persistence of
// compaction telemetry: a later cache-miss window (DeepSeek usage report)
// must be attributable to a specific compaction via the session log file.
func TestEmitCompactionTelemetryLogsStructuredAttrs(t *testing.T) {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(orig)

	a := &Agent{sink: event.Discard}
	a.emitCompactionTelemetry(CompactionTelemetry{
		Trigger:           "auto",
		Mode:              "summarized",
		CacheState:        "warm",
		SourceTokens:      490272,
		ProjectionTokens:  374912,
		InputTokens:       491000,
		OutputTokens:      3000,
		CacheHitTokens:    0,
		CacheMissTokens:   488000,
		CacheWriteTokens:  0,
		RequestCount:      1,
		ProviderRequestID: "req-abc",
	})

	out := buf.String()
	for _, want := range []string{"agent: compaction", "trigger=auto", "mode=summarized",
		"src=490272", "miss=488000", "reqs=1", "provider_request_id=req-abc"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q\n%s", want, out)
		}
	}
}

// TestEmitCompactionTelemetryErrorLevelsWarn guards that failed compactions
// are recorded at Warn level (they are the ones that precede cache misses).
func TestEmitCompactionTelemetryErrorLevelsWarn(t *testing.T) {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(orig)

	a := &Agent{sink: event.Discard}
	a.emitCompactionTelemetry(CompactionTelemetry{
		Trigger: "pressure",
		Mode:    "summarized",
		Error:   "context deadline exceeded",
	})

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("failed compaction must log at WARN level\n%s", out)
	}
	if !strings.Contains(out, `err="context deadline exceeded"`) && !strings.Contains(out, "err=context") {
		t.Errorf("log output missing error attr\n%s", out)
	}
}
