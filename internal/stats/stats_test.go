package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/filelock"
	"reasonix/internal/provider"
)

func flushRecorder(t *testing.T, recorder *Recorder) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := recorder.Flush(ctx); err != nil {
		t.Fatalf("flush recorder: %v", err)
	}
}

func TestRecorderWritesDailyFile(t *testing.T) {
	dir := t.TempDir()
	inner := &spySink{}
	r := NewRecorder(inner, dir, "desktop")

	r.Emit(usageEvent("deepseek/deepseek-v4-flash", 100, 50, 10, 20, 30, 150))
	r.Emit(usageEvent("deepseek/deepseek-v4-pro", 200, 100, 0, 0, 0, 300))
	r.Emit(turnEvent())
	flushRecorder(t, r)

	// The daily file must exist with three lines (2 usage + 1 turn marker).
	files := dailyJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want 1 daily file, got %d", len(files))
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Fatalf("want 3 lines, got %d", lines)
	}
	// Forwarding must be untouched.
	if len(inner.events) != 3 {
		t.Fatalf("want 3 forwarded events, got %d", len(inner.events))
	}
}

func TestRecorderPersistsRateBandAndRatedAt(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(&spySink{}, dir, "desktop")
	e := usageEvent("deepseek/deepseek-v4-pro", 100, 50, 0, 100, 0, 150)
	e.CostQuote = &billing.CostQuote{
		Original:  billing.Money{Amount: "0.00135", Currency: "CNY"},
		Estimated: true, CostComplete: true, DisplayComplete: true, Complete: true,
		RateBand: billing.RateBandPeak, RatedAt: "2026-08-17T01:00:00Z",
	}
	r.Emit(e)
	flushRecorder(t, r)

	files := dailyJSONLFiles(t, dir)
	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &got); err != nil {
		t.Fatal(err)
	}
	if got["rate_band"] != billing.RateBandPeak || got["rated_at"] != "2026-08-17T01:00:00Z" {
		t.Fatalf("scheduled stats fields missing: %s", data)
	}
}

func TestRecorderCountsMergedProviderRequests(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(&spySink{}, dir, "desktop")
	e := usageEvent("deepseek/deepseek-v4-pro", 100, 50, 10, 0, 100, 150)
	e.Usage.RequestCount = 2
	r.Emit(e)
	flushRecorder(t, r)

	day := dayStart(time.Now())
	got, err := r.writer.Query(SourceFilter{From: day, To: day})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Requests != 2 || len(got.Daily) != 1 || got.Daily[0].Requests != 2 {
		t.Fatalf("merged requests = total %d daily %+v, want 2", got.Requests, got.Daily)
	}
}

func TestRecorderCapturesGuardianUsageAndPreservesProtocolAudit(t *testing.T) {
	dir := t.TempDir()
	inner := &auditSpySink{}
	r := NewRecorder(inner, dir, "desktop")
	r.Emit(event.Event{
		Kind:     event.GuardianAssessment,
		ModelRef: "deepseek/deepseek-v4-flash",
		Guardian: event.GuardianResult{Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
	})
	event.RecordProtocolRecovery(r, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryRecovered})
	flushRecorder(t, r)

	day := dayStart(time.Now())
	got, err := r.writer.Query(SourceFilter{From: day, To: day})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Tokens != 15 || got.TopModel != "deepseek/deepseek-v4-flash" {
		t.Fatalf("guardian usage = %+v", got)
	}
	if len(inner.protocol) != 1 || inner.protocol[0].Kind != event.ProtocolRecoveryMissingReasoningRetryRecovered {
		t.Fatalf("protocol audit was not forwarded: %+v", inner.protocol)
	}
}

func TestRecorderSkipsZeroUsage(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(&spySink{}, dir, "desktop")
	r.Emit(usageEvent("m", 0, 0, 0, 0, 0, 0)) // TotalTokens <= 0 -> skipped
	r.Emit(turnEvent())
	flushRecorder(t, r)
	files := dailyJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want 1 file (turn only), got %d", len(files))
	}
}

func TestRecorderPersistsRequestOnlyFailureWithoutForwardingReceipt(t *testing.T) {
	dir := t.TempDir()
	inner := &spySink{}
	r := NewRecorder(inner, dir, "desktop")
	r.Emit(event.Event{
		Kind:     event.Usage,
		ModelRef: "deepseek/deepseek-v4-pro",
		Usage:    &provider.Usage{RequestCount: 3},
	})
	flushRecorder(t, r)

	day := dayStart(time.Now())
	got, err := r.writer.Query(SourceFilter{From: day, To: day})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Requests != 3 || got.Tokens != 0 || got.ActiveDays != 1 {
		t.Fatalf("request-only totals = %+v, want requests=3 tokens=0 activeDays=1", got)
	}
	if len(got.Models) != 0 || len(got.Providers) != 0 {
		t.Fatalf("request-only failure created token distribution rows: models=%+v providers=%+v", got.Models, got.Providers)
	}
	if len(inner.events) != 0 {
		t.Fatalf("request-only usage forwarded %d zero-token receipts", len(inner.events))
	}
}

