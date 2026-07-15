package agent

import (
	"testing"
)

// =============================================================================
// OPT-236: TokenAwarePressureValve — Token感知压力阀
// =============================================================================

// TestTAPV_AddPressureWithinThreshold verifies that adding pressure below the
// threshold does not open the valve.
func TestTAPV_AddPressureWithinThreshold(t *testing.T) {
	v := NewTokenAwarePressureValve(100)

	// Add 50 tokens — still below threshold 100.
	opened := v.AddPressure(50)
	if opened {
		t.Errorf("AddPressure(50): expected valve to remain closed, got opened=true")
	}
	if v.IsOpen() {
		t.Errorf("after AddPressure(50): expected IsOpen=false, got true")
	}

	// Add 49 more tokens — total 99, still below threshold.
	opened = v.AddPressure(49)
	if opened {
		t.Errorf("AddPressure(49): expected valve to remain closed, got opened=true")
	}
	if v.IsOpen() {
		t.Errorf("after AddPressure(49): expected IsOpen=false, got true")
	}
}

// TestTAPV_AddPressureExceedsThreshold verifies that adding pressure reaching
// or exceeding the threshold opens the valve.
func TestTAPV_AddPressureExceedsThreshold(t *testing.T) {
	v := NewTokenAwarePressureValve(100)

	// Add exactly the threshold — should open (>= comparison).
	opened := v.AddPressure(100)
	if !opened {
		t.Errorf("AddPressure(100): expected valve to open, got opened=false")
	}
	if !v.IsOpen() {
		t.Errorf("after AddPressure(100): expected IsOpen=true, got false")
	}

	// Test another scenario: accumulate then exceed.
	v.Reset()
	v.AddPressure(50)
	opened = v.AddPressure(60) // total 110 > 100
	if !opened {
		t.Errorf("AddPressure(60) with total 110: expected valve to open, got opened=false")
	}
	if !v.IsOpen() {
		t.Errorf("after exceeding threshold: expected IsOpen=true, got false")
	}
}

// TestTAPV_ReleaseAutoClose verifies that releasing pressure below the
// threshold automatically closes the valve.
func TestTAPV_ReleaseAutoClose(t *testing.T) {
	v := NewTokenAwarePressureValve(100)

	// Build up pressure to open the valve.
	v.AddPressure(150)
	if !v.IsOpen() {
		t.Fatalf("setup: expected valve open after AddPressure(150), got false")
	}

	// Release 60 tokens — pressure drops to 90 < 100, should auto-close.
	v.Release(60)
	if v.IsOpen() {
		t.Errorf("after Release(60) dropping pressure below threshold: expected IsOpen=false, got true")
	}

	// Verify pressure did not go negative with excess release.
	v.Release(200)
	if v.IsOpen() {
		t.Errorf("after excess Release: expected IsOpen=false, got true")
	}
}

// TestTAPV_IsOpen verifies the IsOpen method reflects valve state correctly.
func TestTAPV_IsOpen(t *testing.T) {
	v := NewTokenAwarePressureValve(100)

	// Initially closed.
	if v.IsOpen() {
		t.Errorf("new valve: expected IsOpen=false, got true")
	}

	// After exceeding threshold — open.
	v.AddPressure(100)
	if !v.IsOpen() {
		t.Errorf("after AddPressure(100): expected IsOpen=true, got false")
	}

	// After manual close — closed.
	v.Close()
	if v.IsOpen() {
		t.Errorf("after Close(): expected IsOpen=false, got true")
	}
}

// TestTAPV_Close verifies that Close manually closes the valve regardless of
// current pressure.
func TestTAPV_Close(t *testing.T) {
	v := NewTokenAwarePressureValve(100)

	// Open the valve with high pressure.
	v.AddPressure(200)
	if !v.IsOpen() {
		t.Fatalf("setup: expected valve open, got false")
	}

	// Manually close — pressure is still 200 but valve should be closed.
	v.Close()
	if v.IsOpen() {
		t.Errorf("after Close(): expected IsOpen=false, got true")
	}

	// Closing an already-closed valve should be a no-op (no panic, no error).
	v.Close()
	if v.IsOpen() {
		t.Errorf("after second Close(): expected IsOpen=false, got true")
	}
}

