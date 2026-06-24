package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// ─── Store 基础功能 ─────────────────────────────────────────────────────────

func TestStoreCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "usage")
	store, err := Open(dir)
	if err != nil {
		t.Fatal("Open should create nested dirs:", err)
	}
	store.Close()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("directory was not created")
	}
}

func TestStoreCloseEmpty(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	store.Close() // 不写任何数据就关闭，不应 panic

	files, _ := listJSONLFiles(dir)
	if len(files) != 0 {
		t.Errorf("empty store should not create files, got %d", len(files))
	}
}

func TestStoreWriteAndReadBack(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)

	store.Write(Record{
		TS:               time.Now(),
		Provider:         "deepseek",
		Model:            "deepseek-reasoner",
		UsageSource:      "executor",
		PromptTokens:     1000,
		CompletionTokens: 200,
		CacheHitTokens:   600,
		CacheMissTokens:  400,
		ReasoningTokens:  50,
		TotalTokens:      1200,
		Cost:             0.0123,
		Currency:         "¥",
		FinishReason:     "stop",
		LatencyMS:        2340,
		SessionID:        "/tmp/test.jsonl",
	})
	store.Close()

	records := readAllFromDir(t, dir)
	if len(records) != 1 {
		t.Fatalf("want 1 record, got %d", len(records))
	}
	r := records[0]
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"provider", r.Provider, "deepseek"},
		{"model", r.Model, "deepseek-reasoner"},
		{"usage_source", r.UsageSource, "executor"},
		{"prompt_tokens", r.PromptTokens, 1000},
		{"completion_tokens", r.CompletionTokens, 200},
		{"cache_hit_tokens", r.CacheHitTokens, 600},
		{"cache_miss_tokens", r.CacheMissTokens, 400},
		{"reasoning_tokens", r.ReasoningTokens, 50},
		{"total_tokens", r.TotalTokens, 1200},
		{"currency", r.Currency, "¥"},
		{"finish_reason", r.FinishReason, "stop"},
		{"latency_ms", r.LatencyMS, int64(2340)},
		{"session_id", r.SessionID, "/tmp/test.jsonl"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if r.Cost < 0.0122 || r.Cost > 0.0124 {
		t.Errorf("cost = %f, want ~0.0123", r.Cost)
	}
}

func TestStoreMultipleRecords(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)

	n := 10
	for i := 0; i < n; i++ {
		store.Write(Record{
			TS:           time.Now(),
			Provider:     "test",
			PromptTokens: i * 100,
			TotalTokens:  i * 100,
		})
	}
	store.Close()

	records := readAllFromDir(t, dir)
	if len(records) != n {
		t.Errorf("want %d records, got %d", n, len(records))
	}
}

func TestStoreDayRotation(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)

	// Write records for two different dates
	store.Write(Record{TS: time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local), PromptTokens: 100})
	store.Write(Record{TS: time.Date(2026, 6, 21, 10, 0, 0, 0, time.Local), PromptTokens: 200})
	store.Close()

	files, _ := listJSONLFiles(dir)
	if len(files) != 2 {
		t.Fatalf("want 2 files (day rotation), got %d: %v", len(files), files)
	}
}

func TestStoreConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)

	var wg sync.WaitGroup
	n := 50
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			store.Write(Record{
				TS:           time.Now(),
				Provider:     fmt.Sprintf("p%d", id%3),
				PromptTokens: id,
				TotalTokens:  id,
			})
		}(i)
	}
	wg.Wait()
	store.Close()

	records := readAllFromDir(t, dir)
	if len(records) != n {
		t.Errorf("want %d records from concurrent writes, got %d", n, len(records))
	}
}

// ─── EnrichSink 测试 ────────────────────────────────────────────────────────

func TestEnrichSinkInjectsAllFields(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	sink := NewSink(event.Discard, store)

	enriched := &EnrichSink{
		Inner:        sink,
		ProviderName: "deepseek",
		ModelName:    "deepseek-reasoner",
		SessionID:    func() string { return "/sessions/test.jsonl" },
	}

	enriched.Emit(event.Event{Kind: event.TurnStarted})
	time.Sleep(10 * time.Millisecond) // 让 latency 有值
	enriched.Emit(event.Event{
		Kind:        event.Usage,
		Usage:       &provider.Usage{PromptTokens: 500, TotalTokens: 500, FinishReason: "stop"},
		Pricing:     &provider.Pricing{Input: 2.0, Output: 8.0, CacheHit: 0.5, Currency: "¥"},
		UsageSource: "executor",
	})

	store.Close()
	r := readAllFromDir(t, dir)[0]

	if r.Provider != "deepseek" {
		t.Errorf("provider = %q, want deepseek", r.Provider)
	}
	if r.Model != "deepseek-reasoner" {
		t.Errorf("model = %q, want deepseek-reasoner", r.Model)
	}
	if r.SessionID != "/sessions/test.jsonl" {
		t.Errorf("session_id = %q", r.SessionID)
	}
	if r.LatencyMS <= 0 {
		t.Errorf("latency_ms = %d, want > 0", r.LatencyMS)
	}
}

