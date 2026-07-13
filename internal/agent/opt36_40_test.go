package agent

import (
	"testing"
)

// ── OPT-36: ModeAwareScheduler ──

func TestModeAwareScheduler_Default(t *testing.T) {
	s := NewModeAwareScheduler(TokenModeFull)
	if s.GetMode() != TokenModeFull {
		t.Fatalf("expected TokenModeFull, got %s", s.GetMode())
	}
	cfg := s.GetConfig()
	if !cfg.EnableToolMemo {
		t.Fatal("full mode should enable tool memo")
	}
}

func TestModeAwareScheduler_Economy(t *testing.T) {
	s := NewModeAwareScheduler(TokenModeEconomy)
	cfg := s.GetConfig()
	if cfg.EnableDescRotation != true {
		t.Fatal("economy mode should enable desc rotation")
	}
	if cfg.PromptCompressLevel < CompressMedium {
		t.Fatal("economy mode should compress at least medium")
	}
}

func TestModeAwareScheduler_SetMode(t *testing.T) {
	s := NewModeAwareScheduler(TokenModeFull)
	s.SetMode(TokenModeEconomy)
	if s.GetMode() != TokenModeEconomy {
		t.Fatalf("expected TokenModeEconomy after SetMode, got %s", s.GetMode())
	}
}

func TestModeAwareScheduler_ApplyToAgent(t *testing.T) {
	s := NewModeAwareScheduler(TokenModeEconomy)
	a := &Agent{
		contextBudget:     NewContextBudget(128000),
		conversationDedup: NewConversationDeduplicator(),
	}
	s.ApplyToAgent(a)
	// economy mode should have been applied without panic
	// (ApplyToAgent adjusts budget and dedup settings)
}

// ── OPT-37: PhantomStatsReporter ──

func TestPhantomStatsReporter_ShouldReport(t *testing.T) {
	r := NewPhantomStatsReporter()
	// 第一次应该可以报告
	if !r.ShouldReport() {
		t.Fatal("first call to ShouldReport should be true")
	}
}

func TestPhantomStatsReporter_CollectSnapshot(t *testing.T) {
	r := NewPhantomStatsReporter()
	a := &Agent{
		cacheEnforcer:       NewCachePrefixEnforcer(),
		toolMemo:            NewToolResultMemo(50),
		conversationDedup:   NewConversationDeduplicator(),
		contextBudget:       NewContextBudget(128000),
		cacheHealthMonitor:  NewCacheHealthMonitor(),
		prefixPinner:        NewPrefixPinner(),
		semanticPruner:      NewSemanticPruner(),
		prefetchPredictor:   NewPrefetchPredictor(),
		windowPredictor:     NewContextWindowPredictor(128000),
		costEstimator:       NewCostEstimator("deepseek"),
	}
	snap := r.CollectSnapshot(a)
	if snap == nil {
		t.Fatal("snapshot should not be nil")
	}
	if snap.ActiveOPTs < 5 {
		t.Fatalf("expected at least 5 active OPTs, got %d", snap.ActiveOPTs)
	}
}

func TestPhantomStatsReporter_GetStats(t *testing.T) {
	r := NewPhantomStatsReporter()
	a := &Agent{
		toolMemo: NewToolResultMemo(50),
	}
	r.CollectSnapshot(a)
	stats := r.GetStats()
	if stats.TotalRequests != 1 {
		t.Fatalf("expected 1 total request, got %d", stats.TotalRequests)
	}
}

// ── OPT-38: DisclosureLazyCoordinator ──

func TestDisclosureLazyCoordinator_Default(t *testing.T) {
	c := NewDisclosureLazyCoordinator()
	if c.GetLevel() != DisclosureCore {
		t.Fatalf("expected DisclosureCore as default, got %s", c.GetLevel())
	}
}

func TestDisclosureLazyCoordinator_SetLevel(t *testing.T) {
	c := NewDisclosureLazyCoordinator()
	// Default is Core; upgrade to Extended should return tools to load
	tools := c.SetLevel(DisclosureExtended)
	if len(tools) == 0 {
		t.Fatal("SetLevel(Extended) from Core should return tools to load")
	}
}

