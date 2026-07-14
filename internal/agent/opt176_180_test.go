package agent

import (
	"math"
	"sort"
	"testing"
)

// approxEqual 用于浮点数的近似相等比较，避免浮点精度问题导致的断言失败。
func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// ============================================================================
// OPT-176: TokenAwareSerializer — Token感知序列化器
// ============================================================================

// TestTokenAwareSerializer_SerializeDeserializeRoundTrip 验证 Serialize 后 Deserialize 可还原原始消息列表。
func TestTokenAwareSerializer_SerializeDeserializeRoundTrip(t *testing.T) {
	s := NewTokenAwareSerializer("pipe")
	messages := []string{"hello", "world", "foo"}
	serialized := s.Serialize(messages)
	deserialized := s.Deserialize(serialized)
	if len(deserialized) != len(messages) {
		t.Errorf("expected %d messages after round-trip, got %d", len(messages), len(deserialized))
	}
	for i, m := range messages {
		if deserialized[i] != m {
			t.Errorf("message %d: expected %q, got %q", i, m, deserialized[i])
		}
	}
}

// TestTokenAwareSerializer_SerializeMultipleMessages 验证 Serialize 将多消息按分隔符正确拼接。
func TestTokenAwareSerializer_SerializeMultipleMessages(t *testing.T) {
	// pipe 格式使用 "|" 分隔
	pipeSer := NewTokenAwareSerializer("pipe")
	if got := pipeSer.Serialize([]string{"a", "b", "c"}); got != "a|b|c" {
		t.Errorf("pipe: expected 'a|b|c', got %q", got)
	}
	// newline 格式使用 "\n" 分隔
	nlSer := NewTokenAwareSerializer("newline")
	if got := nlSer.Serialize([]string{"a", "b", "c"}); got != "a\nb\nc" {
		t.Errorf("newline: expected 'a\\nb\\nc', got %q", got)
	}
	// 空格式默认为 "compact"，使用单元分隔符 0x1F
	compactSer := NewTokenAwareSerializer("")
	if got := compactSer.Serialize([]string{"a", "b"}); got != "a\x1fb" {
		t.Errorf("compact: expected 'a\\x1fb', got %q", got)
	}
}

// TestTokenAwareSerializer_EstimateSavings 验证 EstimateSavings 返回的节省量计算。
func TestTokenAwareSerializer_EstimateSavings(t *testing.T) {
	s := NewTokenAwareSerializer("compact")
	// 正向节省：input - output
	if got := s.EstimateSavings(100, 60); got != 40 {
		t.Errorf("expected savings 40, got %d", got)
	}
	// output 多于 input 时应返回 0（不产生负值）
	if got := s.EstimateSavings(60, 100); got != 0 {
		t.Errorf("expected savings 0 when output>input, got %d", got)
	}
	// 相等时返回 0
	if got := s.EstimateSavings(50, 50); got != 0 {
		t.Errorf("expected savings 0 when equal, got %d", got)
	}
}

// TestTokenAwareSerializer_StatsSerializeCount 验证 GetStats 中 serializeCount 及 token 累计统计。
func TestTokenAwareSerializer_StatsSerializeCount(t *testing.T) {
	s := NewTokenAwareSerializer("compact")
	// 每条 8 字符 => 2 token
	s.Serialize([]string{"aaaaaaaa", "bbbbbbbb"}) // input 4 token, output 4 token
	s.Serialize([]string{"cccccccc"})             // input 2 token, output 2 token
	stats := s.GetStats()
	if stats["format"].(string) != "compact" {
		t.Errorf("expected format 'compact', got %v", stats["format"])
	}
	if stats["serializeCount"].(int) != 2 {
		t.Errorf("expected serializeCount 2, got %v", stats["serializeCount"])
	}
	if stats["totalInputTokens"].(int) != 6 {
		t.Errorf("expected totalInputTokens 6, got %v", stats["totalInputTokens"])
	}
	if stats["totalOutputTokens"].(int) != 6 {
		t.Errorf("expected totalOutputTokens 6, got %v", stats["totalOutputTokens"])
	}
}

