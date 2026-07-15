package agent
import "sync"

// OPT-239: TokenAwareBottleneckDetector — Token感知瓶颈检测器
// TokenAwareBottleneckDetector detects bottlenecks in the token processing pipeline.
// It records per-stage latencies and identifies the stage with the highest latency,
// enabling targeted optimization of the slowest pipeline stages.
type TokenAwareBottleneckDetector struct {
	mu              sync.RWMutex
	stages          map[string]int // 阶段名→延迟（毫秒） stage name to latency in ms
	bottleneckStage string         // 当前瓶颈阶段 current bottleneck stage
	maxLatency      int            // 当前最大延迟 current maximum latency
	detectionCount  int            // 累计检测次数 total detection operations performed
}

// NewTokenAwareBottleneckDetector creates a new TokenAwareBottleneckDetector.
// NewTokenAwareBottleneckDetector 创建一个新的TokenAwareBottleneckDetector。
func NewTokenAwareBottleneckDetector() *TokenAwareBottleneckDetector {
	return &TokenAwareBottleneckDetector{
		stages:          make(map[string]int),
		bottleneckStage: "",
		maxLatency:      0,
		detectionCount:  0,
	}
}

// RecordLatency records the latency for a given pipeline stage.
// RecordLatency 记录阶段延迟。
func (d *TokenAwareBottleneckDetector) RecordLatency(stage string, latency int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stages[stage] = latency
	if latency > d.maxLatency {
		d.maxLatency = latency
		d.bottleneckStage = stage
	}
}

// DetectBottleneck detects and returns the bottleneck stage (the one with the highest latency).
// DetectBottleneck 检测瓶颈阶段（延迟最高的）。
func (d *TokenAwareBottleneckDetector) DetectBottleneck() string {
	d.mu.Lock()
	defer d.mu.Unlock()

	stage, maxLat := tabdFindMax(d.stages)
	d.bottleneckStage = stage
	d.maxLatency = maxLat
	d.detectionCount++
	return stage
}

// GetLatency returns the recorded latency for a given stage.
// GetLatency 获取阶段延迟。
func (d *TokenAwareBottleneckDetector) GetLatency(stage string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.stages[stage]
}

// GetBottleneckStage returns the current bottleneck stage name.
// GetBottleneckStage 返回当前瓶颈阶段。
func (d *TokenAwareBottleneckDetector) GetBottleneckStage() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.bottleneckStage
}

// GetStats returns statistics about the bottleneck detector.
// GetStats 返回瓶颈检测器的统计信息。
func (d *TokenAwareBottleneckDetector) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return map[string]interface{}{
		"stageCount":      len(d.stages),
		"bottleneckStage": d.bottleneckStage,
		"maxLatency":      d.maxLatency,
		"detectionCount":  d.detectionCount,
	}
}

// Reset resets the bottleneck detector to its initial state.
// Reset 重置瓶颈检测器到初始状态。
func (d *TokenAwareBottleneckDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stages = make(map[string]int)
	d.bottleneckStage = ""
	d.maxLatency = 0
	d.detectionCount = 0
}

// tabdFindMax finds the stage with the highest latency in the given map.
// Returns the stage name and its latency value.
// tabdFindMax 在阶段延迟map中查找延迟最高的阶段。
func tabdFindMax(stages map[string]int) (string, int) {
	maxStage := ""
	maxLat := 0
	for stage, lat := range stages {
		if lat > maxLat {
			maxLat = lat
			maxStage = stage
		}
	}
	return maxStage, maxLat
}
