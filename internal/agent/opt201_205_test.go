package agent

import (
	"strings"
	"testing"
)

// ── OPT-201: TokenAwareBackpressure ──

func TestOPT201_CheckRate_WithinMaxRate(t *testing.T) {
	b := NewTokenAwareBackpressure(1000)
	if !b.CheckRate(500) {
		t.Errorf("CheckRate(500) with maxRate=1000 should return true, got false")
	}
	if !b.CheckRate(1000) {
		t.Errorf("CheckRate(1000) with maxRate=1000 should return true (boundary), got false")
	}
}

func TestOPT201_CheckRate_ExceedingMaxRate(t *testing.T) {
	b := NewTokenAwareBackpressure(1000)
	b.ApplyBackpressure(800) // currentRate = 800
	if b.CheckRate(300) {
		t.Errorf("CheckRate(300) with currentRate=800 maxRate=1000 should return false, got true")
	}
}

func TestOPT201_ApplyBackpressure_ReturnsReducedTokens(t *testing.T) {
	b := NewTokenAwareBackpressure(1000)
	b.ApplyBackpressure(800) // currentRate = 800
	allowed := b.ApplyBackpressure(500) // 800+500 > 1000, remaining=200
	if allowed != 200 {
		t.Errorf("ApplyBackpressure(500) should return 200 (reduced), got %d", allowed)
	}
}

func TestOPT201_ReleasePressure_ClearsBackpressure(t *testing.T) {
	b := NewTokenAwareBackpressure(1000)
	b.ApplyBackpressure(1500) // activates backpressure
	if !b.IsBackpressureActive() {
		t.Errorf("backpressure should be active after ApplyBackpressure(1500)")
	}
	b.ReleasePressure()
	if b.IsBackpressureActive() {
		t.Errorf("backpressure should be inactive after ReleasePressure")
	}
	if !b.CheckRate(500) {
		t.Errorf("CheckRate(500) should return true after ReleasePressure, got false")
	}
}

func TestOPT201_IsBackpressureActive(t *testing.T) {
	b := NewTokenAwareBackpressure(1000)
	if b.IsBackpressureActive() {
		t.Errorf("backpressure should be inactive initially")
	}
	b.ApplyBackpressure(1500)
	if !b.IsBackpressureActive() {
		t.Errorf("backpressure should be active after exceeding maxRate")
	}
}

func TestOPT201_Stats_Activations_AndReset(t *testing.T) {
	b := NewTokenAwareBackpressure(1000)
	b.ApplyBackpressure(1500) // activations=1, totalThrottled=500
	b.ApplyBackpressure(2000) // already active, totalThrottled+=2000
	stats := b.GetStats()
	if stats["activations"].(int) != 1 {
		t.Errorf("activations should be 1, got %v", stats["activations"])
	}
	if stats["totalThrottled"].(int) != 2500 {
		t.Errorf("totalThrottled should be 2500, got %v", stats["totalThrottled"])
	}
	b.Reset()
	stats = b.GetStats()
	if stats["activations"].(int) != 0 {
		t.Errorf("activations should be 0 after Reset, got %v", stats["activations"])
	}
	if stats["currentRate"].(int) != 0 {
		t.Errorf("currentRate should be 0 after Reset, got %v", stats["currentRate"])
	}
	if stats["backpressureActive"].(bool) {
		t.Errorf("backpressureActive should be false after Reset, got %v", stats["backpressureActive"])
	}
}

// ── OPT-202: CacheInvalidationStrategy ──

func TestOPT202_Invalidate_ReturnsCurrentStrategy(t *testing.T) {
	c := NewCacheInvalidationStrategy("immediate")
	s := c.Invalidate("key1")
	if s != "immediate" {
		t.Errorf("Invalidate should return 'immediate', got %q", s)
	}
}

func TestOPT202_SetStrategy_Switches(t *testing.T) {
	c := NewCacheInvalidationStrategy("immediate")
	c.SetStrategy("lazy")
	s := c.Invalidate("key1")
	if s != "lazy" {
		t.Errorf("Invalidate after SetStrategy('lazy') should return 'lazy', got %q", s)
	}
}