// TestTokenAwareSerializer_Reset 验证 Reset 清空统计但保留 format 配置。
func TestTokenAwareSerializer_Reset(t *testing.T) {
	s := NewTokenAwareSerializer("pipe")
	s.Serialize([]string{"a", "b"})
	s.Serialize([]string{"c"})
	s.Reset()
	stats := s.GetStats()
	if stats["serializeCount"].(int) != 0 {
		t.Errorf("expected serializeCount 0 after reset, got %v", stats["serializeCount"])
	}
	if stats["totalInputTokens"].(int) != 0 {
		t.Errorf("expected totalInputTokens 0 after reset, got %v", stats["totalInputTokens"])
	}
	if stats["totalOutputTokens"].(int) != 0 {
		t.Errorf("expected totalOutputTokens 0 after reset, got %v", stats["totalOutputTokens"])
	}
	// format 应被保留
	if stats["format"].(string) != "pipe" {
		t.Errorf("expected format preserved as 'pipe' after reset, got %v", stats["format"])
	}
}

// ============================================================================
// OPT-177: CachePressureReliever — 缓存压力缓解器
// ============================================================================

// TestCachePressureReliever_RecordInsertAndGetPressure 验证 RecordInsert 后 GetPressure 返回正确压力值。
func TestCachePressureReliever_RecordInsertAndGetPressure(t *testing.T) {
	r := NewCachePressureReliever(100, 0.8)
	for i := 0; i < 50; i++ {
		r.RecordInsert()
	}
	pressure := r.GetPressure()
	if !approxEqual(pressure, 0.5) {
		t.Errorf("expected pressure 0.5 after 50 inserts into 100, got %f", pressure)
	}
	// maxEntries <= 0 时压力应为 0
	zero := NewCachePressureReliever(0, 0.8)
	zero.RecordInsert()
	if got := zero.GetPressure(); got != 0 {
		t.Errorf("expected pressure 0 when maxEntries<=0, got %f", got)
	}
}

// TestCachePressureReliever_ShouldRelieve 验证 ShouldRelieve 在超阈值时返回 true，未超时返回 false。
func TestCachePressureReliever_ShouldRelieve(t *testing.T) {
	// 压力 0.6 > 阈值 0.5 => true
	r := NewCachePressureReliever(100, 0.5)
	for i := 0; i < 60; i++ {
		r.RecordInsert()
	}
	if !r.ShouldRelieve() {
		t.Errorf("expected ShouldRelieve true at pressure 0.6 > threshold 0.5")
	}
	// 压力 0.4 <= 阈值 0.5 => false
	r2 := NewCachePressureReliever(100, 0.5)
	for i := 0; i < 40; i++ {
		r2.RecordInsert()
	}
	if r2.ShouldRelieve() {
		t.Errorf("expected ShouldRelieve false at pressure 0.4 <= threshold 0.5")
	}
	// 压力恰好等于阈值（0.5 > 0.5 为 false）
	r3 := NewCachePressureReliever(100, 0.5)
	for i := 0; i < 50; i++ {
		r3.RecordInsert()
	}
	if r3.ShouldRelieve() {
		t.Errorf("expected ShouldRelieve false when pressure equals threshold (strict >)")
	}
}

// TestCachePressureReliever_Relieve 验证 Relieve 缓解后压力下降并返回实际缓解条目数。
func TestCachePressureReliever_Relieve(t *testing.T) {
	r := NewCachePressureReliever(100, 0.8)
	for i := 0; i < 100; i++ {
		r.RecordInsert()
	}
	before := r.GetPressure()
	relieved := r.Relieve(30)
	if relieved != 30 {
		t.Errorf("expected relieved 30, got %d", relieved)
	}
	after := r.GetPressure()
	if !approxEqual(before, 1.0) {
		t.Errorf("expected pressure 1.0 before relieve, got %f", before)
	}
	if !approxEqual(after, 0.7) {
		t.Errorf("expected pressure 0.7 after relieving 30, got %f", after)
	}
	if !(after < before) {
		t.Errorf("expected pressure to decrease after relieve, before %f after %f", before, after)
	}
	// 缓解数超过当前条目时只缓解实际数量
	r2 := NewCachePressureReliever(100, 0.8)
	for i := 0; i < 10; i++ {
		r2.RecordInsert()
	}
	if got := r2.Relieve(50); got != 10 {
		t.Errorf("expected relieved 10 (capped at current), got %d", got)
	}
}