// TestTAPV_StatsAndReset verifies the Stats map returns correct openCount and
// that Reset clears all counters while preserving the threshold.
func TestTAPV_StatsAndReset(t *testing.T) {
	v := NewTokenAwarePressureValve(100)

	// Open valve (openCount=1), then release to auto-close (closedCount=1).
	v.AddPressure(150)
	v.Release(60) // pressure 90 < 100, auto-closes

	// Open valve again (openCount=2).
	v.AddPressure(50) // pressure 140 >= 100, opens again

	stats := v.GetStats()
	openCount, ok := stats["openCount"].(int)
	if !ok {
		t.Fatalf("stats[openCount] type assertion to int failed, got %T", stats["openCount"])
	}
	if openCount != 2 {
		t.Errorf("stats openCount: expected 2, got %d", openCount)
	}

	closedCount, ok := stats["closedCount"].(int)
	if !ok {
		t.Fatalf("stats[closedCount] type assertion to int failed")
	}
	if closedCount != 1 {
		t.Errorf("stats closedCount: expected 1, got %d", closedCount)
	}

	totalReleased, ok := stats["totalReleased"].(int)
	if !ok {
		t.Fatalf("stats[totalReleased] type assertion to int failed")
	}
	if totalReleased != 60 {
		t.Errorf("stats totalReleased: expected 60, got %d", totalReleased)
	}

	// Reset — all counters should be zero, threshold preserved.
	v.Reset()
	stats = v.GetStats()
	openCount, _ = stats["openCount"].(int)
	if openCount != 0 {
		t.Errorf("after Reset: expected openCount=0, got %d", openCount)
	}
	threshold, ok := stats["threshold"].(int)
	if !ok {
		t.Fatalf("stats[threshold] type assertion to int failed")
	}
	if threshold != 100 {
		t.Errorf("after Reset: expected threshold=100 (preserved), got %d", threshold)
	}
	if v.IsOpen() {
		t.Errorf("after Reset: expected IsOpen=false, got true")
	}
}

// =============================================================================
// OPT-237: CacheInvalidationAggregator — 缓存失效聚合器
// =============================================================================

// TestCIA_Add verifies that Add stores keys and tracks the pending count.
func TestCIA_Add(t *testing.T) {
	a := NewCacheInvalidationAggregator(5)

	// Add two distinct keys — should not trigger flush (batch not full).
	if a.Add("key1") {
		t.Errorf("Add(key1): expected false (batch not full), got true")
	}
	if a.Add("key2") {
		t.Errorf("Add(key2): expected false (batch not full), got true")
	}
	if count := a.GetPendingCount(); count != 2 {
		t.Errorf("GetPendingCount: expected 2, got %d", count)
	}

	// Adding a duplicate key should not increase the pending count.
	a.Add("key1")
	if count := a.GetPendingCount(); count != 2 {
		t.Errorf("after duplicate Add(key1): expected pending count 2, got %d", count)
	}
}

// TestCIA_AddReturnsTrueAtMaxBatchSize verifies that Add returns true once the
// pending set reaches maxBatchSize.
func TestCIA_AddReturnsTrueAtMaxBatchSize(t *testing.T) {
	a := NewCacheInvalidationAggregator(3)

	if a.Add("a") {
		t.Errorf("Add(a): expected false (1 < 3), got true")
	}
	if a.Add("b") {
		t.Errorf("Add(b): expected false (2 < 3), got true")
	}
	// Third unique key reaches the batch size threshold.
	if !a.Add("c") {
		t.Errorf("Add(c): expected true (3 >= 3), got false")
	}
	if !a.IsFull() {
		t.Errorf("IsFull: expected true after 3 keys, got false")
	}
}

