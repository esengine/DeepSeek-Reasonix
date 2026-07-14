package agent

import (
	"testing"
)

// ── OPT-116: ConversationDepthAnalyzer ──

func TestConversationDepthAnalyzer_Shallow(t *testing.T) {
	a := NewConversationDepthAnalyzer()
	msgs := []string{"hi", "hello"}
	depth := a.AnalyzeDepth(msgs)
	cat := a.GetDepthCategory(depth)
	if cat != "shallow" {
		t.Errorf("short conversation should be shallow, got depth %d (%s)", depth, cat)
	}
}

func TestConversationDepthAnalyzer_Deep(t *testing.T) {
	a := NewConversationDepthAnalyzer()
	msgs := make([]string, 20)
	for i := range msgs {
		msgs[i] = "this is a detailed message about database architecture and performance optimization strategies for large scale distributed systems with complex query patterns and indexing requirements"
	}
	depth := a.AnalyzeDepth(msgs)
	if depth < 40 {
		t.Errorf("long conversation should have depth >= 40, got %d", depth)
	}
}

func TestConversationDepthAnalyzer_Category(t *testing.T) {
	a := NewConversationDepthAnalyzer()
	if a.GetDepthCategory(10) != "shallow" {
		t.Error("10 should be shallow")
	}
	if a.GetDepthCategory(30) != "moderate" {
		t.Error("30 should be moderate")
	}
	if a.GetDepthCategory(50) != "deep" {
		t.Error("50 should be deep")
	}
	if a.GetDepthCategory(70) != "complex" {
		t.Error("70 should be complex")
	}
	if a.GetDepthCategory(90) != "expert" {
		t.Error("90 should be expert")
	}
}

func TestConversationDepthAnalyzer_Stats(t *testing.T) {
	a := NewConversationDepthAnalyzer()
	a.AnalyzeDepth([]string{"hello", "world"})
	stats := a.GetStats()
	if stats["totalAnalyses"].(int) != 1 {
		t.Errorf("totalAnalyses should be 1, got %v", stats["totalAnalyses"])
	}
}

func TestConversationDepthAnalyzer_Reset(t *testing.T) {
	a := NewConversationDepthAnalyzer()
	a.AnalyzeDepth([]string{"hello"})
	a.Reset()
	stats := a.GetStats()
	if stats["totalAnalyses"].(int) != 0 {
		t.Errorf("totalAnalyses should be 0 after reset, got %v", stats["totalAnalyses"])
	}
}

// ── OPT-117: TokenEfficiencyMonitor ──

func TestTokenEfficiencyMonitor_RecordAndCalculate(t *testing.T) {
	m := NewTokenEfficiencyMonitor()
	m.RecordPoint(1000, 500, 200, 100)
	eff := m.CalculateEfficiency()
	// (500 + 200) / (1000 + 500 + 200 + 100) = 700/1800 ≈ 0.389
	if eff < 0.3 || eff > 0.5 {
		t.Errorf("efficiency should be ~0.389, got %f", eff)
	}
}

func TestTokenEfficiencyMonitor_ZeroEfficiency(t *testing.T) {
	m := NewTokenEfficiencyMonitor()
	eff := m.CalculateEfficiency()
	if eff != 0 {
		t.Errorf("no data should give 0 efficiency, got %f", eff)
	}
}

func TestTokenEfficiencyMonitor_Trend(t *testing.T) {
	m := NewTokenEfficiencyMonitor()
	// Improving trend
	m.RecordPoint(1000, 100, 0, 500)
	m.RecordPoint(1000, 200, 0, 400)
	m.RecordPoint(1000, 300, 0, 300)
	m.RecordPoint(1000, 400, 0, 200)
	m.RecordPoint(1000, 500, 0, 100)
	trend := m.GetEfficiencyTrend()
	if trend != "improving" {
		t.Errorf("should be improving, got %s", trend)
	}
}

func TestTokenEfficiencyMonitor_Stats(t *testing.T) {
	m := NewTokenEfficiencyMonitor()
	m.RecordPoint(1000, 500, 200, 100)
	stats := m.GetStats()
	if stats["monitoringPoints"].(int) != 1 {
		t.Errorf("monitoringPoints should be 1, got %v", stats["monitoringPoints"])
	}
	if stats["totalInput"].(int) != 1000 {
		t.Errorf("totalInput should be 1000, got %v", stats["totalInput"])
	}
}

func TestTokenEfficiencyMonitor_Reset(t *testing.T) {
	m := NewTokenEfficiencyMonitor()
	m.RecordPoint(1000, 500, 0, 0)
	m.Reset()
	stats := m.GetStats()
	if stats["monitoringPoints"].(int) != 0 {
		t.Errorf("monitoringPoints should be 0 after reset, got %v", stats["monitoringPoints"])
	}
}

// ── OPT-118: CacheLifecycleManager ──

func TestCacheLifecycleManager_CreateAndAccess(t *testing.T) {
	m := NewCacheLifecycleManager(10)
	m.Create("key1", 100)
	m.Access("key1")
	m.Access("key1")
	stats := m.GetStats()
	if stats["totalCreated"].(int) != 1 {
		t.Errorf("totalCreated should be 1, got %v", stats["totalCreated"])
	}
}

func TestCacheLifecycleManager_EvictExpired(t *testing.T) {
	m := NewCacheLifecycleManager(5)
	m.Create("key1", 100)
	m.Create("key2", 200)
	evicted := m.EvictExpired(10) // both expired (age > 5)
	if evicted != 2 {
		t.Errorf("should evict 2 entries, got %d", evicted)
	}
}

