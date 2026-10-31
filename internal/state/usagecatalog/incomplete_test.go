package usagecatalog

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

// A counted row whose quote could not price it must survive reconcile and
// re-derive as incomplete, while a priced sibling still carries its amount.
func TestAnUnpricedRowSurvivesReconcileAsIncomplete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-10.jsonl")
	lines := []byte(
		"{\"ts\":\"2026-08-10T10:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":42,\"cost_complete\":false,\"incomplete_reason\":\"no_price\"}\n" +
			"{\"ts\":\"2026-08-10T11:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":8,\"cost_amount\":\"1\",\"cost_currency\":\"USD\",\"cost_complete\":true}\n")
	if err := os.WriteFile(path, lines, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	for pass := range 2 { // second pass re-derives the day
		if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		rows, err := catalog.Query(ctx, "2026-08-10", "2026-08-10", "desktop", "")
		if err != nil || len(rows) != 1 {
			t.Fatalf("pass %d: rows=%#v err=%v", pass, rows, err)
		}
		row := rows[0]
		if row.Total != 50 || row.CostIncomplete != 1 {
			t.Fatalf("pass %d: total/incomplete = %d/%d, want 50/1", pass, row.Total, row.CostIncomplete)
		}
		if len(row.Costs) != 1 || row.Costs[0].Currency != "USD" || row.Costs[0].Amount != 1_000_000_000 {
			t.Fatalf("pass %d: costs = %#v, want the priced sibling's USD 1 (1e9 fixed-point units)", pass, row.Costs)
		}
	}
}

// An unpriced row is indexed with the reason it recorded, and a row with no
// cost fact takes the product's existing "no_price". Repeated reconciles must
// answer the same count and vocabulary, not double the count.
func TestUnpricedReasonsAreIndexedOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-10.jsonl")
	lines := []byte(
		"{\"ts\":\"2026-08-10T10:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":42,\"cost_complete\":false,\"incomplete_reason\":\"no_price\"}\n" +
			"{\"ts\":\"2026-08-10T11:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":7}\n" +
			"{\"ts\":\"2026-08-10T12:00:00Z\",\"model\":\"deepseek/m\",\"source\":\"desktop\",\"total\":8,\"cost_amount\":\"1\",\"cost_currency\":\"USD\",\"cost_complete\":true}\n")
	if err := os.WriteFile(path, lines, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	for pass := range 2 { // second pass re-derives the day
		if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		got, err := catalog.ProvenanceInRange(ctx, "2026-08-10", "2026-08-10", "desktop")
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if got.UnpricedCalls != 2 || !reflect.DeepEqual(got.Reasons, []string{"no_price"}) {
			t.Fatalf("pass %d: unpriced = %d / reasons %v, want 2 / [no_price]", pass, got.UnpricedCalls, got.Reasons)
		}
		if got, err = catalog.ProvenanceInRange(ctx, "2026-08-10", "2026-08-10", "cli"); err != nil || got.UnpricedCalls != 0 {
			t.Fatalf("pass %d: source filter read %d unpriced calls (err %v), want none", pass, got.UnpricedCalls, err)
		}
	}
}
