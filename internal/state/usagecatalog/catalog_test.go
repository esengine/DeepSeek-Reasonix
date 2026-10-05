package usagecatalog

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestReconcileFileAndDuplicateReceiptAreIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-10.jsonl")
	line := []byte("{\"ts\":\"2026-08-10T10:00:00+08:00\",\"model\":\"deepseek/model\",\"source\":\"desktop\",\"total\":42}\n")
	if err := os.WriteFile(path, line, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	rows, err := catalog.Query(ctx, "2026-08-10", "2026-08-10", "desktop", "")
	if err != nil || len(rows) != 1 || rows[0].Total != 42 || rows[0].Requests != 1 {
		t.Fatalf("rows=%#v err=%v", rows, err)
	}
	var receipt AppendReceipt
	if err := catalog.db.QueryRow(`SELECT file_path,day,byte_offset,byte_length,line_hash FROM usage_records WHERE file_path=?`, path).Scan(
		&receipt.Path, &receipt.Day, &receipt.Offset, &receipt.Length, &receipt.LineHash); err != nil {
		t.Fatal(err)
	}
	entry := Entry{Day: receipt.Day, Source: "desktop", ModelRef: "deepseek/model", Provider: "deepseek", Total: 42, Requests: 1}
	if err := catalog.applyReceipt(ctx, receipt, entry); err != nil {
		t.Fatal(err)
	}
	rows, err = catalog.Query(ctx, "2026-08-10", "2026-08-10", "desktop", "")
	if err != nil || len(rows) != 1 || rows[0].Total != 42 {
		t.Fatalf("duplicate changed aggregate: rows=%#v err=%v", rows, err)
	}
}

func TestReadyRejectsExternalAppendUntilReconciled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-10.jsonl")
	if err := os.WriteFile(path, []byte("{\"ts\":\"2026-08-10T10:00:00Z\",\"total\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	if !catalog.Ready(ctx, dir, []string{"2026-08-10"}) {
		t.Fatal("catalog should be ready")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{\"ts\":\"2026-08-10T11:00:00Z\",\"total\":2}\n")
	_ = f.Close()
	if catalog.Ready(ctx, dir, []string{"2026-08-10"}) {
		t.Fatal("external append was treated as indexed")
	}
}

func TestReadyRejectsSameSizeRewriteWithDifferentMtime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-10.jsonl")
	original := []byte("{\"ts\":\"2026-08-10T10:00:00Z\",\"total\":1,\"source\":\"desktop\"}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	if !catalog.Ready(ctx, dir, []string{"2026-08-10"}) {
		t.Fatal("catalog should be ready after reconcile")
	}
	// Same byte length, different content and mtime — Ready must fail so JSONL
	// remains the authority until the projection is rescanned.
	replacement := []byte("{\"ts\":\"2026-08-10T12:00:00Z\",\"total\":9,\"source\":\"desktop\"}\n")
	if len(replacement) != len(original) {
		t.Fatalf("test fixture length mismatch: %d vs %d", len(replacement), len(original))
	}
	if err := os.WriteFile(path, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	// Force a distinct mtime on filesystems with coarse timestamps.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if catalog.Ready(ctx, dir, []string{"2026-08-10"}) {
		t.Fatal("same-size rewrite was treated as still ready")
	}
}

// Two currencies on one key must not become one number. The JSONL path in
// internal/state/stats enforces this and is covered there; the catalog is what
// answers once it is ready, so the same invariant needs proving here.
func TestCostsStaySeparatePerCurrencyInTheProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-10.jsonl")
	lines := "" +
		`{"ts":"2026-08-10T10:00:00+08:00","model":"deepseek/model","source":"cli","total":10,"cost_amount":"1.50","cost_currency":"CNY"}` + "\n" +
		`{"ts":"2026-08-10T11:00:00+08:00","model":"deepseek/model","source":"cli","total":10,"cost_amount":"0.25","cost_currency":"USD"}` + "\n" +
		`{"ts":"2026-08-10T12:00:00+08:00","model":"deepseek/model","source":"cli","total":10,"cost_amount":"2.50","cost_currency":"CNY"}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	rows, err := catalog.Query(ctx, "2026-08-10", "2026-08-10", "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want the three records folded onto one key", len(rows))
	}
	if rows[0].Total != 30 {
		t.Errorf("tokens = %d, want 30 — those do add up", rows[0].Total)
	}
	got := map[string]int64{}
	for _, entry := range rows[0].Costs {
		got[entry.Currency] = entry.Amount
	}
	if len(got) != 2 {
		t.Fatalf("costs = %+v, want one entry per currency", rows[0].Costs)
	}
	// pricing.Amount is fixed-point at 1e9.
	if got["CNY"] != 4_000_000_000 {
		t.Errorf("CNY = %d, want 4.00", got["CNY"])
	}
	if got["USD"] != 250_000_000 {
		t.Errorf("USD = %d, want 0.25", got["USD"])
	}
}

// A currency spelled differently in two rows is one currency. The two read
// paths normalize at different moments, and a panel showing "¥1.50 · ¥2.50"
// for one currency is what that costs.
func TestCurrencySpellingIsNormalizedOnIngest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-11.jsonl")
	lines := "" +
		`{"ts":"2026-08-11T10:00:00+08:00","model":"deepseek/model","source":"cli","total":5,"cost_amount":"1.00","cost_currency":"cny"}` + "\n" +
		`{"ts":"2026-08-11T11:00:00+08:00","model":"deepseek/model","source":"cli","total":5,"cost_amount":"1.00","cost_currency":"CNY"}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-11"); err != nil {
		t.Fatal(err)
	}
	rows, err := catalog.Query(ctx, "2026-08-11", "2026-08-11", "cli", "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%#v err=%v", rows, err)
	}
	if len(rows[0].Costs) != 1 || rows[0].Costs[0].Amount != 2_000_000_000 {
		t.Fatalf("costs = %+v, want a single CNY entry of 2.00", rows[0].Costs)
	}
}