// TestCIA_Flush verifies that Flush returns all pending keys and clears the set.
func TestCIA_Flush(t *testing.T) {
	a := NewCacheInvalidationAggregator(10)

	a.Add("alpha")
	a.Add("beta")
	a.Add("gamma")

	keys := a.Flush()
	if len(keys) != 3 {
		t.Errorf("Flush: expected 3 keys, got %d", len(keys))
	}

	// Pending set should be empty after flush.
	if count := a.GetPendingCount(); count != 0 {
		t.Errorf("after Flush: expected pending count 0, got %d", count)
	}

	// Verify all expected keys are present.
	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	for _, expected := range []string{"alpha", "beta", "gamma"} {
		if !keySet[expected] {
			t.Errorf("Flush result missing key: %s", expected)
		}
	}

	// Second flush should return empty slice.
	keys2 := a.Flush()
	if len(keys2) != 0 {
		t.Errorf("second Flush: expected 0 keys, got %d", len(keys2))
	}
}

// TestCIA_GetPendingCount verifies the pending count reflects added and
// duplicate keys correctly.
func TestCIA_GetPendingCount(t *testing.T) {
	a := NewCacheInvalidationAggregator(10)

	if count := a.GetPendingCount(); count != 0 {
		t.Errorf("initial GetPendingCount: expected 0, got %d", count)
	}

	a.Add("x")
	a.Add("y")
	if count := a.GetPendingCount(); count != 2 {
		t.Errorf("after 2 adds: expected pending count 2, got %d", count)
	}

	// Duplicate does not increase count.
	a.Add("x")
	if count := a.GetPendingCount(); count != 2 {
		t.Errorf("after duplicate add: expected pending count 2, got %d", count)
	}
}

// TestCIA_IsFull verifies that IsFull returns true only when the pending set
// has reached maxBatchSize.
func TestCIA_IsFull(t *testing.T) {
	a := NewCacheInvalidationAggregator(2)

	if a.IsFull() {
		t.Errorf("initial IsFull: expected false, got true")
	}

	a.Add("first")
	if a.IsFull() {
		t.Errorf("after 1 add (max=2): expected IsFull=false, got true")
	}

	a.Add("second")
	if !a.IsFull() {
		t.Errorf("after 2 adds (max=2): expected IsFull=true, got false")
	}

	// After flush, should not be full.
	a.Flush()
	if a.IsFull() {
		t.Errorf("after Flush: expected IsFull=false, got true")
	}
}

// TestCIA_StatsAndReset verifies the Stats map returns correct aggregatedCount
// and that Reset clears all counters.
func TestCIA_StatsAndReset(t *testing.T) {
	a := NewCacheInvalidationAggregator(10)

	a.Add("a")
	a.Add("b")
	a.Add("c")

	stats := a.GetStats()
	aggregatedCount, ok := stats["aggregatedCount"].(int)
	if !ok {
		t.Fatalf("stats[aggregatedCount] type assertion to int failed, got %T", stats["aggregatedCount"])
	}
	if aggregatedCount != 3 {
		t.Errorf("stats aggregatedCount: expected 3, got %d", aggregatedCount)
	}

	// Flush should move 3 keys to flushedCount.
	a.Flush()
	stats = a.GetStats()
	flushedCount, ok := stats["flushedCount"].(int)
	if !ok {
		t.Fatalf("stats[flushedCount] type assertion to int failed")
	}
	if flushedCount != 3 {
		t.Errorf("stats flushedCount: expected 3, got %d", flushedCount)
	}
	// aggregatedCount should remain 3 (it tracks total ever added).
	aggregatedCount, _ = stats["aggregatedCount"].(int)
	if aggregatedCount != 3 {
		t.Errorf("stats aggregatedCount after flush: expected 3, got %d", aggregatedCount)
	}

	// Reset clears all counters, preserves maxBatchSize.
	a.Reset()
	stats = a.GetStats()
	aggregatedCount, _ = stats["aggregatedCount"].(int)
	if aggregatedCount != 0 {
		t.Errorf("after Reset: expected aggregatedCount=0, got %d", aggregatedCount)
	}
	flushedCount, _ = stats["flushedCount"].(int)
	if flushedCount != 0 {
		t.Errorf("after Reset: expected flushedCount=0, got %d", flushedCount)
	}
	maxBatchSize, ok := stats["maxBatchSize"].(int)
	if !ok {
		t.Fatalf("stats[maxBatchSize] type assertion to int failed")
	}
	if maxBatchSize != 10 {
		t.Errorf("after Reset: expected maxBatchSize=10 (preserved), got %d", maxBatchSize)
	}
}

