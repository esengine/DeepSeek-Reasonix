package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider/responses"
)

func TestRetrieveInfoToolLocalHit(t *testing.T) {
	cleanCacheForTool(t)
	q := "2026年8月3日北京天气"
	responses.SaveKnowledge(&responses.KnowledgeEntry{Query: q, AnswerSummary: "晴朗 25-32℃"})
	defer cleanCacheForTool(t)

	out, err := (retrieveInfo{}).Execute(context.Background(), json.RawMessage(`{"query":"2026年8月3日北京天气"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "本地知识缓存命中") || !strings.Contains(out, "晴朗") {
		t.Fatalf("hit output wrong: %q", out)
	}
}

func TestRetrieveInfoToolMissBlocked(t *testing.T) {
	cleanCacheForTool(t)
	// 未配置 deepseek-responses（hook 返回 errNoResponsesProvider）→
	// needs_grant 结构化标志（前端弹授权对话框），绝不静默联网。
	systemFetchTestHook = func(ctx context.Context, query string, tier responses.RetrievalTier) (*responses.KnowledgeEntry, error) {
		return nil, errNoResponsesProvider
	}
	defer func() { systemFetchTestHook = nil }()

	out, err := (retrieveInfo{}).Execute(context.Background(), json.RawMessage(`{"query":"完全没查过的问题"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var flag struct {
		NeedsGrant bool     `json:"needs_grant"`
		Options    []string `json:"options"`
	}
	if err := json.Unmarshal([]byte(out), &flag); err != nil {
		t.Fatalf("miss output must be structured JSON, got %q", out)
	}
	if !flag.NeedsGrant {
		t.Fatalf("must carry needs_grant=true, got %q", out)
	}
	// 时长选项（简化后两档）
	if len(flag.Options) != 2 || flag.Options[0] != "session" || flag.Options[1] != "permanent" {
		if len(flag.Options) != 1 || flag.Options[0] != "year" {
			t.Fatalf("must offer the single year grant option, got %v", flag.Options)
		}
	}
}

// TestRetrieveInfoToolSystemFetch verifies the cache-miss + granted path:
// the tool fetches through the (hooked) system pipeline, persists the
// distilled entry, and reports 联网检索完成.
func TestRetrieveInfoToolSystemFetch(t *testing.T) {
	cleanCacheForTool(t)
	systemFetchTestHook = func(ctx context.Context, query string, tier responses.RetrievalTier) (*responses.KnowledgeEntry, error) {
		return responses.DistillEntry(query, "## 来源\n- 路透：https://reuters.com/a\n**1. 事实**：测试内容", 42, "simple"), nil
	}
	defer func() { systemFetchTestHook = nil }()

	out, err := (retrieveInfo{}).Execute(context.Background(), json.RawMessage(`{"query":"系统管道联网测试"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "联网检索完成") {
		t.Fatalf("must report web fetch through system pipeline, got %q", out)
	}
	if !strings.Contains(out, "reuters.com") {
		t.Fatalf("must include distilled sources, got %q", out)
	}
	// 已落盘：再次查询命中缓存（零联网）。
	systemFetchTestHook = func(ctx context.Context, query string, tier responses.RetrievalTier) (*responses.KnowledgeEntry, error) {
		t.Fatal("cache hit must not re-fetch")
		return nil, nil
	}
	out2, err := (retrieveInfo{}).Execute(context.Background(), json.RawMessage(`{"query":"系统管道联网测试"}`))
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if !strings.Contains(out2, "缓存命中") {
		t.Fatalf("second lookup must hit cache, got %q", out2)
	}
}

func TestRetrieveInfoToolEmptyQuery(t *testing.T) {
	if _, err := (retrieveInfo{}).Execute(context.Background(), json.RawMessage(`{"query":"  "}`)); err == nil {
		t.Fatal("empty query must error")
	}
}

func cleanCacheForTool(t *testing.T) {
	t.Helper()
	// 走 responses 的有效缓存根（honor TestMain override）——之前硬编码
	// os.UserCacheDir() 让 tool 测试清掉了真实用户缓存。
	dir, err := responses.KnowledgeCacheDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		_ = os.Remove(filepath.Join(dir, de.Name()))
	}
}