// TestCachePressureReliever_RecordEviction 验证 RecordEviction 递减当前条目数且不低于 0。
func TestCachePressureReliever_RecordEviction(t *testing.T) {
	r := NewCachePressureReliever(100, 0.8)
	for i := 0; i < 50; i++ {
		r.RecordInsert()
	}
	r.RecordEviction()
	if got := r.GetPressure(); !approxEqual(got, 0.49) {
		t.Errorf("expected pressure 0.49 after one eviction, got %f", got)
	}
	// 当前为 0 时驱逐不会变为负
	r2 := NewCachePressureReliever(100, 0.8)
	r2.RecordEviction()
	if got := r2.GetPressure(); got != 0 {
		t.Errorf("expected pressure 0 when evicting from empty cache, got %f", got)
	}
}

// TestCachePressureReliever_StatsMaxEntries 验证 GetStats 返回正确的 maxEntries 及缓解计数。
func TestCachePressureReliever_StatsMaxEntries(t *testing.T) {
	r := NewCachePressureReliever(100, 0.8)
	for i := 0; i < 80; i++ {
		r.RecordInsert()
	}
	r.Relieve(20)
	stats := r.GetStats()
	if stats["maxEntries"].(int) != 100 {
		t.Errorf("expected maxEntries 100, got %v", stats["maxEntries"])
	}
	if stats["currentEntries"].(int) != 60 {
		t.Errorf("expected currentEntries 60, got %v", stats["currentEntries"])
	}
	if stats["reliefCount"].(int) != 1 {
		t.Errorf("expected reliefCount 1, got %v", stats["reliefCount"])
	}
	if stats["totalRelieved"].(int) != 20 {
		t.Errorf("expected totalRelieved 20, got %v", stats["totalRelieved"])
	}
}

// TestCachePressureReliever_Reset 验证 Reset 清空状态但保留 maxEntries 与阈值配置。
func TestCachePressureReliever_Reset(t *testing.T) {
	r := NewCachePressureReliever(100, 0.8)
	for i := 0; i < 50; i++ {
		r.RecordInsert()
	}
	r.Relieve(10)
	r.Reset()
	stats := r.GetStats()
	if stats["currentEntries"].(int) != 0 {
		t.Errorf("expected currentEntries 0 after reset, got %v", stats["currentEntries"])
	}
	if stats["reliefCount"].(int) != 0 {
		t.Errorf("expected reliefCount 0 after reset, got %v", stats["reliefCount"])
	}
	if stats["totalRelieved"].(int) != 0 {
		t.Errorf("expected totalRelieved 0 after reset, got %v", stats["totalRelieved"])
	}
	// 配置应被保留
	if stats["maxEntries"].(int) != 100 {
		t.Errorf("expected maxEntries preserved as 100 after reset, got %v", stats["maxEntries"])
	}
	if stats["pressureThreshold"].(float64) != 0.8 {
		t.Errorf("expected pressureThreshold preserved as 0.8 after reset, got %v", stats["pressureThreshold"])
	}
	if got := r.GetPressure(); got != 0 {
		t.Errorf("expected pressure 0 after reset, got %f", got)
	}
}

// ============================================================================
// OPT-178: ContextSnapshotFreezer — 上下文快照冻结器
// ============================================================================