// A file reindexed any number of times answers the same totals. The cost used
// to be added on every pass while the token columns were rebuilt, so a panel
// read months of reindexing as spend.
func TestReconcilingAFileAgainLeavesItsCostAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(testenv.TempDir(t), "2026-08-10.jsonl")
	lines := "{\"ts\":\"2026-08-10T10:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":10,\"cost_amount\":\"0.25\",\"cost_currency\":\"USD\"}\n" +
		"{\"ts\":\"2026-08-10T11:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":20,\"cost_amount\":\"0.5\",\"cost_currency\":\"USD\",\"cost_estimated\":true}\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for range 3 {
		if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := catalog.Query(ctx, "2026-08-10", "2026-08-10", "desktop", "")
	if err != nil || len(rows) != 1 || rows[0].Total != 30 {
		t.Fatalf("rows=%#v err=%v", rows, err)
	}
	if len(rows[0].Costs) != 1 || rows[0].Costs[0].Amount != 750_000_000 || !rows[0].Costs[0].Estimated {
		t.Fatalf("costs = %#v, want one estimated USD 0.75", rows[0].Costs)
	}
}

// Two stores can hold a file for the same day. Reindexing one of them must
// not take the other's day away with it.
func TestReconcilingOneFileKeepsAnotherFileOfTheSameDay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	write := func(total int, amount string) string {
		path := filepath.Join(testenv.TempDir(t), "2026-08-10.jsonl")
		line := "{\"ts\":\"2026-08-10T10:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":" +
			strconv.Itoa(total) + ",\"cost_amount\":\"" + amount + "\",\"cost_currency\":\"USD\"}\n"
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	first, second := write(10, "1"), write(5, "2")
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for _, path := range []string{first, second, second} {
		if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := catalog.Query(ctx, "2026-08-10", "2026-08-10", "desktop", "")
	if err != nil || len(rows) != 1 || rows[0].Total != 15 {
		t.Fatalf("rows=%#v err=%v", rows, err)
	}
	if len(rows[0].Costs) != 1 || rows[0].Costs[0].Amount != 3_000_000_000 {
		t.Fatalf("costs = %#v, want USD 3 from both files", rows[0].Costs)
	}
}

// Each display view partitions the same rows: a row the vendor's table priced
// in that currency moves into it, and any other row keeps its billed currency.
func TestCostViewsReadVendorValuations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-10.jsonl")
	lines := "{\"ts\":\"2026-08-10T10:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":1,\"cost_amount\":\"0.5\",\"cost_currency\":\"USD\",\"valuation_usd\":\"0.5\",\"valuation_cny\":\"3.5\"}\n" +
		"{\"ts\":\"2026-08-10T11:00:00Z\",\"model\":\"relay/m\",\"source\":\"desktop\",\"total\":1,\"cost_amount\":\"0.25\",\"cost_currency\":\"USD\"}\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	live := Entry{Day: "2026-08-10", Source: "desktop", ModelRef: "deepseek/m", Provider: "deepseek", Total: 1,
		Cost: 1_000_000_000, CostCurrency: "USD", Valuations: map[string]int64{"CNY": 7_000_000_000, "USD": 1_000_000_000}}
	receipt := AppendReceipt{Path: path, Day: "2026-08-10", Offset: int64(len(lines)), Length: 10, LineHash: "live"}
	if err := catalog.applyReceipt(ctx, receipt, live); err != nil {
		t.Fatal(err)
	}
	totals := func(view string) map[string]int64 {
		rows, err := catalog.Query(ctx, "2026-08-10", "2026-08-10", "", view)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, row := range rows {
			for _, c := range row.Costs {
				out[c.Currency] += c.Amount
			}
		}
		return out
	}
	if got := totals("CNY"); len(got) != 2 || got["CNY"] != 10_500_000_000 || got["USD"] != 250_000_000 {
		t.Fatalf("CNY view = %v", got)
	}
	if got := totals(""); len(got) != 1 || got["USD"] != 1_750_000_000 {
		t.Fatalf("billed view = %v", got)
	}
}