func TestEnrichSinkIgnoresNonUsage(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	sink := NewSink(event.Discard, store)

	enriched := &EnrichSink{
		Inner:        sink,
		ProviderName: "test",
		ModelName:    "model",
	}

	// 只发非 Usage 事件
	enriched.Emit(event.Event{Kind: event.TurnStarted})
	enriched.Emit(event.Event{Kind: event.Reasoning, Text: "thinking..."})
	enriched.Emit(event.Event{Kind: event.Message, Text: "hello"})
	enriched.Emit(event.Event{Kind: event.TurnDone})

	store.Close()

	records := readAllFromDir(t, dir)
	if len(records) != 0 {
		t.Errorf("non-usage events should not be recorded, got %d", len(records))
	}
}

func TestEnrichSinkLatencyCalculation(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	sink := NewSink(event.Discard, store)
	enriched := &EnrichSink{Inner: sink, ProviderName: "p", ModelName: "m"}

	// TurnStarted → 50ms → Usage → TurnDone → 再来一轮
	enriched.Emit(event.Event{Kind: event.TurnStarted})
	time.Sleep(50 * time.Millisecond)
	enriched.Emit(event.Event{
		Kind:  event.Usage,
		Usage: &provider.Usage{TotalTokens: 100},
	})
	enriched.Emit(event.Event{Kind: event.TurnDone})

	store.Close()
	r := readAllFromDir(t, dir)[0]

	if r.LatencyMS < 40 || r.LatencyMS > 200 {
		t.Errorf("latency_ms = %d, expected 40-200", r.LatencyMS)
	}
}

func TestEnrichSinkNilSessionID(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	sink := NewSink(event.Discard, store)
	// SessionID 为 nil — 不应 panic
	enriched := &EnrichSink{Inner: sink, ProviderName: "p", ModelName: "m", SessionID: nil}

	enriched.Emit(event.Event{Kind: event.TurnStarted})
	enriched.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{TotalTokens: 100}})
	store.Close()

	r := readAllFromDir(t, dir)[0]
	if r.SessionID != "" {
		t.Errorf("session_id should be empty when SessionID func is nil, got %q", r.SessionID)
	}
}

func TestEnrichSinkDoesNotOverwriteExistingValues(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	sink := NewSink(event.Discard, store)
	enriched := &EnrichSink{Inner: sink, ProviderName: "parent-provider", ModelName: "parent-model"}

	// Simulate a sub-agent Usage event that already has provider/model set
	enriched.Emit(event.Event{Kind: event.TurnStarted})
	enriched.Emit(event.Event{
		Kind:         event.Usage,
		ProviderName: "sub-provider",
		ModelName:    "sub-model",
		UsageSource:  "subagent",
		Usage:        &provider.Usage{TotalTokens: 100},
	})
	store.Close()

	r := readAllFromDir(t, dir)[0]
	if r.Provider != "sub-provider" {
		t.Errorf("provider = %q, want sub-provider (should not be overwritten)", r.Provider)
	}
	if r.Model != "sub-model" {
		t.Errorf("model = %q, want sub-model (should not be overwritten)", r.Model)
	}
}

// ─── Cost Calculation ────────────────────────────────────────────────────────

