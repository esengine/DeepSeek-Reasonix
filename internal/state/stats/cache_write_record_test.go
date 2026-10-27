package stats

// Rows must explain their own money: cache writes are priced, and an imputed
// cache split must not read like a measured one. These tests pin both.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/usagecatalog"
)

const anthropicModelRef = "relay/claude-sonnet-4"

// anthropicRates is a native Anthropic rate card: $3/M input, $0.30/M cache
// read, $15/M output. A 5-minute cache write is billed at 1.25x input.
var anthropicRates = pricing.RateCard{CacheHit: 0.30, Input: 3, Output: 15, Currency: "USD"}

func anthropicWriteUsage() *provider.Usage {
	return &provider.Usage{
		PromptTokens: 7000, CompletionTokens: 10, TotalTokens: 7010,
		CacheHitTokens: 4000, CacheMissTokens: 3000,
		CacheWriteTokens: 2000, CacheWriteBilledTokens: 2500,
		RequestCount: 1,
	}
}

func usageTokensOf(u *provider.Usage) pricing.UsageTokens {
	return pricing.UsageTokens{
		PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens,
		CacheHitTokens: u.CacheHitTokens, CacheMissTokens: u.CacheMissTokens,
		CacheWriteTokens: u.CacheWriteTokens, CacheWriteBilledTokens: u.CacheWriteBilledTokens,
		Estimated: u.Estimated,
	}
}

func quoteForUsage(t *testing.T, usage *provider.Usage) *pricing.CostQuote {
	t.Helper()
	quote := pricing.BuildQuote(pricing.QuoteInput{
		Usage:       usageTokensOf(usage),
		Rates:       anthropicRates,
		OccurredAt:  time.Date(2026, 8, 10, 2, 0, 0, 0, time.UTC),
		BillingMode: pricing.BillingModePAYG,
		ModelRef:    anthropicModelRef,
		UsageSource: "executor",
	})
	if quote.CostComplete == usage.Estimated {
		t.Fatalf("fixture quote completeness disagrees with usage provenance: %+v", quote)
	}
	return &quote
}

// recordUsageLine writes one Usage event through the production recorder and
// returns the single JSONL row it persisted.
func recordUsageLine(t *testing.T, usage *provider.Usage, quote *pricing.CostQuote) string {
	t.Helper()
	dir := testenv.TempDir(t)
	recorder := NewRecorder(event.Discard, dir, "cli")
	recorder.Emit(event.Event{
		Kind: event.Usage, ModelRef: anthropicModelRef, Source: "cli",
		UsageSource: "executor", Usage: usage, CostQuote: quote,
	})
	flushRecorder(t, recorder)
	files := dailyJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("daily files = %d, want 1", len(files))
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	rows := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1:\n%s", len(rows), data)
	}
	return rows[0]
}

func rowMap(t *testing.T, line string) map[string]any {
	t.Helper()
	var row map[string]any
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		t.Fatalf("decode row: %v\nrow: %s", err, line)
	}
	return row
}

func rowNumber(t *testing.T, row map[string]any, key string) float64 {
	t.Helper()
	raw, ok := row[key]
	if !ok {
		return 0
	}
	number, ok := raw.(float64)
	if !ok {
		t.Fatalf("%q in row is %T, want a number", key, raw)
	}
	return number
}

func rowAmount(t *testing.T, row map[string]any) pricing.Amount {
	t.Helper()
	raw, ok := row["cost_amount"].(string)
	if !ok {
		t.Fatalf("cost_amount in row is %T, want a string", row["cost_amount"])
	}
	amount, err := pricing.ParseAmount(raw)
	if err != nil {
		t.Fatalf("parse cost_amount %q: %v", raw, err)
	}
	return amount
}

// rowPriceInput rebuilds the tokens a reader needs to reprice a row from the
// row alone, and says whether its cache split was measured. An absent
// "estimated" marker means unknown: the zero write counts are absence of
// evidence, not evidence of zero writes (likewise when estimated is true).
func rowPriceInput(t *testing.T, line string) (pricing.UsageTokens, bool) {
	t.Helper()
	row := rowMap(t, line)
	tokens := pricing.UsageTokens{
		PromptTokens:           int(rowNumber(t, row, "prompt")),
		CompletionTokens:       int(rowNumber(t, row, "completion")),
		CacheHitTokens:         int(rowNumber(t, row, "cache_hit")),
		CacheMissTokens:        int(rowNumber(t, row, "cache_miss")),
		CacheWriteTokens:       int(rowNumber(t, row, "cache_write")),
		CacheWriteBilledTokens: rowNumber(t, row, "cache_write_billed"),
	}
	estimated, present := row["estimated"].(bool)
	return tokens, present && !estimated
}