// =============================================================================
// OPT-238: ContextWindowSnapshotManager — 上下文窗口快照管理器
// =============================================================================

// TestCWSM_TakeSnapshot verifies that TakeSnapshot returns incrementing IDs.
func TestCWSM_TakeSnapshot(t *testing.T) {
	m := NewContextWindowSnapshotManager(5)

	id1 := m.TakeSnapshot(100, 1000)
	if id1 != 1 {
		t.Errorf("first TakeSnapshot ID: expected 1, got %d", id1)
	}
	id2 := m.TakeSnapshot(200, 2000)
	if id2 != 2 {
		t.Errorf("second TakeSnapshot ID: expected 2, got %d", id2)
	}
	id3 := m.TakeSnapshot(300, 3000)
	if id3 != 3 {
		t.Errorf("third TakeSnapshot ID: expected 3, got %d", id3)
	}
	if count := m.GetSnapshotCount(); count != 3 {
		t.Errorf("GetSnapshotCount: expected 3, got %d", count)
	}
}

// TestCWSM_Restore verifies that Restore recovers an existing snapshot and
// returns false for non-existent IDs.
func TestCWSM_Restore(t *testing.T) {
	m := NewContextWindowSnapshotManager(5)

	id := m.TakeSnapshot(150, 5000)

	snap, found := m.Restore(id)
	if !found {
		t.Fatalf("Restore(%d): expected found=true, got false", id)
	}
	if snap.ID != id {
		t.Errorf("Restore ID: expected %d, got %d", id, snap.ID)
	}
	if snap.TokenCount != 150 {
		t.Errorf("Restore TokenCount: expected 150, got %d", snap.TokenCount)
	}
	if snap.Timestamp != 5000 {
		t.Errorf("Restore Timestamp: expected 5000, got %d", snap.Timestamp)
	}

	// Restore non-existent snapshot.
	_, found = m.Restore(999)
	if found {
		t.Errorf("Restore(999): expected found=false, got true")
	}
}

// TestCWSM_GetLatestSnapshot verifies that GetLatestSnapshot returns the most
// recently taken snapshot.
func TestCWSM_GetLatestSnapshot(t *testing.T) {
	m := NewContextWindowSnapshotManager(5)

	// Empty manager — should return false.
	_, found := m.GetLatestSnapshot()
	if found {
		t.Errorf("empty manager GetLatestSnapshot: expected found=false, got true")
	}

	m.TakeSnapshot(100, 1000)
	m.TakeSnapshot(200, 2000)
	m.TakeSnapshot(300, 3000)

	snap, found := m.GetLatestSnapshot()
	if !found {
		t.Fatalf("GetLatestSnapshot: expected found=true, got false")
	}
	if snap.TokenCount != 300 {
		t.Errorf("latest TokenCount: expected 300, got %d", snap.TokenCount)
	}
	if snap.Timestamp != 3000 {
		t.Errorf("latest Timestamp: expected 3000, got %d", snap.Timestamp)
	}
}

