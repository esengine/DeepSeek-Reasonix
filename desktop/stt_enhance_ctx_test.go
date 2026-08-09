package main

import (
	"strings"
	"testing"
)

func TestRecentConversationContext(t *testing.T) {
	hist := []HistoryMessage{
		{Role: "user", Content: "帮我修一下登录页"},     // 0 最早
		{Role: "assistant", Content: "已定位：token 过期未刷新"}, // 1
		{Role: "tool", Content: "{\"ok\":true}"},        // 2 工具轮跳过
		{Role: "user", Content: "现在要支持邮箱登录"},       // 3 最新
	}
	got := recentConversationContext(hist, 6)
	if !strings.Contains(got, "用户：帮我修一下登录") {
		t.Fatalf("缺少最早用户轮: %q", got)
	}
	if !strings.Contains(got, "助手：已定位") {
		t.Fatalf("缺少助手轮: %q", got)
	}
	if !strings.Contains(got, "用户：现在要支持邮箱登录") {
		t.Fatalf("缺少最新用户轮: %q", got)
	}
	if strings.Contains(got, "tool") || strings.Contains(got, "{\"ok\"") {
		t.Fatalf("工具消息被注入: %q", got)
	}
	// 时间顺序：最早在前，最新在后
	idxFirst := strings.Index(got, "帮我修一下登录")
	idxLast := strings.Index(got, "邮箱登录")
	if idxFirst == -1 || idxLast == -1 || idxFirst > idxLast {
		t.Fatalf("时间顺序错误: %q", got)
	}
}

func TestRecentConversationContextLimitAndEmpty(t *testing.T) {
	if got := recentConversationContext(nil, 6); got != "" {
		t.Fatalf("空历史应返回空串: %q", got)
	}
	hist := []HistoryMessage{{Role: "user", Content: "a"}, {Role: "user", Content: "b"}, {Role: "user", Content: "c"}}
	if got := recentConversationContext(hist, 2); strings.Count(got, "用户：") != 2 {
		t.Fatalf("应只保留最近 2 轮: %q", got)
	}
	onlyTools := []HistoryMessage{{Role: "tool", Content: "x"}}
	if got := recentConversationContext(onlyTools, 6); got != "" {
		t.Fatalf("全工具轮应返回空: %q", got)
	}
}
