package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCanonicalizeArgsNormalizes(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		same bool
	}{
		{"key order", `{"path":"a.go","mode":"r"}`, `{"mode":"r","path":"a.go"}`, true},
		{"whitespace", `{ "path" : "a.go" }`, `{"path":"a.go"}`, true},
		{"dynamic ts stripped", `{"path":"a.go","_ts":12345}`, `{"path":"a.go"}`, true},
		{"dynamic session stripped", `{"path":"a.go","session_id":"s9"}`, `{"path":"a.go"}`, true},
		{"number forms", `{"n":1}`, `{"n":1.0}`, true},
		{"different values differ", `{"path":"a.go"}`, `{"path":"b.go"}`, false},
		{"different keys differ", `{"path":"a.go"}`, `{"path":"a.go","mode":"r"}`, false},
		{"nested order", `{"opt":{"x":1,"y":2}}`, `{"opt":{"y":2,"x":1}}`, true},
		{"empty vs whitespace", ``, `   `, true},
	}
	for _, c := range cases {
		gotA := canonicalizeArgs(c.a)
		gotB := canonicalizeArgs(c.b)
		if (gotA == gotB) != c.same {
			t.Errorf("%s: canonicalize(%q)=%q vs (%q)=%q, same=%v want %v",
				c.name, c.a, gotA, c.b, gotB, gotA == gotB, c.same)
		}
	}

	// 非 JSON 参数保守原样（不做归并）。
	if canonicalizeArgs("not json") != "not json" {
		t.Error("non-JSON args must stay verbatim (conservative)")
	}
	// 输出键有序。
	keys := sortedJSONKeys(canonicalizeArgs(`{"b":1,"a":2}`))
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Errorf("keys not sorted: %v", keys)
	}
}

func TestToolCacheHitExpiryEviction(t *testing.T) {
	c := NewToolCache(50*time.Millisecond, 2)

	key := c.Key("read_file", `{"path":"a.go"}`)
	if _, ok := c.Get(key); ok {
		t.Fatal("cold cache must miss")
	}
	c.Put(key, "content A")

	if v, ok := c.Get(key); !ok || v != "content A" {
		t.Fatalf("hit want content A, got %q ok=%v", v, ok)
	}
	// 参数归一化：同一文件不同键序/噪音 → 同一 key。
	if v, ok := c.Get(c.Key("read_file", `{"_ts":1,"path":"a.go"}`)); !ok || v != "content A" {
		t.Fatalf("canonicalized key should hit, got %q ok=%v", v, ok)
	}

	time.Sleep(60 * time.Millisecond)
	if _, ok := c.Get(key); ok {
		t.Error("expired entry must miss")
	}

	// 容量淘汰：超过 maxEntries 逐出最旧。
	c2 := NewToolCache(time.Minute, 2)
	c2.Put(c2.Key("read_file", `{"path":"1"}`), "1")
	time.Sleep(2 * time.Millisecond)
	c2.Put(c2.Key("read_file", `{"path":"2"}`), "2")
	c2.Put(c2.Key("read_file", `{"path":"3"}`), "3") // 淘汰最旧
	if c2.Len() != 2 {
		t.Fatalf("want 2 entries, got %d", c2.Len())
	}
	if _, ok := c2.Get(c2.Key("read_file", `{"path":"1"}`)); ok {
		t.Error("oldest entry should be evicted")
	}
	if _, ok := c2.Get(c2.Key("read_file", `{"path":"3"}`)); !ok {
		t.Error("newest entry should survive")
	}
}

func TestToolCacheStats(t *testing.T) {
	c := NewToolCache(time.Minute, 8)
	k := c.Key("grep", `{"pattern":"foo"}`)
	c.Get(k) // miss
	c.Get(k) // miss
	c.Put(k, "hit")
	c.Get(k) // hit
	h, m := c.Stats()
	if h != 1 || m != 2 {
		t.Fatalf("stats want 1 hit/2 miss, got %d/%d", h, m)
	}
}

// TestToolCacheIntegration 验证 executeBatch 集成：白名单只读工具重复调用
// 命中缓存（第二次不执行），非白名单工具不缓存。
func TestToolCacheIntegration(t *testing.T) {
	// 直接测 ToolCache + 白名单判定（executeBatch 全链路依赖完整
	// 工具注册表与 provider mock，这里验证核心契约）。
	if !cachedToolNames["read_file"] {
		t.Fatal("read_file must be whitelisted")
	}
	if cachedToolNames["bash"] {
		t.Fatal("bash must NOT be cached (shell side effects)")
	}
	if cachedToolNames["write_file"] {
		t.Fatal("write_file must NOT be cached")
	}
	// canonicalize 保持 JSON 结构完整。
	var m map[string]any
	if err := json.Unmarshal([]byte(canonicalizeArgs(`{"path":"x","_ts":9}`)), &m); err != nil {
		t.Fatalf("canonicalized output must be valid JSON: %v", err)
	}
	if _, has := m["_ts"]; has {
		t.Error("dynamic key must be stripped")
	}
	if m["path"] != "x" {
		t.Errorf("path must survive: %v", m)
	}
}