// TestCWSM_EvictOldest verifies that exceeding maxSnapshots evicts the oldest
// snapshot.
func TestCWSM_EvictOldest(t *testing.T) {
	m := NewContextWindowSnapshotManager(2)

	id1 := m.TakeSnapshot(100, 1000)
	id2 := m.TakeSnapshot(200, 2000)
	// Third snapshot should evict id1 (the oldest).
	id3 := m.TakeSnapshot(300, 3000)

	if count := m.GetSnapshotCount(); count != 2 {
		t.Errorf("after exceeding maxSnapshots: expected count 2, got %d", count)
	}

	// The oldest snapshot (id1) should no longer be restorable.
	_, found := m.Restore(id1)
	if found {
		t.Errorf("Restore(evicted id1): expected found=false, got true")
	}

	// The two most recent snapshots should still exist.
	if _, found := m.Restore(id2); !found {
		t.Errorf("Restore(id2): expected found=true, got false")
	}
	if _, found := m.Restore(id3); !found {
		t.Errorf("Restore(id3): expected found=true, got false")
	}

	// Latest should be id3.
	snap, _ := m.GetLatestSnapshot()
	if snap.ID != id3 {
		t.Errorf("latest snapshot ID: expected %d, got %d", id3, snap.ID)
	}
}

// TestCWSM_Stats verifies that the Stats map returns correct snapshotCount and
// restoreCount.
func TestCWSM_Stats(t *testing.T) {
	m := NewContextWindowSnapshotManager(5)

	m.TakeSnapshot(100, 1000)
	m.TakeSnapshot(200, 2000)

	stats := m.GetStats()
	snapshotCount, ok := stats["snapshotCount"].(int)
	if !ok {
		t.Fatalf("stats[snapshotCount] type assertion to int failed, got %T", stats["snapshotCount"])
	}
	if snapshotCount != 2 {
		t.Errorf("stats snapshotCount: expected 2, got %d", snapshotCount)
	}

	latestTokenCount, ok := stats["latestTokenCount"].(int)
	if !ok {
		t.Fatalf("stats[latestTokenCount] type assertion to int failed")
	}
	if latestTokenCount != 200 {
		t.Errorf("stats latestTokenCount: expected 200, got %d", latestTokenCount)
	}

	// Perform a restore and check restoreCount.
	m.Restore(1)
	stats = m.GetStats()
	restoreCount, ok := stats["restoreCount"].(int)
	if !ok {
		t.Fatalf("stats[restoreCount] type assertion to int failed")
	}
	if restoreCount != 1 {
		t.Errorf("stats restoreCount: expected 1, got %d", restoreCount)
	}
}

// TestCWSM_Reset verifies that Reset clears all snapshots and counters while
// preserving the maxSnapshots configuration.
func TestCWSM_Reset(t *testing.T) {
	m := NewContextWindowSnapshotManager(5)

	m.TakeSnapshot(100, 1000)
	m.TakeSnapshot(200, 2000)
	m.Restore(1)

	m.Reset()

	if count := m.GetSnapshotCount(); count != 0 {
		t.Errorf("after Reset: expected snapshot count 0, got %d", count)
	}
	_, found := m.GetLatestSnapshot()
	if found {
		t.Errorf("after Reset: expected GetLatestSnapshot found=false, got true")
	}
	stats := m.GetStats()
	snapshotCount, _ := stats["snapshotCount"].(int)
	if snapshotCount != 0 {
		t.Errorf("after Reset: expected stats snapshotCount=0, got %d", snapshotCount)
	}
	restoreCount, _ := stats["restoreCount"].(int)
	if restoreCount != 0 {
		t.Errorf("after Reset: expected stats restoreCount=0, got %d", restoreCount)
	}
	// maxSnapshots should be preserved.
	maxSnapshots, ok := stats["maxSnapshots"].(int)
	if !ok {
		t.Fatalf("stats[maxSnapshots] type assertion to int failed")
	}
	if maxSnapshots != 5 {
		t.Errorf("after Reset: expected maxSnapshots=5 (preserved), got %d", maxSnapshots)
	}
}

