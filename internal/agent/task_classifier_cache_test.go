package agent

import (
	"testing"
	"time"
)

// TestClassificationCache_GetSet 测试缓存的基本功能
func TestClassificationCache_GetSet(t *testing.T) {
	cache := newClassificationCache()

	// 测试缓存未命中
	_, ok := cache.Get("hello")
	if ok {
		t.Error("expected cache miss for new input")
	}

	// 测试缓存设置和命中
	cache.Set("hello", false)
	got, ok := cache.Get("hello")
	if !ok {
		t.Error("expected cache hit after Set")
	}
	if got != false {
		t.Errorf("Get() = %v, want false", got)
	}

	// 测试不同输入
	cache.Set("fix bug", true)
	got2, ok2 := cache.Get("fix bug")
	if !ok2 {
		t.Error("expected cache hit for second input")
	}
	if got2 != true {
		t.Errorf("Get() = %v, want true", got2)
	}
}

// TestClassificationCache_TTL 测试缓存过期
func TestClassificationCache_TTL(t *testing.T) {
	cache := &classificationCache{
		entries: make(map[string]cacheEntry),
		maxSize: 100,
		ttl:     100 * time.Millisecond, // 短 TTL 用于测试
	}

	cache.Set("hello", false)

	// 立即获取应该命中
	_, ok := cache.Get("hello")
	if !ok {
		t.Error("expected cache hit immediately after Set")
	}

	// 等待 TTL 过期
	time.Sleep(150 * time.Millisecond)

	// 应该过期
	_, ok = cache.Get("hello")
	if ok {
		t.Error("expected cache miss after TTL expiration")
	}
}

// TestClassificationCache_Clear 测试缓存清除
func TestClassificationCache_Clear(t *testing.T) {
	cache := newClassificationCache()

	cache.Set("hello", false)
	cache.Set("fix bug", true)

	// 清除前应该命中
	_, ok1 := cache.Get("hello")
	_, ok2 := cache.Get("fix bug")
	if !ok1 || !ok2 {
		t.Error("expected cache hits before Clear")
	}

	cache.Clear()

	// 清除后应该未命中
	_, ok1 = cache.Get("hello")
	_, ok2 = cache.Get("fix bug")
	if ok1 || ok2 {
		t.Error("expected cache misses after Clear")
	}
}

// TestClassificationCache_LRUEviction 测试 LRU 淘汰
func TestClassificationCache_LRUEviction(t *testing.T) {
	cache := &classificationCache{
		entries: make(map[string]cacheEntry),
		maxSize: 5, // 小的最大值用于测试
		ttl:     5 * time.Minute,
	}

	// 填充到最大容量
	for i := 0; i < 5; i++ {
		cache.Set(string(rune('a'+i)), i%2 == 0)
	}

	if len(cache.entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(cache.entries))
	}

	// 添加第 6 个元素应该触发淘汰
	cache.Set("f", true)

	// 缓存大小应该减少（淘汰了 20%）
	if len(cache.entries) > 5 {
		t.Errorf("expected cache size <= 5 after eviction, got %d", len(cache.entries))
	}
}

// TestNormalizeInputForCache 测试输入规范化
func TestNormalizeInputForCache(t *testing.T) {
	tests := []struct {
		input1 string
		input2 string
		same   bool
	}{
		{"hello", "hello", true},
		{"hello", "Hello", true},      // 大小写
		{"hello", " hello ", true},    // 空格
		{"hello", "hello!", false},    // 不同内容
		{"fix bug", "fix  bug", true}, // 多余空格（SHA256 会不同，但规范化相同）
	}

	for _, tt := range tests {
		hash1 := normalizeInputForCache(tt.input1)
		hash2 := normalizeInputForCache(tt.input2)
		same := hash1 == hash2
		if same != tt.same {
			t.Errorf("normalizeInputForCache(%q) == normalizeInputForCache(%q): got %v, want %v",
				tt.input1, tt.input2, same, tt.same)
		}
	}
}