func TestRecorderNeverWaitsForStatsFileLock(t *testing.T) {
	dir := t.TempDir()
	release, err := filelock.Acquire(context.Background(), filepath.Join(dir, ".append.lock"))
	if err != nil {
		t.Fatalf("hold stats lock: %v", err)
	}
	locked := true
	defer func() {
		if locked {
			release()
		}
	}()

	inner := &spySink{}
	recorder := NewRecorder(inner, dir, "desktop")
	emitted := make(chan struct{})
	go func() {
		recorder.Emit(usageEvent("deepseek/model", 10, 4, 0, 0, 10, 14))
		close(emitted)
	}()

	select {
	case <-emitted:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("stats file lock blocked event forwarding")
	}
	if len(inner.events) != 1 {
		t.Fatalf("forwarded events = %d, want 1", len(inner.events))
	}

	release()
	locked = false
	flushRecorder(t, recorder)
	result, err := recorder.writer.Query(SourceFilter{From: dayStart(time.Now()), To: dayStart(time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tokens != 14 {
		t.Fatalf("tokens after lock release = %d, want 14", result.Tokens)
	}
}

func TestRecorderDisabledOnEmptyDir(t *testing.T) {
	r := NewRecorder(&spySink{}, "", "desktop")
	r.Emit(usageEvent("m", 1, 1, 0, 0, 0, 2))
	r.Emit(turnEvent())
	// No panic, nothing written — query on empty dir returns zeros.
	got, err := r.writer.Query(SourceFilter{From: time.Now().Add(-24 * time.Hour), To: time.Now()})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Tokens != 0 || got.Turns != 0 {
		t.Fatalf("want zero stats, got %+v", got)
	}
}

func TestQueryAggregates(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	now := time.Now()
	day := dayStart(now)

	// Two usage rows + one turn on "today", one usage row yesterday.
	w.Append(record{Timestamp: day.Add(1 * time.Hour), ModelRef: "deepseek/deepseek-v4-flash", Source: "desktop", Total: 100, Prompt: 60, Completion: 40, CacheHit: 10, CacheMiss: 50})
	w.Append(record{Timestamp: day.Add(2 * time.Hour), ModelRef: "deepseek/deepseek-v4-pro", Source: "desktop", Total: 200, Prompt: 100, Completion: 100})
	w.Append(record{Timestamp: day.Add(3 * time.Hour), Source: "desktop", Turn: true})
	w.Append(record{Timestamp: day.AddDate(0, 0, -1), ModelRef: "zhipu/glm-5.2", Source: "cli", Total: 300})

	got, err := w.Query(SourceFilter{From: day.AddDate(0, 0, -1), To: day})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Tokens != 600 {
		t.Fatalf("tokens: want 600, got %d", got.Tokens)
	}
	if got.Requests != 3 {
		t.Fatalf("requests: want 3, got %d", got.Requests)
	}
	if got.Turns != 1 {
		t.Fatalf("turns: want 1, got %d", got.Turns)
	}
	if got.CacheHit != 10 || got.CacheMiss != 50 {
		t.Fatalf("cache: want hit=10 miss=50, got hit=%d miss=%d", got.CacheHit, got.CacheMiss)
	}
	if got.ActiveDays != 2 {
		t.Fatalf("active days: want 2, got %d", got.ActiveDays)
	}
	if got.TopModel != "zhipu/glm-5.2" {
		t.Fatalf("top model: want zhipu/glm-5.2 (300 tokens), got %q", got.TopModel)
	}
	if len(got.Daily) != 2 {
		t.Fatalf("daily series: want 2 entries, got %d", len(got.Daily))
	}
	// daysInRange walks from -> to, so Daily[0] is yesterday (glm, no cache)
	// and Daily[1] is today (flash hit=10 miss=50 + pro no cache).
	if got.Daily[0].CacheHit != 0 || got.Daily[0].CacheMiss != 0 {
		t.Fatalf("yesterday cache: want 0/0, got hit=%d miss=%d", got.Daily[0].CacheHit, got.Daily[0].CacheMiss)
	}
	if got.Daily[1].CacheHit != 10 || got.Daily[1].CacheMiss != 50 {
		t.Fatalf("today cache: want hit=10 miss=50, got hit=%d miss=%d", got.Daily[1].CacheHit, got.Daily[1].CacheMiss)
	}
	if len(got.Models) != 3 {
		t.Fatalf("models: want 3, got %d", len(got.Models))
	}
	// Providers: deepseek (100+200=300), zhipu (300) — tied, so find by name.
	found := map[string]int64{}
	for _, p := range got.Providers {
		found[p.Provider] = p.Tokens
	}
	if found["deepseek"] != 300 || found["zhipu"] != 300 || len(found) != 2 {
		t.Fatalf("providers: want deepseek=300 zhipu=300, got %+v", got.Providers)
	}
	// Percent on models sums to ~100 across 3 models: 200/600=33.3, 100/600=16.7, 300/600=50
	if got.Models[0].Percent <= 0 || got.Models[0].Percent > 100 {
		t.Fatalf("model percent out of range: %+v", got.Models[0])
	}
}

func TestQuerySourceFilter(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	now := time.Now()
	day := dayStart(now)

	w.Append(record{Timestamp: day, ModelRef: "m1", Source: "desktop", Total: 100})
	w.Append(record{Timestamp: day, ModelRef: "m2", Source: "cli", Total: 50})

	got, err := w.Query(SourceFilter{From: day, To: day, Source: "cli"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Tokens != 50 {
		t.Fatalf("cli-filtered tokens: want 50, got %d", got.Tokens)
	}
	if len(got.Models) != 1 || got.Models[0].Model != "m2" {
		t.Fatalf("cli-filtered models: want [m2], got %+v", got.Models)
	}
}

func TestQueryEmptyRange(t *testing.T) {
	w := NewWriter(t.TempDir())
	now := time.Now()
	got, err := w.Query(SourceFilter{From: now, To: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Tokens != 0 || got.ActiveDays != 0 || len(got.Daily) != 0 {
		t.Fatalf("want empty stats, got %+v", got)
	}
}

func TestQueryDisabledWriterReturnsArrayContract(t *testing.T) {
	now := time.Now()
	got, err := NewWriter("").Query(SourceFilter{From: now, To: now})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Daily == nil || got.Models == nil || got.Providers == nil {
		t.Fatalf("array contract contains nil slices: %+v", got)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire struct {
		Daily     json.RawMessage `json:"daily"`
		Models    json.RawMessage `json:"models"`
		Providers json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(wire.Daily) != "[]" || string(wire.Models) != "[]" || string(wire.Providers) != "[]" {
		t.Fatalf("empty arrays serialized incorrectly: %s", b)
	}
}

func TestQueryTopProviderAggregatesAcrossModels(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	day := dayStart(time.Now())
	for _, rec := range []record{
		{Timestamp: day, ModelRef: "provider-a/model-1", Total: 60},
		{Timestamp: day, ModelRef: "provider-a/model-2", Total: 60},
		{Timestamp: day, ModelRef: "provider-b/model-1", Total: 100},
	} {
		if err := w.Append(rec); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := w.Query(SourceFilter{From: day, To: day})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.TopModel != "provider-b/model-1" {
		t.Fatalf("top model = %q, want provider-b/model-1", got.TopModel)
	}
	if got.TopProvider != "provider-a" {
		t.Fatalf("top provider = %q, want provider-a", got.TopProvider)
	}
}

func TestDecodeRecordsSkipsMalformed(t *testing.T) {
	// A torn or hand-edited line must not fail the whole day's read: it is
	// skipped and the surrounding valid records still come through.
	good := `{"ts":"2026-08-02T10:00:00+08:00","total":100}` + "\n"
	bad := `{"ts":"2026-08-02T10:00:00+08:00","total":` + "\n" // truncated JSON
	recs, err := decodeRecords(strings.NewReader(good + bad + bad + good))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 valid records, got %d", len(recs))
	}
	for _, r := range recs {
		if r.Total != 100 {
			t.Fatalf("record total: want 100, got %d", r.Total)
		}
	}
}

func TestAppendRepairsTornTrailingRecord(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	now := time.Now()
	path := filepath.Join(dir, now.Format(dayLayout)+".jsonl")
	if err := os.WriteFile(path, []byte(`{"ts":"2026-08-02T10:00:00+08:00","total":`), 0o600); err != nil {
		t.Fatalf("seed torn record: %v", err)
	}
	if err := w.Append(record{Timestamp: now, ModelRef: "deepseek/deepseek-v4-flash", Total: 42}); err != nil {
		t.Fatalf("append after torn record: %v", err)
	}
	recs, err := readDaily(dir, now.Format(dayLayout))
	if err != nil {
		t.Fatalf("read daily: %v", err)
	}
	if len(recs) != 1 || recs[0].Total != 42 || recs[0].ModelRef != "deepseek/deepseek-v4-flash" {
		t.Fatalf("recovered records = %+v", recs)
	}
}

func TestConcurrentWritersAppendWholeRecords(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	const writers = 8
	const perWriter = 40
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(model int) {
			defer wg.Done()
			w := NewWriter(dir)
			for range perWriter {
				if err := w.Append(record{Timestamp: now, ModelRef: fmt.Sprintf("provider/model-%d", model), Total: 1}); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	recs, err := readDaily(dir, now.Format(dayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != writers*perWriter {
		t.Fatalf("records = %d, want %d", len(recs), writers*perWriter)
	}
}

// TestDailyTokensWireKeys guards the JSON contract the desktop panel reads:
// the hand-written frontend types use camelCase (byModel/byProvider), so a
// snake_case tag here silently yields undefined fields in DailyTrend and
// crashed the panel with "Cannot convert undefined or null to object".
func TestDailyTokensWireKeys(t *testing.T) {
	d := DailyTokens{Day: "2026-08-02", Total: 150, ByModel: map[string]int64{"deepseek/x": 150}, Requests: 2, Turns: 1, CacheHit: 10, CacheMiss: 50}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var keys map[string]any
	if err := json.Unmarshal(b, &keys); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, want := range []string{"day", "total", "byModel", "byProvider", "requests", "turns", "cacheHit", "cacheMiss"} {
		if _, ok := keys[want]; !ok {
			t.Fatalf("wire key %q missing from %s", want, b)
		}
	}
	for _, bad := range []string{"by_model", "by_provider", "cache_hit", "cache_miss"} {
		if _, ok := keys[bad]; ok {
			t.Fatalf("legacy snake_case key %q still present in %s", bad, b)
		}
	}
}

func TestProviderSplit(t *testing.T) {
	if got := providerOf("deepseek/deepseek-v4-flash"); got != "deepseek" {
		t.Fatalf("provider: want deepseek, got %q", got)
	}
	if got := providerOf("bare-model"); got != "default" {
		t.Fatalf("bare model: want default, got %q", got)
	}
}

// test helpers

func dailyJSONLFiles(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	files := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".jsonl") {
			files = append(files, entry)
		}
	}
	return files
}

type spySink struct{ events []event.Event }

func (s *spySink) Emit(e event.Event) { s.events = append(s.events, e) }

type auditSpySink struct {
	events     []event.Event
	protocol   []event.ProtocolRecoveryAudit
	turns      int
	workspace  []event.WorkspaceMutation
	runBudgets []event.RunBudgetSample
}

func (s *auditSpySink) Emit(e event.Event) { s.events = append(s.events, e) }
func (s *auditSpySink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	s.protocol = append(s.protocol, a)
}
func (s *auditSpySink) RecordTurnCompletion() { s.turns++ }
func (s *auditSpySink) RecordWorkspaceMutation(m event.WorkspaceMutation) {
	s.workspace = append(s.workspace, m)
}
func (s *auditSpySink) RecordRunBudget(sample event.RunBudgetSample) {
	s.runBudgets = append(s.runBudgets, sample)
}

func TestRecorderForwardsHostCapabilities(t *testing.T) {
	inner := &auditSpySink{}
	r := NewRecorder(inner, t.TempDir(), "test")

	event.RecordTurnCompletion(r)
	event.RecordWorkspaceMutation(r, event.WorkspaceMutation{ToolName: "write_file"})
	event.RecordRunBudget(r, event.RunBudgetSample{Currency: "USD"})
	flushRecorder(t, r)

	if inner.turns != 1 || len(inner.workspace) != 1 || len(inner.runBudgets) != 1 {
		t.Fatalf("host capabilities not forwarded: turns=%d workspace=%d run_budget=%d", inner.turns, len(inner.workspace), len(inner.runBudgets))
	}
}

func usageEvent(model string, prompt, completion, reasoning, hit, miss, total int) event.Event {
	return event.Event{
		Kind:     event.Usage,
		ModelRef: model,
		Usage: &provider.Usage{
			PromptTokens:     prompt,
			CompletionTokens: completion,
			ReasoningTokens:  reasoning,
			CacheHitTokens:   hit,
			CacheMissTokens:  miss,
			TotalTokens:      total,
		},
	}
}

func turnEvent() event.Event { return event.Event{Kind: event.TurnDone} }