// TestRecordedCacheWritesRepriceExactly is the audit: the stored amount must be
// derivable from the row's own fields at the rate card the row was priced by.
func TestRecordedCacheWritesRepriceExactly(t *testing.T) {
	usage := anthropicWriteUsage()
	quote := quoteForUsage(t, usage)
	if quote.Original.Amount != "0.01185" {
		t.Fatalf("fixture quote = %s %s, want 0.01185 USD", quote.Original.Amount, quote.Original.Currency)
	}
	line := recordUsageLine(t, usage, quote)
	t.Logf("row: %s", line)

	row := rowMap(t, line)
	stored := rowAmount(t, row)
	tokens, measured := rowPriceInput(t, line)
	repriced := pricing.OriginalCostAmount(anthropicRates, tokens)
	if !measured {
		t.Fatalf("row cannot say what it was priced from: a reader reprices it as %s while the stored amount is %s\nrow: %s",
			repriced, stored, line)
	}
	if repriced != stored {
		t.Fatalf("reprice from row = %s, stored amount = %s; the row does not explain its own money\nrow: %s",
			repriced, stored, line)
	}
	if repriced.String() != "0.01185" {
		t.Fatalf("repriced amount = %s, want 0.01185", repriced)
	}
	// The production reader keeps the write counts too.
	decoded, err := decodeRecords(strings.NewReader(line + "\n"))
	if err != nil || len(decoded) != 1 {
		t.Fatalf("decode row: %v / %d rows", err, len(decoded))
	}
	if decoded[0].CacheWrite != 2000 || decoded[0].CacheWriteBilled != 2500 {
		t.Fatalf("decoded cache write = %d tokens / %v billed, want 2000 / 2500",
			decoded[0].CacheWrite, decoded[0].CacheWriteBilled)
	}
}

// TestCacheSplitProvenanceIsRecorded distinguishes an imputed cache split from
// a reported one. The two rows carry the same tokens and the same money, so
// only the provenance marker can tell a reader what the split is worth.
func TestCacheSplitProvenanceIsRecorded(t *testing.T) {
	reported := &provider.Usage{
		PromptTokens: 5000, CompletionTokens: 20, TotalTokens: 5020,
		CacheMissTokens: 5000, RequestCount: 1,
	}
	imputed := &provider.Usage{
		PromptTokens: 5000, CompletionTokens: 20, TotalTokens: 5020,
		CacheMissTokens: 5000, Estimated: true, RequestCount: 1,
	}
	reportedLine := recordUsageLine(t, reported, quoteForUsage(t, reported))
	imputedLine := recordUsageLine(t, imputed, quoteForUsage(t, imputed))
	for _, check := range []struct {
		line      string
		estimated bool
	}{{reportedLine, false}, {imputedLine, true}} {
		row := rowMap(t, check.line)
		if row["cost_complete"] != !check.estimated || row["display_complete"] != !check.estimated {
			t.Fatalf("row completeness lost provenance: %s", check.line)
		}
		if check.estimated {
			if row["incomplete_reason"] != "missing_price_or_usage" || row["display_status"] != pricing.DisplayStatusUnavailable {
				t.Fatalf("estimated row certified for display: %s", check.line)
			}
			for _, key := range []string{"selected_amount", "selected_currency", "selected_cost"} {
				if _, ok := row[key]; ok {
					t.Fatalf("estimated row retained selected alias %s: %s", key, check.line)
				}
			}
		} else if row["selected_amount"] != "0.0153" || row["selected_currency"] != "USD" || row["display_status"] != pricing.DisplayStatusMatched {
			t.Fatalf("reported row lost display selection: %s", check.line)
		}
	}

	if !reflect.DeepEqual(rowSansProvenance(t, reportedLine), rowSansProvenance(t, imputedLine)) {
		t.Fatalf("fixture rows differ beyond provenance\nreported: %s\nimputed:  %s", reportedLine, imputedLine)
	}
	imputedEstimated, imputedPresent := rowMap(t, imputedLine)["estimated"].(bool)
	if !imputedPresent || !imputedEstimated {
		t.Fatalf("imputed cache split has no provenance marker; it reads exactly like a reported one\nreported: %s\nimputed:  %s",
			reportedLine, imputedLine)
	}
	reportedEstimated, reportedPresent := rowMap(t, reportedLine)["estimated"].(bool)
	if !reportedPresent || reportedEstimated {
		t.Fatalf("reported cache split is not marked as measured\nrow: %s", reportedLine)
	}

	// A row written before this change has no marker at all. It must read as
	// unknown — not as a measured zero-write row — in the production decoder
	// and in a reader that reprices from the row.
	oldRow := `{"ts":"2026-08-01T02:00:00Z","model":"relay/claude-sonnet-4","source":"cli",` +
		`"prompt":5000,"completion":20,"cache_miss":5000,"total":5020,"requests":1,` +
		`"cost_amount":"0.0153","cost_currency":"USD"}`
	old, err := decodeRecords(strings.NewReader(oldRow + "\n"))
	if err != nil || len(old) != 1 {
		t.Fatalf("decode old row: %v / %d rows", err, len(old))
	}
	if old[0].Estimated != nil {
		t.Fatalf("an old row decoded with estimated=%v; absence must stay unknown", *old[0].Estimated)
	}
	if _, measured := rowPriceInput(t, oldRow); measured {
		t.Fatalf("an old row was read as a measured cache split: %s", oldRow)
	}
}