// TestContextSnapshotFreezer_FreezeRestoreRoundTrip 验证 Freeze 后 Restore 可还原消息副本。
func TestContextSnapshotFreezer_FreezeRestoreRoundTrip(t *testing.T) {
	f := NewContextSnapshotFreezer(10)
	messages := []string{"msg1", "msg2", "msg3"}
	if ok := f.Freeze("snap1", messages); !ok {
		t.Errorf("expected Freeze to return true for new snapshot")
	}
	restored, ok := f.Restore("snap1")
	if !ok {
		t.Errorf("expected Restore to return true for existing snapshot")
	}
	if len(restored) != len(messages) {
		t.Errorf("expected %d restored messages, got %d", len(messages), len(restored))
	}
	for i, m := range messages {
		if restored[i] != m {
			t.Errorf("restored message %d: expected %q, got %q", i, m, restored[i])
		}
	}
	// Restore 返回的是副本，修改后不应影响内部快照
	restored[0] = "modified"
	restored2, _ := f.Restore("snap1")
	if restored2[0] != "msg1" {
		t.Errorf("snapshot should not be affected by external modification, got %q", restored2[0])
	}
}

// TestContextSnapshotFreezer_ExceedMaxSnapshotsRejected 验证超过 maxSnapshots 时拒绝新增，但覆盖已有 ID 仍成功。
func TestContextSnapshotFreezer_ExceedMaxSnapshotsRejected(t *testing.T) {
	f := NewContextSnapshotFreezer(2)
	f.Freeze("a", []string{"1"})
	f.Freeze("b", []string{"2"})
	// 已达上限，新增应被拒绝
	if ok := f.Freeze("c", []string{"3"}); ok {
		t.Errorf("expected Freeze to be rejected when maxSnapshots reached")
	}
	// 覆盖已有 ID 应成功
	if ok := f.Freeze("a", []string{"new"}); !ok {
		t.Errorf("expected Freeze to succeed when overwriting existing id")
	}
	restored, _ := f.Restore("a")
	if len(restored) != 1 || restored[0] != "new" {
		t.Errorf("expected overwritten snapshot 'new', got %v", restored)
	}
}

// TestContextSnapshotFreezer_RemoveThenRestoreFalse 验证 Remove 后 Restore 返回 false。
func TestContextSnapshotFreezer_RemoveThenRestoreFalse(t *testing.T) {
	f := NewContextSnapshotFreezer(10)
	f.Freeze("snap", []string{"data"})
	f.Remove("snap")
	if _, ok := f.Restore("snap"); ok {
		t.Errorf("expected Restore to return false after Remove")
	}
	// 移除不存在的 ID 不应 panic
	f.Remove("nonexistent")
}

// TestContextSnapshotFreezer_ListSnapshots 验证 ListSnapshots 返回所有已冻结快照的 ID。
func TestContextSnapshotFreezer_ListSnapshots(t *testing.T) {
	f := NewContextSnapshotFreezer(10)
	f.Freeze("s1", []string{"a"})
	f.Freeze("s2", []string{"b"})
	f.Freeze("s3", []string{"c"})
	ids := f.ListSnapshots()
	if len(ids) != 3 {
		t.Errorf("expected 3 snapshot ids, got %d", len(ids))
	}
	sort.Strings(ids)
	expected := []string{"s1", "s2", "s3"}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("snapshot id %d: expected %q, got %q", i, want, ids[i])
		}
	}
}

// TestContextSnapshotFreezer_Stats 验证 GetStats 返回正确的快照与计数统计。
func TestContextSnapshotFreezer_Stats(t *testing.T) {
	f := NewContextSnapshotFreezer(10)
	f.Freeze("s1", []string{"a"})
	f.Freeze("s2", []string{"b"})
	f.Restore("s1")
	stats := f.GetStats()
	if stats["maxSnapshots"].(int) != 10 {
		t.Errorf("expected maxSnapshots 10, got %v", stats["maxSnapshots"])
	}
	if stats["snapshotCount"].(int) != 2 {
		t.Errorf("expected snapshotCount 2, got %v", stats["snapshotCount"])
	}
	if stats["frozenCount"].(int) != 2 {
		t.Errorf("expected frozenCount 2, got %v", stats["frozenCount"])
	}
	if stats["restoredCount"].(int) != 1 {
		t.Errorf("expected restoredCount 1, got %v", stats["restoredCount"])
	}
}

