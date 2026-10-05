package boot

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/frontend/doctor"
	"reasonix/internal/state/usagereport"
)

// usageReportingProvider answers every round with a cache split, so a test can
// assert what the session recorded rather than what a component computed.
type usageReportingProvider struct {
	mu     sync.Mutex
	rounds int
}

func (p *usageReportingProvider) Name() string { return "usage-effect" }

func (p *usageReportingProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	round := p.rounds
	p.rounds++
	p.mu.Unlock()

	// The opening request pays the prefix; a warmed one starts near-hit. The
	// point of the record is that these do not average into one number.
	hit, miss := 400, 8600
	if round > 0 {
		hit, miss = 9000, 500
	}
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
		PromptTokens: hit + miss, CompletionTokens: 10, TotalTokens: hit + miss + 10,
		CacheHitTokens: hit, CacheMissTokens: miss, RequestCount: 1,
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// TestEffectSessionRecordsItsColdStart is the boundary for the accounting a
// report is built from: a real session must leave a record beside itself, and
// `reasonix doctor` must read the opening request out of it. Nothing produced
// this file after the desktop that used to write it was retired, so every
// report said usage was unavailable.
func TestEffectSessionRecordsItsColdStart(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	provider.Register("boot-usage-effect", func(provider.Config) (provider.Provider, error) {
		return &usageReportingProvider{}, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-usage-effect"
model = "x"
`)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	sessionPath := sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label())
	ctrl.SetSessionPath(sessionPath)

	for _, turn := range []string{"first", "second"} {
		if err := ctrl.Run(context.Background(), turn); err != nil {
			t.Fatalf("Run(%s): %v", turn, err)
		}
	}
	if err := ctrl.SnapshotForShutdown(); err != nil {
		t.Fatalf("SnapshotForShutdown: %v", err)
	}

	report, ok := usagereport.Load(sessionPath)
	if !ok {
		t.Fatalf("no usage record beside the session at %s", usagereport.Path(sessionPath))
	}
	cold := report.Usage.ColdStart
	if cold == nil {
		t.Fatalf("record has no cold start: %+v", report.Usage)
	}
	if cold.CacheHitTokens != 400 || cold.CacheMissTokens != 8600 {
		t.Fatalf("cold start = %+v; want the opening request, not a later or averaged one", *cold)
	}
	if report.Usage.CacheHitTokens != 9400 {
		t.Fatalf("session total hit = %d, want both requests", report.Usage.CacheHitTokens)
	}

	// The reader is the reason the record exists.
	quality, err := doctor.CollectQuality(doctor.QualityOptions{SessionRef: sessionPath})
	if err != nil {
		t.Fatalf("CollectQuality: %v", err)
	}
	if !quality.Usage.Available {
		t.Fatalf("doctor reports usage unavailable for a session that recorded it; warnings: %v", quality.Warnings)
	}
	if quality.Usage.ColdStartHitPercent == nil || *quality.Usage.ColdStartHitPercent != 4 {
		t.Fatalf("doctor cold-start hit = %v, want 4%% (400 of 9000)", quality.Usage.ColdStartHitPercent)
	}
	if quality.Usage.CacheHitPercent == nil || *quality.Usage.CacheHitPercent <= 4 {
		t.Fatalf("session percent %v does not exceed the cold start; the two must not collapse into one number",
			quality.Usage.CacheHitPercent)
	}
	if quality.Signals.ExecutorRequests != 2 {
		t.Fatalf("executor requests = %d, want 2", quality.Signals.ExecutorRequests)
	}
}