func TestCostCalculation(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	sink := NewSink(event.Discard, store)
	enriched := &EnrichSink{Inner: sink, ProviderName: "p", ModelName: "m"}

	enriched.Emit(event.Event{Kind: event.TurnStarted})
	enriched.Emit(event.Event{
		Kind: event.Usage,
		Usage: &provider.Usage{
			CacheHitTokens:  600,
			CacheMissTokens: 400,
			CompletionTokens: 200,
			TotalTokens:     1200,
		},
		Pricing: &provider.Pricing{
			CacheHit: 0.5,  // 0.5 per million
			Input:    2.0,  // 2.0 per million
			Output:   8.0,  // 8.0 per million
			Currency: "¥",
		},
	})
	store.Close()

	r := readAllFromDir(t, dir)[0]
	// cost = 600*0.5/1e6 + 400*2.0/1e6 + 200*8.0/1e6
	//      = 0.0003 + 0.0008 + 0.0016 = 0.0027
	expected := (600*0.5 + 400*2.0 + 200*8.0) / 1e6
	if diff := r.Cost - expected; diff > 0.00001 || diff < -0.00001 {
		t.Errorf("cost = %f, want %f", r.Cost, expected)
	}
	if r.Currency != "¥" {
		t.Errorf("currency = %q, want ¥", r.Currency)
	}
}

func TestCostZeroWhenNoPricing(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	sink := NewSink(event.Discard, store)
	enriched := &EnrichSink{Inner: sink, ProviderName: "p", ModelName: "m"}

	enriched.Emit(event.Event{Kind: event.TurnStarted})
	enriched.Emit(event.Event{
		Kind:    event.Usage,
		Usage:   &provider.Usage{TotalTokens: 1000},
		Pricing: nil, // 无定价
	})
	store.Close()

	r := readAllFromDir(t, dir)[0]
	if r.Cost != 0 {
		t.Errorf("cost = %f, want 0 when pricing is nil", r.Cost)
	}
}

// ─── 查询 API ───────────────────────────────────────────────────────────────

func TestQueryOverviewAggregation(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now()

	store.Write(Record{TS: now, PromptTokens: 100, CompletionTokens: 20, CacheHitTokens: 50, CacheMissTokens: 50, TotalTokens: 120, Cost: 0.01, Currency: "¥"})
	store.Write(Record{TS: now, PromptTokens: 200, CompletionTokens: 30, CacheHitTokens: 100, CacheMissTokens: 100, TotalTokens: 230, Cost: 0.02, Currency: "¥"})
	store.Close()

	ov, err := QueryOverview(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Requests != 2 {
		t.Errorf("requests = %d, want 2", ov.Requests)
	}
	if ov.PromptTokens != 300 {
		t.Errorf("prompt_tokens = %d, want 300", ov.PromptTokens)
	}
	if ov.CompletionTokens != 50 {
		t.Errorf("completion_tokens = %d, want 50", ov.CompletionTokens)
	}
	if ov.CacheHitTokens != 150 {
		t.Errorf("cache_hit_tokens = %d, want 150", ov.CacheHitTokens)
	}
	if ov.CacheMissTokens != 150 {
		t.Errorf("cache_miss_tokens = %d, want 150", ov.CacheMissTokens)
	}
	if ov.TotalTokens != 350 {
		t.Errorf("total_tokens = %d, want 350", ov.TotalTokens)
	}
	if ov.Cost < 0.029 || ov.Cost > 0.031 {
		t.Errorf("cost = %f, want ~0.03", ov.Cost)
	}
}

func TestQueryOverviewEmpty(t *testing.T) {
	dir := t.TempDir()
	ov, err := QueryOverview(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Requests != 0 {
		t.Errorf("empty store: requests = %d, want 0", ov.Requests)
	}
}

func TestQueryOverviewDayFilter(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)

	store.Write(Record{TS: yesterday, PromptTokens: 100, TotalTokens: 100})
	store.Write(Record{TS: today, PromptTokens: 200, TotalTokens: 200})
	store.Close()

	// days=0 → 只看今天
	ovToday, _ := QueryOverview(dir, 0)
	if ovToday.Requests != 1 || ovToday.TotalTokens != 200 {
		t.Errorf("today: requests=%d tokens=%d, want 1/200", ovToday.Requests, ovToday.TotalTokens)
	}

	// days=1 → 昨天+今天
	ovBoth, _ := QueryOverview(dir, 1)
	if ovBoth.Requests != 2 || ovBoth.TotalTokens != 300 {
		t.Errorf("last 1 day: requests=%d tokens=%d, want 2/300", ovBoth.Requests, ovBoth.TotalTokens)
	}
}

func TestQueryModelsGrouping(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now()

	store.Write(Record{TS: now, Provider: "deepseek", Model: "reasoner", PromptTokens: 100, TotalTokens: 100, Cost: 0.01, Currency: "¥"})
	store.Write(Record{TS: now, Provider: "deepseek", Model: "reasoner", PromptTokens: 200, TotalTokens: 200, Cost: 0.02, Currency: "¥"})
	store.Write(Record{TS: now, Provider: "openai", Model: "gpt-4o", PromptTokens: 50, TotalTokens: 50, Cost: 0.05, Currency: "$"})
	store.Close()

	models, _ := QueryModels(dir, 0)
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	// 按 cost 降序: openai/gpt-4o (0.05) > deepseek/reasoner (0.03)
	if models[0].Provider != "openai" {
		t.Errorf("first model = %s/%s, want openai/gpt-4o", models[0].Provider, models[0].Model)
	}
	if models[0].Requests != 1 {
		t.Errorf("openai requests = %d, want 1", models[0].Requests)
	}
	if models[1].Requests != 2 {
		t.Errorf("deepseek requests = %d, want 2", models[1].Requests)
	}
}

func TestQueryModelsAvgLatency(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now()

	store.Write(Record{TS: now, Provider: "p", Model: "m", LatencyMS: 100})
	store.Write(Record{TS: now, Provider: "p", Model: "m", LatencyMS: 300})
	store.Close()

	models, _ := QueryModels(dir, 0)
	if len(models) != 1 {
		t.Fatal("want 1 model")
	}
	if models[0].AvgLatencyMS < 199 || models[0].AvgLatencyMS > 201 {
		t.Errorf("avg_latency = %f, want ~200", models[0].AvgLatencyMS)
	}
}

func TestQueryTrend(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)

	// Today's records
	store.Write(Record{TS: time.Now(), PromptTokens: 100, TotalTokens: 100, Cost: 0.01, Currency: "¥"})
	store.Close()

	trend, _ := QueryTrend(dir, 0)
	if len(trend) != 1 {
		t.Fatalf("want 1 trend point, got %d", len(trend))
	}
	if trend[0].Requests != 1 {
		t.Errorf("trend requests = %d, want 1", trend[0].Requests)
	}
}