// TestContextSnapshotFreezer_Reset 验证 Reset 清空快照与计数但保留 maxSnapshots 配置。
func TestContextSnapshotFreezer_Reset(t *testing.T) {
	f := NewContextSnapshotFreezer(10)
	f.Freeze("s1", []string{"a"})
	f.Restore("s1")
	f.Reset()
	stats := f.GetStats()
	if stats["snapshotCount"].(int) != 0 {
		t.Errorf("expected snapshotCount 0 after reset, got %v", stats["snapshotCount"])
	}
	if stats["frozenCount"].(int) != 0 {
		t.Errorf("expected frozenCount 0 after reset, got %v", stats["frozenCount"])
	}
	if stats["restoredCount"].(int) != 0 {
		t.Errorf("expected restoredCount 0 after reset, got %v", stats["restoredCount"])
	}
	// maxSnapshots 应被保留
	if stats["maxSnapshots"].(int) != 10 {
		t.Errorf("expected maxSnapshots preserved as 10 after reset, got %v", stats["maxSnapshots"])
	}
	if len(f.ListSnapshots()) != 0 {
		t.Errorf("expected no snapshots after reset, got %d", len(f.ListSnapshots()))
	}
}

// ============================================================================
// OPT-179: TokenAwareValidator — Token感知验证器
// ============================================================================

// TestTokenAwareValidator_ValidateInRange 验证 Validate 在范围内（含边界）返回 true。
func TestTokenAwareValidator_ValidateInRange(t *testing.T) {
	v := NewTokenAwareValidator(1000, 100)
	if !v.Validate(500) {
		t.Errorf("expected Validate true for 500 within [100,1000]")
	}
	if !v.Validate(100) {
		t.Errorf("expected Validate true for 100 at lower boundary")
	}
	if !v.Validate(1000) {
		t.Errorf("expected Validate true for 1000 at upper boundary")
	}
}

// TestTokenAwareValidator_ValidateOutOfRange 验证 Validate 超出范围返回 false。
func TestTokenAwareValidator_ValidateOutOfRange(t *testing.T) {
	v := NewTokenAwareValidator(1000, 100)
	if v.Validate(50) {
		t.Errorf("expected Validate false for 50 below minimum 100")
	}
	if v.Validate(1500) {
		t.Errorf("expected Validate false for 1500 above maximum 1000")
	}
}

// TestTokenAwareValidator_ValidateWithWarning 验证 ValidateWithWarning 返回通过状态与警告信息。
func TestTokenAwareValidator_ValidateWithWarning(t *testing.T) {
	v := NewTokenAwareValidator(1000, 100)
	// 范围内：通过且警告为空
	ok, warning := v.ValidateWithWarning(500)
	if !ok {
		t.Errorf("expected ValidateWithWarning true for 500 within range")
	}
	if warning != "" {
		t.Errorf("expected empty warning when valid, got %q", warning)
	}
	// 低于下限：不通过且返回 below minimum 警告
	ok, warning = v.ValidateWithWarning(50)
	if ok {
		t.Errorf("expected ValidateWithWarning false for 50 below minimum")
	}
	if warning != "token count below minimum" {
		t.Errorf("expected 'token count below minimum', got %q", warning)
	}
	// 高于上限：不通过且返回 above maximum 警告
	ok, warning = v.ValidateWithWarning(1500)
	if ok {
		t.Errorf("expected ValidateWithWarning false for 1500 above maximum")
	}
	if warning != "token count above maximum" {
		t.Errorf("expected 'token count above maximum', got %q", warning)
	}
}