// rowSansProvenance compares original amounts and tokens independently of the
// separately asserted provenance and selected-display facts.
func rowSansProvenance(t *testing.T, line string) map[string]any {
	t.Helper()
	row := rowMap(t, line)
	for _, key := range []string{"ts", "estimated", "cost_complete", "display_complete", "display_status", "incomplete_reason", "selected_amount", "selected_currency", "selected_cost"} {
		delete(row, key)
	}
	return row
}

// TestRecordedPlainCallIsUnchanged is the control: a reported call with no
// cache writes records the same counts and the same money as before, and still
// reprices from its row.
func TestRecordedPlainCallIsUnchanged(t *testing.T) {
	usage := &provider.Usage{
		PromptTokens: 1200, CompletionTokens: 30, TotalTokens: 1230,
		CacheHitTokens: 200, CacheMissTokens: 1000, RequestCount: 1,
	}
	quote := quoteForUsage(t, usage)
	line := recordUsageLine(t, usage, quote)
	row := rowMap(t, line)

	for key, want := range map[string]float64{
		"prompt": 1200, "completion": 30, "cache_hit": 200, "cache_miss": 1000, "total": 1230, "requests": 1,
	} {
		if got := rowNumber(t, row, key); got != want {
			t.Errorf("%s = %v, want %v\nrow: %s", key, got, want, line)
		}
	}
	for _, key := range []string{"cache_write", "cache_write_billed"} {
		if _, ok := row[key]; ok {
			t.Errorf("a call with no cache writes recorded %q\nrow: %s", key, line)
		}
	}
	tokens, _ := rowPriceInput(t, line)
	if repriced, stored := pricing.OriginalCostAmount(anthropicRates, tokens), rowAmount(t, row); repriced != stored {
		t.Fatalf("plain call reprices to %s, stored %s\nrow: %s", repriced, stored, line)
	}
}

// TestCacheWriteRowsReadTheSameFromJSONLAndProjection is the read-path control:
// the new fields belong to the row, not to an aggregate, so a row carrying them
// must count exactly like any other row in both readers.
func TestCacheWriteRowsReadTheSameFromJSONLAndProjection(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-08-01", map[string]any{
		"ts": "2026-08-01T02:00:00Z", "model": anthropicModelRef, "source": "cli",
		"prompt": 7000, "completion": 10, "cache_hit": 4000, "cache_miss": 3000,
		"cache_write": 2000, "cache_write_billed": 2500, "total": 7010, "requests": 1,
		"estimated":   false,
		"cost_amount": "0.01185", "cost_currency": "USD", "cost_complete": true,
	})
	writeDay(t, dir, "2026-08-02", map[string]any{
		"ts": "2026-08-02T02:00:00Z", "model": anthropicModelRef, "source": "cli",
		"prompt": 1200, "completion": 30, "cache_hit": 200, "cache_miss": 1000,
		"total": 1230, "requests": 1,
		"cost_amount": "0.00043", "cost_currency": "USD", "cost_complete": true,
	})
	ctx := context.Background()
	catalog, err := usagecatalog.Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDir(ctx, dir); err != nil {
		t.Fatal(err)
	}

	fromFiles := auditQuery(t, dir, nil)
	fromProjection := auditQuery(t, dir, catalog)
	if fromFiles.Tokens != 8240 || fromFiles.Requests != 2 || fromFiles.CacheHit != 4200 || fromFiles.CacheMiss != 4000 {
		t.Fatalf("JSONL totals = %d tokens / %d requests / cache %d+%d, want 8240 / 2 / 4200+4000",
			fromFiles.Tokens, fromFiles.Requests, fromFiles.CacheHit, fromFiles.CacheMiss)
	}
	if fromProjection.Tokens != fromFiles.Tokens || fromProjection.Requests != fromFiles.Requests ||
		fromProjection.CacheHit != fromFiles.CacheHit || fromProjection.CacheMiss != fromFiles.CacheMiss {
		t.Fatalf("the two read paths disagree on tokens\nJSONL:      %d tokens / %d requests / cache %d+%d\nprojection: %d tokens / %d requests / cache %d+%d",
			fromFiles.Tokens, fromFiles.Requests, fromFiles.CacheHit, fromFiles.CacheMiss,
			fromProjection.Tokens, fromProjection.Requests, fromProjection.CacheHit, fromProjection.CacheMiss)
	}
	if !reflect.DeepEqual(fromFiles.Cost, fromProjection.Cost) || !reflect.DeepEqual(fromFiles.EstimateCost, fromProjection.EstimateCost) {
		t.Fatalf("the two read paths disagree on money\nJSONL:      %+v / %+v\nprojection: %+v / %+v",
			fromFiles.Cost, fromFiles.EstimateCost, fromProjection.Cost, fromProjection.EstimateCost)
	}
	if len(fromFiles.Cost) != 1 || fromFiles.Cost[0].Currency != "USD" || fromFiles.Cost[0].Amount != "0.01228" {
		t.Fatalf("JSONL cost = %+v, want USD 0.01228", fromFiles.Cost)
	}
}
