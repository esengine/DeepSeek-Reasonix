package agent

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reasonix/internal/contract/provider"
	"testing"
)

type budgetStreamProbe struct {
	calls  int
	chunks []provider.Chunk
	err    error
}

func (*budgetStreamProbe) Name() string { return "budget-stream" }
func (p *budgetStreamProbe) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	out := make(chan provider.Chunk, len(p.chunks))
	for _, chunk := range p.chunks {
		out <- chunk
	}
	close(out)
	return out, nil
}

func TestBudgetedStreamSettlesBeforeDoneAndKeepsUnknownAttemptsReserved(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []provider.Chunk
		fail   bool
		holds  int
	}{
		{"exact", []provider.Chunk{{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 10}}, {Type: provider.ChunkDone}}, false, 0},
		{"unknown", []provider.Chunk{{Type: provider.ChunkDone}}, false, 1},
		{"estimated", []provider.Chunk{{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 100, Estimated: true}}, {Type: provider.ChunkDone}}, false, 1},
		{"interrupted", []provider.Chunk{{Type: provider.ChunkError, Err: errors.New("interrupted")}}, false, 1},
		{"start-error", nil, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newSharedBudget(TaskBudget{Tokens: 400}, nil)
			p := &budgetStreamProbe{chunks: tc.chunks}
			if tc.fail {
				p.err = errors.New("unknown transport outcome")
			}
			req := provider.Request{MaxTokens: 300}
			ch, err := budgetedStream(t.Context(), p, &req, b, 100, nil, false)
			if tc.fail {
				if err == nil {
					t.Fatal("missing transport error")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				for range ch {
				}
			}
			if len(b.reserved) != tc.holds {
				t.Fatalf("holds=%d", len(b.reserved))
			}
			if tc.holds > 0 {
				if _, _, err := b.reserve(t.Context(), 1, 1, nil); err == nil {
					t.Fatal("uncertain attempt became free")
				}
			}
		})
	}
}

func TestBudgetedRetryCannotShrinkTheFrozenPayload(t *testing.T) {
	b := newSharedBudget(TaskBudget{Tokens: 500}, nil)
	p := &budgetStreamProbe{err: errors.New("unknown outcome")}
	req := provider.Request{MaxTokens: 250}
	_, _ = budgetedStream(t.Context(), p, &req, b, 100, nil, false)
	if _, err := budgetedStream(t.Context(), p, &req, b, 100, nil, true); err == nil {
		t.Fatal("retry changed its output cap")
	}
	if p.calls != 1 || req.MaxTokens != 250 || len(b.reserved) != 1 {
		t.Fatalf("calls=%d output=%d holds=%d", p.calls, req.MaxTokens, len(b.reserved))
	}
}

func TestBudgetLedgerDeletionAndOverflowNeverReopenAllowance(t *testing.T) {
	b := newSharedBudget(TaskBudget{Tokens: 1000}, nil)
	b.path = filepath.Join(t.TempDir(), "budget.json")
	b.scope = "task"
	if _, _, err := b.reserve(t.Context(), 100, 100, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(b.path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.reserve(t.Context(), 100, 100, nil); err == nil {
		t.Fatal("deleted ledger reopened allowance")
	}
	b = newSharedBudget(TaskBudget{Tokens: 1000}, nil)
	b.tokens = math.MaxInt - 10
	b.reserved["old"] = budgetReservation{Tokens: 100}
	if _, _, err := b.reserve(t.Context(), 1, 1, nil); err == nil {
		t.Fatal("overflow reopened allowance")
	}
}

func TestBudgetLedgerRefusesSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	dir := filepath.Join(t.TempDir(), "budgets")
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	b := newSharedBudget(TaskBudget{Tokens: 1000}, nil)
	b.path = filepath.Join(dir, "budget.json")
	b.scope = "task"
	if _, _, err := b.reserve(t.Context(), 100, 100, nil); err == nil {
		t.Fatal("symlink budget directory accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside directory changed", err)
	}
}