func TestOPT202_GetInvalidationCount(t *testing.T) {
	c := NewCacheInvalidationStrategy("immediate")
	c.Invalidate("k1")
	c.Invalidate("k2")
	c.Invalidate("k3")
	if c.GetInvalidationCount("immediate") != 3 {
		t.Errorf("GetInvalidationCount('immediate') should be 3, got %d", c.GetInvalidationCount("immediate"))
	}
	if c.GetInvalidationCount("lazy") != 0 {
		t.Errorf("GetInvalidationCount('lazy') should be 0, got %d", c.GetInvalidationCount("lazy"))
	}
}

func TestOPT202_Stats_TotalInvalidated(t *testing.T) {
	c := NewCacheInvalidationStrategy("immediate")
	c.Invalidate("k1")
	c.Invalidate("k2")
	c.SetStrategy("ttl")
	c.Invalidate("k3")
	stats := c.GetStats()
	if stats["totalInvalidated"].(int) != 3 {
		t.Errorf("totalInvalidated should be 3, got %v", stats["totalInvalidated"])
	}
	if stats["immediateCount"].(int) != 2 {
		t.Errorf("immediateCount should be 2, got %v", stats["immediateCount"])
	}
	if stats["ttlCount"].(int) != 1 {
		t.Errorf("ttlCount should be 1, got %v", stats["ttlCount"])
	}
}

func TestOPT202_Reset(t *testing.T) {
	c := NewCacheInvalidationStrategy("immediate")
	c.Invalidate("k1")
	c.Invalidate("k2")
	c.Reset()
	if c.GetInvalidationCount("immediate") != 0 {
		t.Errorf("GetInvalidationCount('immediate') should be 0 after Reset, got %d", c.GetInvalidationCount("immediate"))
	}
	stats := c.GetStats()
	if stats["totalInvalidated"].(int) != 0 {
		t.Errorf("totalInvalidated should be 0 after Reset, got %v", stats["totalInvalidated"])
	}
}

// ── OPT-203: ContextPruningStrategy ──

// makeMessages creates a slice of `count` messages, each `charLen` characters long.
// Each message contributes roughly charLen/4 tokens when pruned.
func makeMessages(count, charLen int) []string {
	msgs := make([]string, count)
	for i := range msgs {
		msgs[i] = strings.Repeat("a", charLen)
	}
	return msgs
}

func TestOPT203_Prune_TrimMessages(t *testing.T) {
	c := NewContextPruningStrategy("moderate", 400)
	msgs := makeMessages(10, 400) // each 400 chars -> 100 tokens removed
	result, pruned := c.Prune(msgs, 1000)
	if len(result) >= 10 {
		t.Errorf("Prune should reduce message count, got %d", len(result))
	}
	if pruned <= 0 {
		t.Errorf("pruned tokens should be > 0, got %d", pruned)
	}
	if len(result) != 4 {
		t.Errorf("Prune should leave 4 messages, got %d", len(result))
	}
	if pruned != 600 {
		t.Errorf("pruned should be 600, got %d", pruned)
	}
}

func TestOPT203_Prune_AggressivePrunesMore(t *testing.T) {
	aggressive := NewContextPruningStrategy("aggressive", 400)
	moderate := NewContextPruningStrategy("moderate", 400)
	msgs := makeMessages(10, 400)
	aggressiveResult, aggressivePruned := aggressive.Prune(msgs, 1000)
	moderateResult, moderatePruned := moderate.Prune(msgs, 1000)
	if aggressivePruned <= moderatePruned {
		t.Errorf("aggressive should prune more tokens than moderate: aggressive=%d moderate=%d", aggressivePruned, moderatePruned)
	}
	if len(aggressiveResult) >= len(moderateResult) {
		t.Errorf("aggressive should leave fewer messages: aggressive=%d moderate=%d", len(aggressiveResult), len(moderateResult))
	}
}

func TestOPT203_SetStrategy_Switches(t *testing.T) {
	c := NewContextPruningStrategy("moderate", 400)
	c.SetStrategy("conservative")
	stats := c.GetStats()
	if stats["strategy"].(string) != "conservative" {
		t.Errorf("strategy should be 'conservative', got %v", stats["strategy"])
	}
}

