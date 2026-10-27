package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/contract/provider"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSharedReservationsSerializeAcrossIndependentRuntimeOwners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.json")
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			b := newSharedBudget(TaskBudget{Tokens: 1000}, nil)
			b.path = path
			b.scope = "task"
			if _, _, err := b.reserve(context.Background(), 200, 100, nil); err == nil {
				admitted.Add(1)
			}
		})
	}
	wg.Wait()
	if admitted.Load() != 3 {
		t.Fatalf("admitted %d reservations into a 1000-token budget", admitted.Load())
	}
	reopened := newSharedBudget(TaskBudget{Tokens: 1000}, nil)
	reopened.path = path
	reopened.scope = "task"
	if _, _, err := reopened.reserve(context.Background(), 200, 100, nil); err == nil {
		t.Fatal("restart lost outstanding reservations")
	}
}

func TestPersistedBudgetRefusesUnknownRecordsAndKeepsUnknownUsageReserved(t *testing.T) {
	b := newSharedBudget(TaskBudget{Tokens: 1000}, nil)
	b.path = filepath.Join(t.TempDir(), "budget.json")
	b.scope = "task"
	id, _, err := b.reserve(context.Background(), 500, 500, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.settle(context.Background(), id, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.reserve(context.Background(), 1, 1, nil); err == nil {
		t.Fatal("unknown usage was treated as free")
	}
	data, err := os.ReadFile(b.path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["version"] = 99
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.reserve(context.Background(), 1, 1, nil); err == nil {
		t.Fatal("future ledger was consumed")
	}
	if got, err := os.ReadFile(b.path); err != nil || string(got) != string(data) {
		t.Fatal("future ledger changed")
	}
}

func TestCostReservationsRejectUnpricedOrForeignCurrencyRequests(t *testing.T) {
	usd := &provider.Pricing{Input: 1, Output: 2, Currency: "USD"}
	b := newSharedBudget(TaskBudget{Cost: 0.001}, usd)
	for _, rates := range []*provider.Pricing{nil, {Input: 1, Output: 2, Currency: "CNY"}} {
		if _, _, err := b.reserve(context.Background(), 100, 100, rates); err == nil {
			t.Fatal("unpriced or mixed-currency reservation was admitted")
		}
	}
	id, output, err := b.reserve(context.Background(), 100, 1000, usd)
	if err != nil {
		t.Fatal(err)
	}
	if output > 450 {
		t.Fatalf("cost reservation output=%d", output)
	}
	if err := b.settle(context.Background(), id, &provider.Usage{PromptTokens: 100, CacheHitTokens: 100, CompletionTokens: 10}, usd); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.reserve(context.Background(), 100, 100, usd); err != nil {
		t.Fatalf("unused allowance was not released: %v", err)
	}
}

func TestBudgetReservationStressHasNoLostSettlement(t *testing.T) {
	b := newSharedBudget(TaskBudget{Tokens: 1000000}, nil)
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			for range 25 {
				id, _, err := b.reserve(context.Background(), 20, 20, nil)
				if err != nil {
					t.Error(err)
					return
				}
				if err := b.settle(context.Background(), id, &provider.Usage{PromptTokens: 20, CompletionTokens: 10}, nil); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tokens != 64*25*30 || len(b.reserved) != 0 {
		t.Fatalf("spent=%d reservations=%d", b.tokens, len(b.reserved))
	}
	t.Logf("workers=64 completed_requests=1600 tokens=%d leaked_reservations=%d", b.tokens, len(b.reserved))
}

func TestSharedReservationSettlementReleasesOnlyUnusedAllowance(t *testing.T) {
	b := newSharedBudget(TaskBudget{Tokens: 1000}, nil)
	b.path = filepath.Join(t.TempDir(), "budget.json")
	b.scope = "task"
	id, _, err := b.reserve(context.Background(), 300, 500, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.settle(context.Background(), id, &provider.Usage{PromptTokens: 300, CompletionTokens: 100}, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.settle(context.Background(), id, &provider.Usage{PromptTokens: 300, CompletionTokens: 100}, nil); err != nil {
		t.Fatal(err)
	}
	reopened := newSharedBudget(TaskBudget{Tokens: 1000}, nil)
	reopened.path = b.path
	reopened.scope = "task"
	_, maxOut, err := reopened.reserve(context.Background(), 300, 500, nil)
	if err != nil {
		t.Fatal(err)
	}
	if maxOut != 300 {
		t.Fatalf("remaining output=%d, want 300", maxOut)
	}
}