// TestTokenAwareValidator_GetViolationRate 验证 GetViolationRate 返回正确的违规率。
func TestTokenAwareValidator_GetViolationRate(t *testing.T) {
	v := NewTokenAwareValidator(1000, 100)
	v.Validate(500)  // valid
	v.Validate(50)   // violation
	v.Validate(1500) // violation
	rate := v.GetViolationRate()
	if !approxEqual(rate, 2.0/3.0) {
		t.Errorf("expected violation rate 2/3, got %f", rate)
	}
	// 无验证记录时违规率应为 0
	empty := NewTokenAwareValidator(1000, 100)
	if got := empty.GetViolationRate(); got != 0 {
		t.Errorf("expected violation rate 0 for empty validator, got %f", got)
	}
}

// TestTokenAwareValidator_Stats 验证 GetStats 返回正确的验证与违规统计。
func TestTokenAwareValidator_Stats(t *testing.T) {
	v := NewTokenAwareValidator(1000, 100)
	v.Validate(500) // valid
	v.Validate(50)  // violation
	stats := v.GetStats()
	if stats["maxTokens"].(int) != 1000 {
		t.Errorf("expected maxTokens 1000, got %v", stats["maxTokens"])
	}
	if stats["minTokens"].(int) != 100 {
		t.Errorf("expected minTokens 100, got %v", stats["minTokens"])
	}
	if stats["validationCount"].(int) != 2 {
		t.Errorf("expected validationCount 2, got %v", stats["validationCount"])
	}
	if stats["violations"].(int) != 1 {
		t.Errorf("expected violations 1, got %v", stats["violations"])
	}
	if stats["lastViolation"].(string) != "token count below minimum" {
		t.Errorf("expected lastViolation 'token count below minimum', got %v", stats["lastViolation"])
	}
}

// TestTokenAwareValidator_Reset 验证 Reset 清空统计但保留 maxTokens 与 minTokens 配置。
func TestTokenAwareValidator_Reset(t *testing.T) {
	v := NewTokenAwareValidator(1000, 100)
	v.Validate(50)
	v.Validate(1500)
	v.Reset()
	stats := v.GetStats()
	if stats["validationCount"].(int) != 0 {
		t.Errorf("expected validationCount 0 after reset, got %v", stats["validationCount"])
	}
	if stats["violations"].(int) != 0 {
		t.Errorf("expected violations 0 after reset, got %v", stats["violations"])
	}
	if stats["lastViolation"].(string) != "" {
		t.Errorf("expected lastViolation empty after reset, got %v", stats["lastViolation"])
	}
	// 配置应被保留
	if stats["maxTokens"].(int) != 1000 {
		t.Errorf("expected maxTokens preserved as 1000 after reset, got %v", stats["maxTokens"])
	}
	if stats["minTokens"].(int) != 100 {
		t.Errorf("expected minTokens preserved as 100 after reset, got %v", stats["minTokens"])
	}
	if got := v.GetViolationRate(); got != 0 {
		t.Errorf("expected violation rate 0 after reset, got %f", got)
	}
}

// ============================================================================
// OPT-180: PromptRedundancyEliminator — 提示冗余消除器
// ============================================================================