func TestOPT203_GetPruneRatio(t *testing.T) {
	c := NewContextPruningStrategy("moderate", 400)
	msgs := makeMessages(10, 400)
	c.Prune(msgs, 1000) // pruned=600, pruneCount=1
	ratio := c.GetPruneRatio()
	// totalRetained = 1 * 400 = 400, totalInput = 600 + 400 = 1000, ratio = 0.6
	if ratio != 0.6 {
		t.Errorf("GetPruneRatio should be 0.6, got %v", ratio)
	}
}

func TestOPT203_Stats_PruneCount(t *testing.T) {
	c := NewContextPruningStrategy("moderate", 400)
	msgs := makeMessages(10, 400)
	c.Prune(msgs, 1000)
	c.Prune(msgs, 1000)
	stats := c.GetStats()
	if stats["pruneCount"].(int) != 2 {
		t.Errorf("pruneCount should be 2, got %v", stats["pruneCount"])
	}
	if stats["totalTokensPruned"].(int) != 1200 {
		t.Errorf("totalTokensPruned should be 1200, got %v", stats["totalTokensPruned"])
	}
}

func TestOPT203_Reset(t *testing.T) {
	c := NewContextPruningStrategy("moderate", 400)
	msgs := makeMessages(10, 400)
	c.Prune(msgs, 1000)
	c.Reset()
	stats := c.GetStats()
	if stats["pruneCount"].(int) != 0 {
		t.Errorf("pruneCount should be 0 after Reset, got %v", stats["pruneCount"])
	}
	if stats["totalTokensPruned"].(int) != 0 {
		t.Errorf("totalTokensPruned should be 0 after Reset, got %v", stats["totalTokensPruned"])
	}
	if c.GetPruneRatio() != 0 {
		t.Errorf("GetPruneRatio should be 0 after Reset, got %v", c.GetPruneRatio())
	}
}

// ── OPT-204: TokenAwareThrottleV2 ──

func TestOPT204_TryConsume_TrueWhenTokensAvailable(t *testing.T) {
	th := NewTokenAwareThrottleV2(1000, 100)
	if !th.TryConsume(100, 10) {
		t.Errorf("TryConsume(100) should return true with full bucket, got false")
	}
	if th.GetBucketLevel() != 900 {
		t.Errorf("bucket level should be 900 after consuming 100, got %d", th.GetBucketLevel())
	}
}

func TestOPT204_TryConsume_FalseWhenBucketEmpty(t *testing.T) {
	th := NewTokenAwareThrottleV2(100, 50)
	th.TryConsume(100, 10) // bucket now 0
	if th.TryConsume(50, 10) {
		t.Errorf("TryConsume(50) should return false when bucket empty, got true")
	}
	if th.GetBucketLevel() != 0 {
		t.Errorf("bucket level should be 0, got %d", th.GetBucketLevel())
	}
}

func TestOPT204_Refill_ReplenishesTokens(t *testing.T) {
	th := NewTokenAwareThrottleV2(1000, 100)
	th.TryConsume(1000, 10) // bucket now 0, windowStart=10
	th.Refill(15)           // elapsed=5, refill=5*100=500
	if th.GetBucketLevel() != 500 {
		t.Errorf("bucket level should be 500 after refill, got %d", th.GetBucketLevel())
	}
}

func TestOPT204_GetBucketLevel(t *testing.T) {
	th := NewTokenAwareThrottleV2(500, 100)
	if th.GetBucketLevel() != 500 {
		t.Errorf("initial bucket level should be 500 (full), got %d", th.GetBucketLevel())
	}
	th.TryConsume(200, 5)
	if th.GetBucketLevel() != 300 {
		t.Errorf("bucket level should be 300 after consuming 200, got %d", th.GetBucketLevel())
	}
}

