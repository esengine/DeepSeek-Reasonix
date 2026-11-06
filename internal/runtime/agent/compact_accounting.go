package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"sync"
)

type compactionSpendKey struct{}

// compactionSpend accumulates a transaction's bill. It is reached through the
// context rather than passed down because the alternative is every call site
// remembering to return its usage, and the paths that lost usage were the ones
// that discarded a result: a discarded answer is still a charge.
type compactionSpend struct {
	mu    sync.Mutex
	total sessionstore.CompactionUsage
}

// withCompactionSpend opens a transaction. A nested call keeps the outer one so
// the whole compaction lands in a single bill.
func withCompactionSpend(ctx context.Context) (context.Context, *compactionSpend) {
	if ctx == nil {
		ctx = context.Background()
	}
	if spend, _ := ctx.Value(compactionSpendKey{}).(*compactionSpend); spend != nil {
		return ctx, spend
	}
	spend := &compactionSpend{}
	return context.WithValue(ctx, compactionSpendKey{}, spend), spend
}

func compactionSpendFrom(ctx context.Context) *compactionSpend {
	if ctx == nil {
		return nil
	}
	spend, _ := ctx.Value(compactionSpendKey{}).(*compactionSpend)
	return spend
}

func (s *compactionSpend) read() sessionstore.CompactionUsage {
	if s == nil {
		return sessionstore.CompactionUsage{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}