// TestPromptRedundancyEliminator_EliminateRemovesDuplicateNgram 验证 Eliminate 移除重复 n-gram 段落。
func TestPromptRedundancyEliminator_EliminateRemovesDuplicateNgram(t *testing.T) {
	e := NewPromptRedundancyEliminator(2)
	// "the cat" 重复出现，第二次出现应被移除
	result := e.Eliminate("the cat sat the cat ran")
	expected := "the cat sat ran"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestPromptRedundancyEliminator_EliminatePreservesNonRedundant 验证无冗余或 ngramSize 非法时文本保持不变。
func TestPromptRedundancyEliminator_EliminatePreservesNonRedundant(t *testing.T) {
	// 无重复 n-gram，文本应不变
	e := NewPromptRedundancyEliminator(2)
	if got := e.Eliminate("hello world foo bar"); got != "hello world foo bar" {
		t.Errorf("expected unchanged text for non-redundant input, got %q", got)
	}
	// ngramSize <= 0 时应原样返回
	e2 := NewPromptRedundancyEliminator(0)
	if got := e2.Eliminate("a b c d"); got != "a b c d" {
		t.Errorf("expected unchanged text when ngramSize<=0, got %q", got)
	}
	// token 数少于 ngramSize 时应原样返回
	e3 := NewPromptRedundancyEliminator(5)
	if got := e3.Eliminate("a b c"); got != "a b c" {
		t.Errorf("expected unchanged text when tokens < ngramSize, got %q", got)
	}
}

// TestPromptRedundancyEliminator_FindRedundancyReturnsDuplicates 验证 FindRedundancy 返回重复出现的 n-gram。
func TestPromptRedundancyEliminator_FindRedundancyReturnsDuplicates(t *testing.T) {
	e := NewPromptRedundancyEliminator(2)
	redundant := e.FindRedundancy("the cat sat the cat ran")
	if len(redundant) != 1 {
		t.Errorf("expected 1 redundant ngram, got %d (%v)", len(redundant), redundant)
	}
	if len(redundant) > 0 && redundant[0] != "the cat" {
		t.Errorf("expected redundant ngram 'the cat', got %q", redundant[0])
	}
}

// TestPromptRedundancyEliminator_FindRedundancyReturnsNilForNoRedundancy 验证无冗余时 FindRedundancy 返回 nil。
func TestPromptRedundancyEliminator_FindRedundancyReturnsNilForNoRedundancy(t *testing.T) {
	e := NewPromptRedundancyEliminator(2)
	if got := e.FindRedundancy("hello world foo bar"); got != nil {
		t.Errorf("expected nil for non-redundant text, got %v", got)
	}
	// ngramSize <= 0 时也应返回 nil
	e2 := NewPromptRedundancyEliminator(0)
	if got := e2.FindRedundancy("a b c"); got != nil {
		t.Errorf("expected nil when ngramSize<=0, got %v", got)
	}
}

// TestPromptRedundancyEliminator_StatsEliminatedCount 验证 GetStats 中 eliminatedCount 等统计。
func TestPromptRedundancyEliminator_StatsEliminatedCount(t *testing.T) {
	e := NewPromptRedundancyEliminator(2)
	// "the cat" 重复一次 => eliminatedCount=1, tokensSaved=len("the cat")/4=1, trackedNgrams=3
	e.Eliminate("the cat sat the cat ran")
	stats := e.GetStats()
	if stats["ngramSize"].(int) != 2 {
		t.Errorf("expected ngramSize 2, got %v", stats["ngramSize"])
	}
	if stats["eliminatedCount"].(int) != 1 {
		t.Errorf("expected eliminatedCount 1, got %v", stats["eliminatedCount"])
	}
	if stats["tokensSaved"].(int) != 1 {
		t.Errorf("expected tokensSaved 1, got %v", stats["tokensSaved"])
	}
	if stats["trackedNgrams"].(int) != 3 {
		t.Errorf("expected trackedNgrams 3, got %v", stats["trackedNgrams"])
	}
}

// TestPromptRedundancyEliminator_Reset 验证 Reset 清空状态但保留 ngramSize 配置。
func TestPromptRedundancyEliminator_Reset(t *testing.T) {
	e := NewPromptRedundancyEliminator(2)
	e.Eliminate("the cat sat the cat ran")
	e.Reset()
	stats := e.GetStats()
	if stats["eliminatedCount"].(int) != 0 {
		t.Errorf("expected eliminatedCount 0 after reset, got %v", stats["eliminatedCount"])
	}
	if stats["tokensSaved"].(int) != 0 {
		t.Errorf("expected tokensSaved 0 after reset, got %v", stats["tokensSaved"])
	}
	if stats["trackedNgrams"].(int) != 0 {
		t.Errorf("expected trackedNgrams 0 after reset, got %v", stats["trackedNgrams"])
	}
	// ngramSize 应被保留
	if stats["ngramSize"].(int) != 2 {
		t.Errorf("expected ngramSize preserved as 2 after reset, got %v", stats["ngramSize"])
	}
}