func TestCacheLifecycleManager_NoEviction(t *testing.T) {
	m := NewCacheLifecycleManager(10)
	m.Create("key1", 100)
	evicted := m.EvictExpired(3) // not expired (age = 3 < 10)
	if evicted != 0 {
		t.Errorf("should not evict, got %d", evicted)
	}
}

func TestCacheLifecycleManager_Stats(t *testing.T) {
	m := NewCacheLifecycleManager(10)
	m.Create("key1", 100)
	m.Create("key2", 200)
	m.Access("key1")
	stats := m.GetStats()
	if stats["totalCreated"].(int) != 2 {
		t.Errorf("totalCreated should be 2, got %v", stats["totalCreated"])
	}
	if stats["activeEntries"].(int) != 2 {
		t.Errorf("activeEntries should be 2, got %v", stats["activeEntries"])
	}
}

func TestCacheLifecycleManager_Reset(t *testing.T) {
	m := NewCacheLifecycleManager(10)
	m.Create("key1", 100)
	m.Reset()
	stats := m.GetStats()
	if stats["totalCreated"].(int) != 0 {
		t.Errorf("totalCreated should be 0 after reset, got %v", stats["totalCreated"])
	}
}

// ── OPT-119: PromptSegmentCacheV2 ──

func TestPromptSegmentCacheV2_GetAndPut(t *testing.T) {
	c := NewPromptSegmentCacheV2(500)
	c.PutSegment("greeting", "hello world")
	val, ok := c.GetSegment("greeting")
	if !ok {
		t.Errorf("should find segment, got not found")
	}
	if val != "hello world" {
		t.Errorf("segment should be 'hello world', got %q", val)
	}
}

func TestPromptSegmentCacheV2_Miss(t *testing.T) {
	c := NewPromptSegmentCacheV2(500)
	_, ok := c.GetSegment("nonexistent")
	if ok {
		t.Errorf("should not find nonexistent segment")
	}
}

func TestPromptSegmentCacheV2_Invalidate(t *testing.T) {
	c := NewPromptSegmentCacheV2(500)
	c.PutSegment("key1", "content")
	c.Invalidate("key1")
	_, ok := c.GetSegment("key1")
	if ok {
		t.Errorf("should not find invalidated segment")
	}
}

func TestPromptSegmentCacheV2_HitRate(t *testing.T) {
	c := NewPromptSegmentCacheV2(500)
	c.PutSegment("key1", "content")
	c.GetSegment("key1") // hit
	c.GetSegment("key2") // miss
	rate := c.GetHitRate()
	if rate < 0 || rate > 1 {
		t.Errorf("hit rate should be 0..1, got %f", rate)
	}
}

func TestPromptSegmentCacheV2_Stats(t *testing.T) {
	c := NewPromptSegmentCacheV2(500)
	c.PutSegment("key1", "content")
	c.GetSegment("key1")
	stats := c.GetStats()
	if stats["hits"].(int) != 1 {
		t.Errorf("hits should be 1, got %v", stats["hits"])
	}
}

func TestPromptSegmentCacheV2_Reset(t *testing.T) {
	c := NewPromptSegmentCacheV2(500)
	c.PutSegment("key1", "content")
	c.Reset()
	stats := c.GetStats()
	if stats["hits"].(int) != 0 {
		t.Errorf("hits should be 0 after reset, got %v", stats["hits"])
	}
	if stats["activeSegments"].(int) != 0 {
		t.Errorf("activeSegments should be 0 after reset, got %v", stats["activeSegments"])
	}
}

// ── OPT-120: TokenAwareCompressor ──

func TestTokenAwareCompressor_NoCompression(t *testing.T) {
	c := NewTokenAwareCompressor(10000)
	content := "hello world"
	result := c.Compress(content, 10000) // budget >> content
	if result != content {
		t.Errorf("should not compress when budget is large, got %q", result)
	}
}

func TestTokenAwareCompressor_AggressiveCompression(t *testing.T) {
	c := NewTokenAwareCompressor(100)
	long := "this is a very long content that should be aggressively compressed because the available budget is very small compared to the content length"
	result := c.Compress(long, 10) // budget << content/2
	if len(result) > 110 { // ~100 chars + "..."
		t.Errorf("aggressive compression should be short, got length %d", len(result))
	}
}

func TestTokenAwareCompressor_LightCompression(t *testing.T) {
	c := NewTokenAwareCompressor(1000)
	content := "this is   content   with   extra   spaces   that   should   be   lightly   compressed"
	result := c.Compress(content, 200) // budget between content/2 and content*4
	if len(result) >= len(content) {
		t.Errorf("light compression should reduce length, got %d (original %d)", len(result), len(content))
	}
}

func TestTokenAwareCompressor_CompressionRatio(t *testing.T) {
	c := NewTokenAwareCompressor(100)
	c.RecordCompression("light", 1000, 500)
	ratio := c.GetCompressionRatio()
	if ratio != 0.5 {
		t.Errorf("ratio should be 0.5, got %f", ratio)
	}
}

func TestTokenAwareCompressor_Stats(t *testing.T) {
	c := NewTokenAwareCompressor(100)
	c.RecordCompression("light", 1000, 500)
	stats := c.GetStats()
	if stats["totalCompressed"].(int) != 1 {
		t.Errorf("totalCompressed should be 1, got %v", stats["totalCompressed"])
	}
}

func TestTokenAwareCompressor_Reset(t *testing.T) {
	c := NewTokenAwareCompressor(100)
	c.RecordCompression("light", 1000, 500)
	c.Reset()
	stats := c.GetStats()
	if stats["totalCompressed"].(int) != 0 {
		t.Errorf("totalCompressed should be 0 after reset, got %v", stats["totalCompressed"])
	}
}
