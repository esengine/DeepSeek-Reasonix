package agent
import "sync"

// OPT-236: TokenAwarePressureValve — Token感知压力阀
// TokenAwarePressureValve releases pressure when token pressure reaches a threshold.
// It monitors accumulated token pressure and automatically opens the valve
// when the threshold is exceeded, allowing the system to shed load gracefully.
type TokenAwarePressureValve struct {
	mu              sync.RWMutex
	threshold       int  // 开阀阈值（以token计） valve open threshold in tokens
	currentPressure int  // 当前累积压力 current accumulated pressure
	openCount       int  // 阀门开启次数 number of times valve opened
	closedCount     int  // 阀门关闭次数 number of times valve closed
	totalReleased   int  // 累计释放的token总数 total tokens released
	isOpen          bool // 阀门当前是否打开 whether valve is currently open
}

// NewTokenAwarePressureValve creates a new TokenAwarePressureValve with the given threshold.
// NewTokenAwarePressureValve 使用给定的阈值创建新的TokenAwarePressureValve。
func NewTokenAwarePressureValve(threshold int) *TokenAwarePressureValve {
	return &TokenAwarePressureValve{
		threshold:       threshold,
		currentPressure: 0,
		openCount:       0,
		closedCount:     0,
		totalReleased:   0,
		isOpen:          false,
	}
}

// AddPressure adds token pressure and automatically opens the valve if the threshold is exceeded.
// Returns true if the valve was opened (or was already open) due to this addition.
// AddPressure 增加token压力，超过阈值时自动开阀，返回是否开阀。
func (v *TokenAwarePressureValve) AddPressure(tokens int) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.currentPressure += tokens
	if !v.isOpen && tapvShouldOpen(v.currentPressure, v.threshold) {
		v.isOpen = true
		v.openCount++
		return true
	}
	return v.isOpen
}

// Release releases pressure by reducing the current pressure by the given token count.
// If pressure drops below the threshold, the valve automatically closes.
// Release 释放压力（减少当前压力）。
func (v *TokenAwarePressureValve) Release(tokens int) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.currentPressure -= tokens
	if v.currentPressure < 0 {
		v.currentPressure = 0
	}
	v.totalReleased += tokens
	if v.isOpen && v.currentPressure < v.threshold {
		v.isOpen = false
		v.closedCount++
	}
}

// IsOpen returns whether the valve is currently open.
// IsOpen 返回阀门是否打开。
func (v *TokenAwarePressureValve) IsOpen() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.isOpen
}

// Close manually closes the valve regardless of current pressure.
// Close 手动关闭阀门。
func (v *TokenAwarePressureValve) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.isOpen {
		v.isOpen = false
		v.closedCount++
	}
}

// GetStats returns statistics about the pressure valve.
// GetStats 返回压力阀的统计信息。
func (v *TokenAwarePressureValve) GetStats() map[string]interface{} {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return map[string]interface{}{
		"threshold":       v.threshold,
		"currentPressure": v.currentPressure,
		"openCount":       v.openCount,
		"closedCount":     v.closedCount,
		"totalReleased":   v.totalReleased,
		"isOpen":          v.isOpen,
	}
}

// Reset resets the pressure valve to its initial state (preserving threshold).
// Reset 重置压力阀到初始状态（保留阈值配置）。
func (v *TokenAwarePressureValve) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.currentPressure = 0
	v.openCount = 0
	v.closedCount = 0
	v.totalReleased = 0
	v.isOpen = false
}

// tapvShouldOpen determines whether the valve should open given the current pressure and threshold.
// tapvShouldOpen 判断当前压力是否达到开阀阈值。
func tapvShouldOpen(currentPressure, threshold int) bool {
	return currentPressure >= threshold
}