func TestQueryTrendSortsByDate(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now()

	store.Write(Record{TS: now.Add(-48 * time.Hour), PromptTokens: 100})
	store.Write(Record{TS: now.Add(-72 * time.Hour), PromptTokens: 200})
	store.Write(Record{TS: now.Add(-24 * time.Hour), PromptTokens: 300})
	store.Close()

	trend, _ := QueryTrend(dir, 4)
	if len(trend) < 2 {
		t.Fatalf("want >= 2 points, got %d", len(trend))
	}
	for i := 1; i < len(trend); i++ {
		if trend[i-1].Date >= trend[i].Date {
			t.Errorf("trend not sorted ascending: %v", trend)
			break
		}
	}
}

func TestQueryLogsTail(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now()

	for i := 0; i < 10; i++ {
		store.Write(Record{TS: now.Add(time.Duration(i) * time.Second), Provider: "p", Model: "m", PromptTokens: i * 10})
	}
	store.Close()

	logs, _ := QueryLogs(dir, 3, "", "", 0)
	if len(logs) != 3 {
		t.Fatalf("want 3 logs, got %d", len(logs))
	}
	// Newest first
	if logs[0].PromptTokens != 90 {
		t.Errorf("first log prompt_tokens = %d, want 90 (newest)", logs[0].PromptTokens)
	}
}

func TestQueryLogsFilterByProvider(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now()

	store.Write(Record{TS: now, Provider: "deepseek", Model: "r", PromptTokens: 100})
	store.Write(Record{TS: now, Provider: "openai", Model: "g", PromptTokens: 200})
	store.Write(Record{TS: now, Provider: "deepseek", Model: "r", PromptTokens: 300})
	store.Close()

	logs, _ := QueryLogs(dir, 100, "deepseek", "", 0)
	if len(logs) != 2 {
		t.Errorf("filter by provider: want 2, got %d", len(logs))
	}
}

func TestQueryLogsFilterByModel(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now()

	store.Write(Record{TS: now, Provider: "p", Model: "a", PromptTokens: 100})
	store.Write(Record{TS: now, Provider: "p", Model: "b", PromptTokens: 200})
	store.Close()

	logs, _ := QueryLogs(dir, 100, "", "a", 0)
	if len(logs) != 1 {
		t.Errorf("filter by model: want 1, got %d", len(logs))
	}
}

