package agent
import "sync"

// ── OPT-179: TokenAwareValidator (Token 感知验证器) ──
// 验证请求的 token 数量是否在预算范围 [minTokens, maxTokens] 内。
// 记录验证次数、违规次数和最近一次违规信息，并计算违规率。

// TokenAwareValidator Token 感知验证器，验证请求 token 数是否在预算内。
type TokenAwareValidator struct {
	mu              sync.RWMutex
	maxTokens       int
	minTokens       int
	validationCount int
	violations      int
	lastViolation   string
}

// NewTokenAwareValidator 创建一个新的 Token 感知验证器。
// maxTokens 指定 token 上限，minTokens 指定 token 下限。
func NewTokenAwareValidator(maxTokens int, minTokens int) *TokenAwareValidator {
	return &TokenAwareValidator{
		maxTokens: maxTokens,
		minTokens: minTokens,
	}
}

// Validate 验证 tokenCount 是否在 [minTokens, maxTokens] 范围内。
// 每次调用递增 validationCount；若超出范围则递增 violations 并记录最近违规信息。
// 返回 true 表示验证通过。
func (v *TokenAwareValidator) Validate(tokenCount int) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.validationCount++
	valid := tokenCount >= v.minTokens && tokenCount <= v.maxTokens
	if !valid {
		v.violations++
		v.lastViolation = v.buildViolationLocked(tokenCount)
	}
	return valid
}

// ValidateWithWarning 验证 tokenCount 并返回是否通过及警告信息。
// 验证通过时警告信息为空字符串；违规时返回具体的越界描述。
// 同样更新 validationCount、violations 和 lastViolation。
func (v *TokenAwareValidator) ValidateWithWarning(tokenCount int) (bool, string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.validationCount++
	valid := tokenCount >= v.minTokens && tokenCount <= v.maxTokens
	if valid {
		return true, ""
	}
	v.violations++
	warning := v.buildViolationLocked(tokenCount)
	v.lastViolation = warning
	return false, warning
}

// GetViolationRate 返回违规率 (violations / validationCount)。
// 若 validationCount 为 0 则返回 0。
func (v *TokenAwareValidator) GetViolationRate() float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return tavComputeViolationRate(v.violations, v.validationCount)
}

// GetStats 返回验证器的统计信息，包括 maxTokens、minTokens、validationCount、
// violations、violationRate 和 lastViolation。
func (v *TokenAwareValidator) GetStats() map[string]interface{} {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return map[string]interface{}{
		"maxTokens":       v.maxTokens,
		"minTokens":       v.minTokens,
		"validationCount": v.validationCount,
		"violations":      v.violations,
		"violationRate":   tavComputeViolationRate(v.violations, v.validationCount),
		"lastViolation":   v.lastViolation,
	}
}

// Reset 重置验证器的所有统计数据，保留 maxTokens 和 minTokens 配置。
func (v *TokenAwareValidator) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.validationCount = 0
	v.violations = 0
	v.lastViolation = ""
}

// buildViolationLocked 在已加锁的情况下构造违规描述信息。
func (v *TokenAwareValidator) buildViolationLocked(tokenCount int) string {
	if tokenCount < v.minTokens {
		return "token count below minimum"
	}
	return "token count above maximum"
}

// ── 辅助函数（tav 前缀）──

// tavComputeViolationRate 计算违规率 (violations / total)。
// 若 total <= 0 则返回 0。
func tavComputeViolationRate(violations int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(violations) / float64(total)
}
