package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type noticeCaptureSink struct {
	notices []string
}

func (s *noticeCaptureSink) Emit(e event.Event) {
	if e.Kind == event.Notice {
		s.notices = append(s.notices, e.Detail)
	}
}

// TestCompactionNoopEmitsTelemetry pins the 2026-08-09 blind spot: a manual
// compact whose fold region is empty (planFold !ok) returned CompactionNoop
// with zero records, so /compact reported "compacted" while stats showed
// nothing. The deferred emit must land one status=noop row per silent exit.
func TestCompactionNoopEmitsTelemetry(t *testing.T) {
	sink := &noticeCaptureSink{}
	a := &Agent{
		prov:          &fakeProvider{reply: "SUMMARY"},
		contextWindow: 1_000_000,
		compactRatio:  0.8,
		sink:          sink,
	}
	// A session so small the fold region is empty: everything fits in head+tail.
	a.session = &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "hi"},
	}}
	outcome, err := a.compactToProjection(context.Background(), "manual", "", true)
	if err != nil {
		t.Fatalf("compactToProjection: %v", err)
	}
	if outcome != CompactionNoop {
		t.Fatalf("outcome=%v, want Noop for tiny session", outcome)
	}
	found := false
	for _, d := range sink.notices {
		if strings.Contains(d, "status=noop") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no status=noop telemetry emitted; notices: %v", sink.notices)
	}
	foundReason := false
	for _, d := range sink.notices {
		if strings.Contains(d, "reason=manual") {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("no reason=manual in telemetry; notices: %v", sink.notices)
	}
	// The fold admission reason rides through to the row (set by foldContext;
	// the manual path here leaves the last admission label untouched).
}

// TestCompactionTelemetrySourceTokensCalibrated pins the 2026-08-09 display
// gap: telemetry src/proj must use the usage-calibrated estimate once a turn
// has reported real tokens, so stats numbers track the provider's real counts
// instead of the raw rune-based estimate (which counts reasoning that never
// rides the wire and can run ~10x the real prompt).
func TestCompactionTelemetrySourceTokensCalibrated(t *testing.T) {
	canonical := []provider.Message{
		{Role: provider.RoleUser, Content: strings.Repeat("the quick brown fox jumps over the lazy dog ", 200)},
	}
	raw := estimateMessagesTokens(provider.ModelMessages(canonical))
	a := &Agent{}
	// Simulate one completed turn: 10k chars sent, 2000 real prompt tokens
	// (tpc=0.2, off the 0.25 fallback so calibration applies).
	a.setPromptTokenCalibration(2000, requestCalibrationShape{requestChars: 10000})
	calibrated := a.estimatedPromptTokens(canonical)
	if calibrated >= raw {
		t.Fatalf("calibrated %d must be below raw %d once usage exists", calibrated, raw)
	}
	tele := a.silentCompactionTelemetry("manual", canonical, nil)
	if tele.SourceTokens != calibrated {
		t.Fatalf("telemetry src=%d, want calibrated %d (raw would be %d)", tele.SourceTokens, calibrated, raw)
	}
	if tele.Status != CompactionStatusNoop {
		t.Fatalf("status=%q, want noop", tele.Status)
	}
	if tele.TokPerChar != a.tokPerChar() {
		t.Fatalf("telemetry tpc=%v, want tokPerChar %v", tele.TokPerChar, a.tokPerChar())
	}
	if tele.TokPerChar <= 0 {
		t.Fatalf("telemetry tpc must be positive once calibration exists, got %v", tele.TokPerChar)
	}
}