func TestQueryProvidersAndModelNames(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now()

	store.Write(Record{TS: now, Provider: "deepseek", Model: "reasoner"})
	store.Write(Record{TS: now, Provider: "openai", Model: "gpt-4o"})
	store.Write(Record{TS: now, Provider: "deepseek", Model: "chat"})
	store.Close()

	providers, _ := QueryProviders(dir)
	if len(providers) != 2 {
		t.Errorf("providers = %d, want 2", len(providers))
	}

	models, _ := QueryModelNames(dir)
	if len(models) != 3 {
		t.Errorf("model_names = %d, want 3", len(models))
	}
}

func TestCacheHitRate(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	store.Write(Record{
		TS:              time.Now(),
		CacheHitTokens:  750,
		CacheMissTokens: 250,
		TotalTokens:     1000,
	})
	store.Close()

	ov, _ := QueryOverview(dir, 0)
	rate := ov.CacheHitRate()
	if rate < 0.749 || rate > 0.751 {
		t.Errorf("cache hit rate = %f, want ~0.75", rate)
	}
}

func TestTPMAndRPM(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	base := time.Now()

	// 5 records spanning 10 minutes
	for i := 0; i < 5; i++ {
		store.Write(Record{
			TS:           base.Add(time.Duration(i*2) * time.Minute),
			PromptTokens: 1000,
			TotalTokens:  1000,
		})
	}
	store.Close()

	ov, _ := QueryOverview(dir, 0)
	// span = 8 minutes (0,2,4,6,8 min)
	if ov.TPM < 620 || ov.TPM > 630 {
		t.Errorf("TPM = %.1f, want ~625 (5000/8)", ov.TPM)
	}
	if ov.RPM < 0.6 || ov.RPM > 0.65 {
		t.Errorf("RPM = %.2f, want ~0.63 (5/8)", ov.RPM)
	}
	if ov.FirstTS == "" || ov.LastTS == "" {
		t.Error("FirstTS/LastTS should be set")
	}
}

func TestTPMRPMSingleRequest(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	store.Write(Record{TS: time.Now(), PromptTokens: 500, TotalTokens: 500})
	store.Close()

	ov, _ := QueryOverview(dir, 0)
	// 单条记录：span=0，按 1 分钟算
	if ov.TPM != 500 {
		t.Errorf("TPM = %.0f, want 500 (single request, min 1 min)", ov.TPM)
	}
	if ov.RPM != 1 {
		t.Errorf("RPM = %.1f, want 1", ov.RPM)
	}
}

// ─── JSON 行格式 ────────────────────────────────────────────────────────────

func TestJSONLFormatValidity(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)

	store.Write(Record{TS: time.Now(), Provider: "test", Model: "m", PromptTokens: 100})
	store.Close()

	data, _ := os.ReadFile(filepath.Join(dir, time.Now().Format("2006-01-02")+".jsonl"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}

	// 验证是合法 JSON
	var r Record
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if r.Provider != "test" {
		t.Errorf("provider = %q", r.Provider)
	}
}

func TestJSONLOmitsEmptyFields(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	store.Write(Record{TS: time.Now(), PromptTokens: 100}) // provider/model 为空
	store.Close()

	data, _ := os.ReadFile(filepath.Join(dir, time.Now().Format("2006-01-02")+".jsonl"))
	line := string(data)
	if strings.Contains(line, `"provider"`) {
		t.Error("empty provider should be omitted (omitempty)")
	}
	if strings.Contains(line, `"model"`) {
		t.Error("empty model should be omitted (omitempty)")
	}
}

// ─── report 格式化 ──────────────────────────────────────────────────────────

func TestCommaFmt(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{123, "123"},
		{1234, "1,234"},
		{1234567, "1,234,567"},
		{100000, "100,000"},
	}
	for _, tt := range tests {
		got := commaFmt(tt.in)
		if got != tt.want {
			t.Errorf("commaFmt(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	bar := progressBar(0.5, 10)
	filled := strings.Count(bar, "█")
	empty := strings.Count(bar, "░")
	if filled != 5 {
		t.Errorf("filled = %d, want 5", filled)
	}
	if empty != 5 {
		t.Errorf("empty = %d, want 5", empty)
	}

	// Edge case
	if progressBar(0, 10) != strings.Repeat("░", 10) {
		t.Error("ratio=0 should be all empty")
	}
	if progressBar(1, 10) != strings.Repeat("█", 10) {
		t.Error("ratio=1 should be all filled")
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func readAllFromDir(t *testing.T, dir string) []Record {
	t.Helper()
	var all []Record
	files, _ := listJSONLFiles(dir)
	for _, f := range files {
		records, err := readFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal("readFile:", err)
		}
		all = append(all, records...)
	}
	return all
}