func TestOPT204_Stats_AllowedAndThrottled(t *testing.T) {
	th := NewTokenAwareThrottleV2(100, 50)
	th.TryConsume(100, 10) // allowed, allowedCount=1
	th.TryConsume(50, 10)  // throttled, throttledCount=1
	stats := th.GetStats()
	if stats["allowedCount"].(int) != 1 {
		t.Errorf("allowedCount should be 1, got %v", stats["allowedCount"])
	}
	if stats["throttledCount"].(int) != 1 {
		t.Errorf("throttledCount should be 1, got %v", stats["throttledCount"])
	}
}

func TestOPT204_Reset(t *testing.T) {
	th := NewTokenAwareThrottleV2(100, 50)
	th.TryConsume(100, 10)
	th.TryConsume(50, 10)
	th.Reset()
	if th.GetBucketLevel() != 100 {
		t.Errorf("bucket level should be 100 (full) after Reset, got %d", th.GetBucketLevel())
	}
	stats := th.GetStats()
	if stats["allowedCount"].(int) != 0 {
		t.Errorf("allowedCount should be 0 after Reset, got %v", stats["allowedCount"])
	}
	if stats["throttledCount"].(int) != 0 {
		t.Errorf("throttledCount should be 0 after Reset, got %v", stats["throttledCount"])
	}
}

// ── OPT-205: PromptCacheRevalidator ──

func TestOPT205_Register_NeedsRevalidation(t *testing.T) {
	r := NewPromptCacheRevalidator(1000)
	r.Register("key1")
	if !r.NeedsRevalidation("key1", 500) {
		t.Errorf("NeedsRevalidation should return true for unvalidated entry, got false")
	}
	if !r.NeedsRevalidation("key1", 2000) {
		t.Errorf("NeedsRevalidation should return true for unvalidated entry (timestamp 0), got false")
	}
}

func TestOPT205_Revalidate_Valid_KeepsEntry(t *testing.T) {
	r := NewPromptCacheRevalidator(1000)
	r.Register("key1")
	result := r.Revalidate("key1", 1000, true)
	if !result {
		t.Errorf("Revalidate with stillValid=true should return true, got false")
	}
	if r.NeedsRevalidation("key1", 1500) {
		t.Errorf("NeedsRevalidation should return false within interval, got true")
	}
	if !r.NeedsRevalidation("key1", 2001) {
		t.Errorf("NeedsRevalidation should return true after interval, got false")
	}
}

func TestOPT205_Revalidate_Invalid_RemovesEntry(t *testing.T) {
	r := NewPromptCacheRevalidator(1000)
	r.Register("key1")
	result := r.Revalidate("key1", 1000, false)
	if result {
		t.Errorf("Revalidate with stillValid=false should return false, got true")
	}
	if !r.NeedsRevalidation("key1", 1000) {
		t.Errorf("NeedsRevalidation should return true for removed entry, got false")
	}
	stats := r.GetStats()
	if stats["entryCount"].(int) != 0 {
		t.Errorf("entryCount should be 0 after removal, got %v", stats["entryCount"])
	}
}

func TestOPT205_Stats_RevalidatedCount(t *testing.T) {
	r := NewPromptCacheRevalidator(1000)
	r.Register("k1")
	r.Register("k2")
	r.Revalidate("k1", 1000, true)
	r.Revalidate("k2", 1000, true)
	stats := r.GetStats()
	if stats["revalidatedCount"].(int) != 2 {
		t.Errorf("revalidatedCount should be 2, got %v", stats["revalidatedCount"])
	}
	if stats["invalidatedCount"].(int) != 0 {
		t.Errorf("invalidatedCount should be 0, got %v", stats["invalidatedCount"])
	}
	if stats["totalChecks"].(int) != 2 {
		t.Errorf("totalChecks should be 2, got %v", stats["totalChecks"])
	}
}

func TestOPT205_Reset(t *testing.T) {
	r := NewPromptCacheRevalidator(1000)
	r.Register("k1")
	r.Revalidate("k1", 1000, true)
	r.Reset()
	stats := r.GetStats()
	if stats["revalidatedCount"].(int) != 0 {
		t.Errorf("revalidatedCount should be 0 after Reset, got %v", stats["revalidatedCount"])
	}
	if stats["entryCount"].(int) != 0 {
		t.Errorf("entryCount should be 0 after Reset, got %v", stats["entryCount"])
	}
}