// =============================================================================
// OPT-239: TokenAwareBottleneckDetector — Token感知瓶颈检测器
// =============================================================================

// TestTABD_RecordAndDetect verifies that RecordLatency stores latencies and
// DetectBottleneck returns the stage with the highest latency.
func TestTABD_RecordAndDetect(t *testing.T) {
	d := NewTokenAwareBottleneckDetector()

	d.RecordLatency("stage1", 100)
	d.RecordLatency("stage2", 300)
	d.RecordLatency("stage3", 200)

	bottleneck := d.DetectBottleneck()
	if bottleneck != "stage2" {
		t.Errorf("DetectBottleneck: expected 'stage2' (highest latency 300), got '%s'", bottleneck)
	}
}

// TestTABD_GetLatency verifies that GetLatency returns the recorded latency for
// a stage and 0 for unknown stages.
func TestTABD_GetLatency(t *testing.T) {
	d := NewTokenAwareBottleneckDetector()

	d.RecordLatency("ingest", 50)
	d.RecordLatency("process", 120)

	if lat := d.GetLatency("ingest"); lat != 50 {
		t.Errorf("GetLatency(ingest): expected 50, got %d", lat)
	}
	if lat := d.GetLatency("process"); lat != 120 {
		t.Errorf("GetLatency(process): expected 120, got %d", lat)
	}
	// Unknown stage returns 0 (zero value of map lookup).
	if lat := d.GetLatency("nonexistent"); lat != 0 {
		t.Errorf("GetLatency(nonexistent): expected 0, got %d", lat)
	}
}

// TestTABD_GetBottleneckStage verifies that GetBottleneckStage returns the
// current bottleneck stage after detection.
func TestTABD_GetBottleneckStage(t *testing.T) {
	d := NewTokenAwareBottleneckDetector()

	// Before any detection, bottleneck stage should be empty.
	if stage := d.GetBottleneckStage(); stage != "" {
		t.Errorf("initial GetBottleneckStage: expected empty string, got '%s'", stage)
	}

	d.RecordLatency("fast", 10)
	d.RecordLatency("slow", 500)
	d.DetectBottleneck()

	if stage := d.GetBottleneckStage(); stage != "slow" {
		t.Errorf("after DetectBottleneck: expected 'slow', got '%s'", stage)
	}
}

// TestTABD_Stats verifies that the Stats map returns correct stageCount and
// detectionCount.
func TestTABD_Stats(t *testing.T) {
	d := NewTokenAwareBottleneckDetector()

	d.RecordLatency("stage1", 100)
	d.RecordLatency("stage2", 300)
	d.RecordLatency("stage3", 200)

	stats := d.GetStats()
	stageCount, ok := stats["stageCount"].(int)
	if !ok {
		t.Fatalf("stats[stageCount] type assertion to int failed, got %T", stats["stageCount"])
	}
	if stageCount != 3 {
		t.Errorf("stats stageCount: expected 3, got %d", stageCount)
	}

	// Before detection, detectionCount should be 0.
	detectionCount, ok := stats["detectionCount"].(int)
	if !ok {
		t.Fatalf("stats[detectionCount] type assertion to int failed")
	}
	if detectionCount != 0 {
		t.Errorf("stats detectionCount before detect: expected 0, got %d", detectionCount)
	}

	d.DetectBottleneck()
	d.DetectBottleneck()

	stats = d.GetStats()
	detectionCount, _ = stats["detectionCount"].(int)
	if detectionCount != 2 {
		t.Errorf("stats detectionCount after 2 detects: expected 2, got %d", detectionCount)
	}

	maxLatency, ok := stats["maxLatency"].(int)
	if !ok {
		t.Fatalf("stats[maxLatency] type assertion to int failed")
	}
	if maxLatency != 300 {
		t.Errorf("stats maxLatency: expected 300, got %d", maxLatency)
	}
}