func TestDisclosureLazyCoordinator_MarkLoaded(t *testing.T) {
	c := NewDisclosureLazyCoordinator()
	c.MarkLoaded("bash")
	if !c.IsLoaded("bash") {
		t.Fatal("bash should be marked as loaded")
	}
	if c.IsLoaded("grep") {
		t.Fatal("grep should not be loaded yet")
	}
}

func TestDisclosureLazyCoordinator_TokenSavings(t *testing.T) {
	c := NewDisclosureLazyCoordinator()
	c.SetLevel(DisclosureMinimal)
	savings := c.EstimateTokenSavings()
	if savings <= 0 {
		t.Fatal("minimal level should save tokens")
	}
}

// ── OPT-39: BreakpointOptimizer ──

func TestBreakpointOptimizer_Default(t *testing.T) {
	o := NewBreakpointOptimizer(ProviderAnthropic)
	if !o.ShouldUseBreakpoints() {
		t.Fatal("Anthropic should use breakpoints")
	}
	bps := o.GetBreakpoints()
	if len(bps) == 0 {
		t.Fatal("should have default breakpoints")
	}
}

func TestBreakpointOptimizer_DeepSeek(t *testing.T) {
	o := NewBreakpointOptimizer(ProviderDeepSeek)
	// DeepSeek auto-caches, doesn't use explicit breakpoints
	if o.ShouldUseBreakpoints() {
		t.Fatal("DeepSeek should not need explicit breakpoints")
	}
}

func TestBreakpointOptimizer_RecordHit(t *testing.T) {
	o := NewBreakpointOptimizer(ProviderAnthropic)
	// Record some hits and misses
	for i := 0; i < 10; i++ {
		o.RecordHit(0, true)
	}
	for i := 0; i < 5; i++ {
		o.RecordHit(0, false)
	}
	optimal := o.GetOptimalBreakpoints()
	if len(optimal) == 0 {
		t.Fatal("should still have breakpoints after recording")
	}
}

func TestBreakpointOptimizer_SetProvider(t *testing.T) {
	o := NewBreakpointOptimizer(ProviderUnknown)
	o.SetProvider(ProviderAnthropic)
	if !o.ShouldUseBreakpoints() {
		t.Fatal("after SetProvider(Anthropic) should use breakpoints")
	}
}

// ── OPT-40: SmartCompactionTrigger ──

func TestSmartCompactionTrigger_NoPredictor(t *testing.T) {
	t2 := NewSmartCompactionTrigger(nil)
	action := t2.CheckAndTrigger(10)
	if action != CompactionActionNone {
		t.Fatal("without predictor should return None")
	}
}

func TestSmartCompactionTrigger_WithPredictor(t *testing.T) {
	p := NewContextWindowPredictor(128000)
	t2 := NewSmartCompactionTrigger(p)
	// Without any consumption recorded, prediction should be nil → None
	action := t2.CheckAndTrigger(10)
	if action != CompactionActionNone {
		t.Fatal("without consumption data should return None")
	}
}

func TestSmartCompactionTrigger_HighUsage(t *testing.T) {
	p := NewContextWindowPredictor(128000)
	// Simulate high usage
	p.RecordConsumption(0, 120000, 5000)
	t2 := NewSmartCompactionTrigger(p)
	action := t2.CheckAndTrigger(10)
	// Should trigger because usage is very high
	if action == CompactionActionNone {
		t.Fatal("high usage should trigger compaction")
	}
}

func TestSmartCompactionTrigger_SuppressedCloseTrigger(t *testing.T) {
	p := NewContextWindowPredictor(128000)
	p.RecordConsumption(0, 100000, 3000)
	t2 := NewSmartCompactionTrigger(p)
	// messagesSinceLast=1 is below minMessagesBetweenTriggers=5
	action := t2.CheckAndTrigger(1)
	// Should be suppressed (None) unless immediate threshold
	if action == CompactionActionProactive {
		t.Fatal("should suppress proactive when too close to last trigger")
	}
}

func TestSmartCompactionTrigger_GetStats(t *testing.T) {
	p := NewContextWindowPredictor(128000)
	t2 := NewSmartCompactionTrigger(p)
	stats := t2.GetStats()
	if stats.TotalTriggers != 0 {
		t.Fatalf("expected 0 total triggers initially, got %d", stats.TotalTriggers)
	}
}
