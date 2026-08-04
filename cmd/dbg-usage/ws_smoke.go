//go:build ignore

// 检索系统冒烟：授权 1 年 + 缓存 + 真实 web_search 管道
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/provider/responses"
)

func main() {
	dir, _ := os.MkdirTemp("", "ws-smoke-*")
	defer os.RemoveAll(dir)
	responses.SetKnowledgeDirOverride(dir)

	// 1. 授权（1 年档）
	p := responses.DefaultPolicy()
	p.Approve(responses.GrantYear, time.Now())
	if !p.IsGranted(time.Now()) || p.IsGranted(time.Now().Add(366*24*time.Hour)) {
		fmt.Println("FAIL: 授权语义")
		os.Exit(1)
	}
	fmt.Println("[1] 授权 1 年档 ✅")

	// 2. 缓存读写（L1）
	q := "今日北京天气"
	responses.SaveKnowledge(&responses.KnowledgeEntry{Query: q, AnswerSummary: "晴 25°C", Language: "zh"})
	e, hit := responses.LoadKnowledge(q)
	if !hit || e.AnswerSummary != "晴 25°C" {
		fmt.Println("FAIL: 缓存")
		os.Exit(1)
	}
	fmt.Println("[2] 缓存 L1 ✅")

	// 3. 变体学习（L2 命中 → L1 变体）
	responses.SaveKnowledge(&responses.KnowledgeEntry{Query: "北京今天天气", AnswerSummary: "晴 25°C", Language: "zh"})
	if _, ok := responses.LoadKnowledge("今日北京天气"); !ok {
		fmt.Println("FAIL: 缓存同义")
		os.Exit(1)
	}
	fmt.Println("[3] 变体/同义 ✅")

	// 4. 真实管道（deepseek-responses web_search）
	cfg, err := config.LoadForRootReadOnly("")
	if err != nil { fmt.Println("config err:", err); os.Exit(1) }
	var baseURL, apiKey, model string
	for _, pe := range cfg.Providers {
		if pe.Kind == "responses" && contains(pe.BaseURL, "api.deepseek.com") {
			baseURL, apiKey = pe.BaseURL, pe.APIKey()
			model = pe.Model
			if model == "" && len(pe.Models) > 0 { model = pe.Models[0] }
			break
		}
	}
	if baseURL == "" || apiKey == "" { fmt.Println("SKIP: 无 deepseek-responses"); return }
	c := responses.New(responses.Config{Name: "smoke", APIKey: apiKey, BaseURL: baseURL, Model: model, Effort: "low", WebSearch: true})
	req := provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "今日北京天气"}},
	}
	ch, err := c.Stream(context.Background(), req)
	if err != nil { fmt.Println("FAIL stream:", err); os.Exit(1) }
	text := ""
	for ck := range ch {
		if ck.Type == provider.ChunkText { text += ck.Text }
	}
	if len(text) < 20 { fmt.Println("FAIL: 真实管道无输出"); os.Exit(1) }
	fmt.Printf("[4] 真实 web_search ✅（%d 字）\n", len(text))
	fmt.Println("SMOKE ALL PASS")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ { if s[i:i+len(sub)] == sub { return true } }
	return false
}