// TestTABD_Reset verifies that Reset clears all stages and counters.
func TestTABD_Reset(t *testing.T) {
	d := NewTokenAwareBottleneckDetector()

	d.RecordLatency("stage1", 100)
	d.RecordLatency("stage2", 300)
	d.DetectBottleneck()

	d.Reset()

	// All stages should be cleared.
	if lat := d.GetLatency("stage1"); lat != 0 {
		t.Errorf("after Reset GetLatency(stage1): expected 0, got %d", lat)
	}
	if stage := d.GetBottleneckStage(); stage != "" {
		t.Errorf("after Reset GetBottleneckStage: expected empty string, got '%s'", stage)
	}
	stats := d.GetStats()
	stageCount, _ := stats["stageCount"].(int)
	if stageCount != 0 {
		t.Errorf("after Reset stats stageCount: expected 0, got %d", stageCount)
	}
	detectionCount, _ := stats["detectionCount"].(int)
	if detectionCount != 0 {
		t.Errorf("after Reset stats detectionCount: expected 0, got %d", detectionCount)
	}
	maxLatency, _ := stats["maxLatency"].(int)
	if maxLatency != 0 {
		t.Errorf("after Reset stats maxLatency: expected 0, got %d", maxLatency)
	}
}

// =============================================================================
// OPT-240: PromptCacheEfficiencyMonitor — 提示缓存效率监控器
// =============================================================================

// TestPCEM_RecordAndEfficiency verifies that RecordRequest accumulates token
// counts and GetEfficiency computes the correct ratio.
func TestPCEM_RecordAndEfficiency(t *testing.T) {
	m := NewPromptCacheEfficiencyMonitor()

	// No data — efficiency should be 0.
	if eff := m.GetEfficiency(); eff != 0.0 {
		t.Errorf("initial GetEfficiency: expected 0.0, got %f", eff)
	}

	// Record: saved 50 out of 100 processed (hit), saved 30 out of 100 (miss).
	// Total saved = 80, total processed = 200, efficiency = 0.4.
	m.RecordRequest(50, 100, true)
	m.RecordRequest(30, 100, false)

	eff := m.GetEfficiency()
	expected := 80.0 / 200.0
	if absFloat(eff-expected) > 1e-9 {
		t.Errorf("GetEfficiency: expected %f, got %f", expected, eff)
	}
}

// TestPCEM_GetHitRate verifies that GetHitRate computes the correct hit
// percentage.
func TestPCEM_GetHitRate(t *testing.T) {
	m := NewPromptCacheEfficiencyMonitor()

	// No requests — hit rate should be 0.
	if rate := m.GetHitRate(); rate != 0.0 {
		t.Errorf("initial GetHitRate: expected 0.0, got %f", rate)
	}

	// 3 requests: 2 hits, 1 miss — hit rate = 2/3.
	m.RecordRequest(50, 100, true)
	m.RecordRequest(30, 100, false)
	m.RecordRequest(40, 100, true)

	rate := m.GetHitRate()
	expected := 2.0 / 3.0
	if absFloat(rate-expected) > 1e-9 {
		t.Errorf("GetHitRate: expected %f, got %f", expected, rate)
	}
}

// TestPCEM_HitMissCount verifies that cache hits and misses are counted
// correctly in the stats.
func TestPCEM_HitMissCount(t *testing.T) {
	m := NewPromptCacheEfficiencyMonitor()

	m.RecordRequest(50, 100, true)
	m.RecordRequest(30, 100, false)
	m.RecordRequest(40, 100, true)

	stats := m.GetStats()
	cacheHits, ok := stats["cacheHits"].(int)
	if !ok {
		t.Fatalf("stats[cacheHits] type assertion to int failed, got %T", stats["cacheHits"])
	}
	if cacheHits != 2 {
		t.Errorf("stats cacheHits: expected 2, got %d", cacheHits)
	}

	cacheMisses, ok := stats["cacheMisses"].(int)
	if !ok {
		t.Fatalf("stats[cacheMisses] type assertion to int failed")
	}
	if cacheMisses != 1 {
		t.Errorf("stats cacheMisses: expected 1, got %d", cacheMisses)
	}

	totalTokensSaved, ok := stats["totalTokensSaved"].(int)
	if !ok {
		t.Fatalf("stats[totalTokensSaved] type assertion to int failed")
	}
	if totalTokensSaved != 120 {
		t.Errorf("stats totalTokensSaved: expected 120, got %d", totalTokensSaved)
	}
}

