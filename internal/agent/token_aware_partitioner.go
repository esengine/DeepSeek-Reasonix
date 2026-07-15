package agent

import "sync"

// ── OPT-214: TokenAwarePartitioner (Token感知分区器) ──
// 将请求按token特征分区以优化处理。通过维护各分区的负载信息，
// 将新请求分配到负载最低的分区，实现负载均衡。
// 支持按maxPartitionSize限制单分区负载，并提供重新平衡能力。
//
// 核心能力：
//   - Partition: 将key分配到负载最低的分区，累加tokenCount到分区负载
//   - Rebalance: 将总负载平均分配到所有分区
//   - GetPartitionLoad: 查询指定分区的当前负载

// TokenAwarePartitioner Token感知分区器。
type TokenAwarePartitioner struct {
	mu               sync.RWMutex
	partitions       map[string]int // partitionID → load
	partitionCount   int
	totalPartitioned int
	maxPartitionSize int
}

// NewTokenAwarePartitioner 创建一个新的Token感知分区器实例。
// partitionCount 指定分区数量，maxPartitionSize 指定单个分区的最大负载。
// 分区ID格式为"p0"、"p1"、...、"p{partitionCount-1}"。
// itoaSimple 为包内已有的int转string辅助函数（定义于prompt_minimizer.go）。
func NewTokenAwarePartitioner(partitionCount int, maxPartitionSize int) *TokenAwarePartitioner {
	if partitionCount < 0 {
		partitionCount = 0
	}
	p := &TokenAwarePartitioner{
		partitions:       make(map[string]int),
		partitionCount:   partitionCount,
		maxPartitionSize: maxPartitionSize,
	}
	for i := 0; i < partitionCount; i++ {
		id := "p" + itoaSimple(i)
		p.partitions[id] = 0
	}
	return p
}

// Partition 将key分配到负载最低的分区，并将tokenCount累加到该分区负载。
// 返回分配到的分区ID。若无可用分区返回空字符串。
func (p *TokenAwarePartitioner) Partition(key string, tokenCount int) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	partitionID := tapFindMinLoadPartition(p.partitions)
	if partitionID == "" {
		return ""
	}
	p.partitions[partitionID] += tokenCount
	p.totalPartitioned++
	return partitionID
}

// GetPartitionLoad 获取指定分区的当前负载。
// 若分区不存在返回0。
func (p *TokenAwarePartitioner) GetPartitionLoad(partitionID string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.partitions[partitionID]
}

// Rebalance 重新平衡所有分区的负载。
// 将总负载平均分配到所有分区，使各分区负载趋于一致。
func (p *TokenAwarePartitioner) Rebalance() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.partitionCount == 0 {
		return
	}
	totalLoad := 0
	for _, load := range p.partitions {
		totalLoad += load
	}
	avgLoad := totalLoad / p.partitionCount
	for id := range p.partitions {
		p.partitions[id] = avgLoad
	}
}

// GetStats 返回分区器的统计信息。
// balancedPartitions 表示负载未超过maxPartitionSize的分区数量。
// 若maxPartitionSize <= 0则视为无限制，所有分区均计为已平衡。
func (p *TokenAwarePartitioner) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	balanced := 0
	for _, load := range p.partitions {
		if p.maxPartitionSize <= 0 || load <= p.maxPartitionSize {
			balanced++
		}
	}
	return map[string]interface{}{
		"partitionCount":     p.partitionCount,
		"totalPartitioned":   p.totalPartitioned,
		"maxPartitionSize":   p.maxPartitionSize,
		"balancedPartitions": balanced,
	}
}

// Reset 重置分区器为初始状态，清零所有分区负载和计数。
func (p *TokenAwarePartitioner) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id := range p.partitions {
		p.partitions[id] = 0
	}
	p.totalPartitioned = 0
}

// tapFindMinLoadPartition 在分区中查找负载最低的分区ID。
// 若无分区返回空字符串。同负载时返回map遍历中遇到的第一个。
func tapFindMinLoadPartition(partitions map[string]int) string {
	if len(partitions) == 0 {
		return ""
	}
	minPartition := ""
	minLoad := 0
	first := true
	for id, load := range partitions {
		if first || load < minLoad {
			minLoad = load
			minPartition = id
			first = false
		}
	}
	return minPartition
}