// TestPCEM_Stats verifies that the Stats map returns the correct totalRequests
// and related fields.
func TestPCEM_Stats(t *testing.T) {
	m := NewPromptCacheEfficiencyMonitor()

	m.RecordRequest(50, 100, true)
	m.RecordRequest(30, 100, false)

	stats := m.GetStats()
	totalRequests, ok := stats["totalRequests"].(int)
	if !ok {
		t.Fatalf("stats[totalRequests] type assertion to int failed, got %T", stats["totalRequests"])
	}
	if totalRequests != 2 {
		t.Errorf("stats totalRequests: expected 2, got %d", totalRequests)
	}

	totalTokensProcessed, ok := stats["totalTokensProcessed"].(int)
	if !ok {
		t.Fatalf("stats[totalTokensProcessed] type assertion to int failed")
	}
	if totalTokensProcessed != 200 {
		t.Errorf("stats totalTokensProcessed: expected 200, got %d", totalTokensProcessed)
	}

	// Verify efficiency field in stats matches GetEfficiency.
	effInStats, ok := stats["efficiency"].(float64)
	if !ok {
		t.Fatalf("stats[efficiency] type assertion to float64 failed, got %T", stats["efficiency"])
	}
	effDirect := m.GetEfficiency()
	if absFloat(effInStats-effDirect) > 1e-9 {
		t.Errorf("stats efficiency (%f) != GetEfficiency() (%f)", effInStats, effDirect)
	}

	// Verify hitRate field in stats matches GetHitRate.
	hitRateInStats, ok := stats["hitRate"].(float64)
	if !ok {
		t.Fatalf("stats[hitRate] type assertion to float64 failed, got %T", stats["hitRate"])
	}
	hitRateDirect := m.GetHitRate()
	if absFloat(hitRateInStats-hitRateDirect) > 1e-9 {
		t.Errorf("stats hitRate (%f) != GetHitRate() (%f)", hitRateInStats, hitRateDirect)
	}
}

// TestPCEM_Reset verifies that Reset clears all accumulated data.
func TestPCEM_Reset(t *testing.T) {
	m := NewPromptCacheEfficiencyMonitor()

	m.RecordRequest(50, 100, true)
	m.RecordRequest(30, 100, false)
	m.SetMonitoringDuration(60)

	m.Reset()

	if eff := m.GetEfficiency(); eff != 0.0 {
		t.Errorf("after Reset GetEfficiency: expected 0.0, got %f", eff)
	}
	if rate := m.GetHitRate(); rate != 0.0 {
		t.Errorf("after Reset GetHitRate: expected 0.0, got %f", rate)
	}
	stats := m.GetStats()
	totalRequests, _ := stats["totalRequests"].(int)
	if totalRequests != 0 {
		t.Errorf("after Reset stats totalRequests: expected 0, got %d", totalRequests)
	}
	cacheHits, _ := stats["cacheHits"].(int)
	if cacheHits != 0 {
		t.Errorf("after Reset stats cacheHits: expected 0, got %d", cacheHits)
	}
	cacheMisses, _ := stats["cacheMisses"].(int)
	if cacheMisses != 0 {
		t.Errorf("after Reset stats cacheMisses: expected 0, got %d", cacheMisses)
	}
}

// =============================================================================
// Helper functions
// =============================================================================

// absFloat returns the absolute value of a float64.
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
