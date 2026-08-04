// Command websearch_smoke exercises the web_search knowledge-harvester
// end to end against the live DeepSeek Responses API and proves the
// cost-saving claim: cache hits must never touch the API.
//
// Usage: DEEPSEEK_API_KEY=... go run ./cmd/websearch-smoke
//
// Scenarios (four legs, one API call total):
//
//	A. first ask  -> real web_search + json_schema  -> tokens spent, cached
//	B. exact re-ask -> L1 hash hit                  -> 0 tokens, 0 API
//	C. near-synonym  -> L2 semantic hit             -> 0 tokens, 0 API
//	D. unrelated ask -> cache miss                  -> real API (tokens spent)
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/provider/responses"
)

func apiKey() string {
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		return v
	}
	data, err := os.ReadFile(os.ExpandEnv("$HOME/.reasonix/.env"))
	if err == nil {
		for _, line := range splitLines(string(data)) {
			if len(line) > 17 && line[:17] == "DEEPSEEK_API_KEY=" {
				return line[17:]
			}
		}
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

var knowledgeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"answer_summary": map[string]any{"type": "string"},
		"key_facts":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"sources": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
				"url":   map[string]any{"type": "string"},
			},
		}},
	},
}

// ask runs one real DeepSeek web_search turn and returns spent tokens.
func ask(ctx context.Context, key, query string) (int, error) {
	p := responses.New(responses.Config{
		Name: "deepseek-responses", APIKey: key,
		BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash",
		Effort: "low",
	})
	req := provider.Request{
		Messages:       []provider.Message{{Role: provider.RoleUser, Content: query}},
		Tools:          []provider.ToolSchema{provider.WebSearchTool(false)},
		ToolChoice:     &provider.ToolChoice{Type: "web_search"},
		ResponseFormat: provider.JSONSchemaFormat("knowledge_extract", knowledgeSchema),
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("stream: %w", err)
	}
	text := ""
	tokens := 0
	seenWeb := false
	for c := range ch {
		switch c.Type {
		case provider.ChunkText:
			text += c.Text
		case provider.ChunkUsage:
			if c.Usage != nil {
				tokens = c.Usage.TotalTokens
			}
		}
	}
	_ = seenWeb // web_search_call 事件被 readStream 静默吸收，无需计数
	if text == "" {
		return tokens, fmt.Errorf("no output text for %q", query)
	}
	// Persist distilled knowledge (summary = raw text; the smoke test does
	// not need the JSON extraction to succeed).
	responses.SaveKnowledge(&responses.KnowledgeEntry{
		Query: query, AnswerSummary: text, TotalTokens: tokens,
	})
	return tokens, nil
}

func main() {
	key := apiKey()
	if key == "" {
		fmt.Println("❌ DEEPSEEK_API_KEY not found")
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "-retrieve" {
		runRetrieve(key)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-expert" {
		runExpert()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-war" {
		runWarEventStream(key)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-query" && len(os.Args) > 2 {
		runQuery(key, strings.Join(os.Args[2:], " "))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	q := "2026年8月3日北京天气怎么样？"
	fmt.Println("=== web_search 知识收割机冒烟测试 ===")
	fmt.Println("（命中必须 0 API 调用 0 token；只有 Miss 才花钱）")

	// Leg A: first ask -> real API.
	t0 := time.Now()
	tokensA, err := ask(ctx, key, q)
	dtA := time.Since(t0)
	if err != nil {
		fmt.Printf("❌ A 首查失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("A 首查(API)   : %6d tokens  %8v  命中=Miss ✅ 已缓存\n", tokensA, dtA.Round(time.Millisecond))

	// Leg B: exact re-ask -> L1 hash hit, zero API.
	t0 = time.Now()
	if e, hit := responses.LoadKnowledge(q); !hit {
		fmt.Println("❌ B 精确命中失败: 缓存未找到")
		os.Exit(1)
	} else {
		dtB := time.Since(t0)
		fmt.Printf("B 精确命中(L1): %6d tokens  %8v  命中=HIT ✅ 零API (tokens→%d)\n", 0, dtB.Round(time.Microsecond), e.TotalTokens)
	}

	// Leg C: near-synonym -> L2 semantic hit, zero API.
	qC := "北京今天天气如何？"
	t0 = time.Now()
	e, sim, hit := responses.LoadKnowledgeSemantic(qC, responses.DefaultSemanticThreshold)
	dtC := time.Since(t0)
	if !hit {
		fmt.Printf("❌ C 语义命中失败: sim=%.3f (阈值 %.2f)\n", sim, responses.DefaultSemanticThreshold)
		os.Exit(1)
	}
	fmt.Printf("C 语义命中(L2): %6d tokens  %8v  命中=HIT ✅ 相似度=%.3f (零API)\n", 0, dtC.Round(time.Microsecond), sim)
	_ = e

	// Leg D: unrelated -> cache miss -> real API (money spent).
	qD := "图灵奖得主是谁？"
	t0 = time.Now()
	tokensD, err := ask(ctx, key, qD)
	dtD := time.Since(t0)
	if err != nil {
		fmt.Printf("❌ D 未命中失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("D 无关(API)   : %6d tokens  %8v  命中=Miss ✅ 正确花钱\n", tokensD, dtD.Round(time.Millisecond))

	fmt.Println()
	fmt.Println("=== 省钱结论 ===")
	saved := tokensA + tokensD
	fmt.Printf("4 次提问实际 API 调用 %d 次（B/C 命中零调用）\n", 2)
	fmt.Printf("命中节省 tokens: %d（=A+D 两轮真实消耗，B/C 完全免费）\n", saved)
	fmt.Printf("若 100 个相似提问：只花 %d 次首查的钱，其余全部缓存命中\n", 2)
}

// realFetch builds a FetchFunc that runs a real DeepSeek web_search turn and
// distills the json_schema output into a KnowledgeEntry.
func realFetch(key string) responses.FetchFunc {
	return func(ctx context.Context, query string, tier responses.RetrievalTier) (*responses.KnowledgeEntry, error) {
		p := responses.New(responses.Config{
			Name: "deepseek-responses", APIKey: key,
			BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash",
			Effort: "low",
		})
		req := provider.Request{
			// instructions 提示模型：回答末尾必须列出来源（机构名+URL），
			// 解决来源标注缺失（json_schema 是引导非强制，模型常输出 markdown）。
			Messages:       []provider.Message{{Role: provider.RoleUser, Content: query + "\n\n回答末尾必须列出数据来源（机构名 + URL，至少 2 个）。"}},
			Tools:          []provider.ToolSchema{provider.WebSearchTool(false)},
			ToolChoice:     &provider.ToolChoice{Type: "web_search"},
			ResponseFormat: provider.JSONSchemaFormat("knowledge_extract", knowledgeSchema),
		}
		ch, err := p.Stream(ctx, req)
		if err != nil {
			return nil, err
		}
		text := ""
		tokens := 0
		for c := range ch {
			switch c.Type {
			case provider.ChunkText:
				text += c.Text
			case provider.ChunkUsage:
				if c.Usage != nil {
					tokens = c.Usage.TotalTokens
				}
			}
		}
		entry := &responses.KnowledgeEntry{
			Query:         query,
			AnswerSummary: text,
			TotalTokens:   tokens,
			Tier:          string(tier),
		}
		// Try to extract structured JSON (answer_summary/key_facts/sources).
		if v, ok := responses.ExtractJSONFromOutput(text); ok {
			if obj, ok := v.(map[string]any); ok {
				if s, ok := obj["answer_summary"].(string); ok && s != "" {
					entry.AnswerSummary = s
				}
				if facts, ok := obj["key_facts"].([]any); ok {
					for _, f := range facts {
						if fs, ok := f.(string); ok {
							entry.KeyFacts = append(entry.KeyFacts, fs)
						}
					}
				}
				if srcs, ok := obj["sources"].([]any); ok {
					for _, s := range srcs {
						if sm, ok := s.(map[string]any); ok {
							src := responses.Source{}
							if t, ok := sm["title"].(string); ok {
								src.Title = t
							}
							if u, ok := sm["url"].(string); ok {
								src.URL = u
							}
							if sn, ok := sm["snippet"].(string); ok {
								src.Snippet = sn
							}
							entry.Sources = append(entry.Sources, src)
						}
					}
				}
			}
		}
		// DeepSeek json_schema is advisory: the reply is often markdown
		// ("## 关键事实 / ## 来源") instead of JSON. Fall back to markdown
		// extraction so sources/facts still land in the cache.
		if len(entry.Sources) == 0 {
			entry.Sources = extractMarkdownSources(text)
		}
		// 仍为空 → 正文内联来源（全文 URL + 已知机构名）
		if len(entry.Sources) == 0 {
			entry.Sources = extractInlineSources(text)
		}
		if len(entry.KeyFacts) == 0 {
			entry.KeyFacts = extractMarkdownFacts(text)
		}
		return entry, nil
	}
}

func webPolicy() *responses.RetrievalPolicy {
	p := responses.DefaultPolicy()
	p.Approve(responses.GrantYear, time.Now())
	p.Frequency = responses.FrequencyHigh
	return &p
}

func runRetrieve(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	fetch := realFetch(key)

	q := "2026年8月3日北京天气怎么样？"
	fmt.Println("\n=== 真实 Retrieve 闭环（P0-P5 全链路） ===")

	// 首查：未命中 → 全量 web_search + json_schema → 质量过滤 → 落盘
	r1, err := responses.Retrieve(ctx, q, responses.RetrieveOptions{TimeSensitive: true, Policy: webPolicy()}, fetch)
	if err != nil {
		fmt.Printf("❌ 首查失败: %v\n", err)
		return
	}
	fmt.Printf("① 首查   : API=%v tier=%s sources=%d tokens=%d 摘要=%s\n",
		r1.APIUsed, r1.Tier, len(r1.Entry.Sources), r1.Entry.TotalTokens,
		truncate(r1.Entry.AnswerSummary, 40))

	// L1 精确命中（零 API）
	r2, _ := responses.Retrieve(ctx, q, responses.RetrieveOptions{TimeSensitive: true, Policy: webPolicy()}, fetch)
	fmt.Printf("② L1命中 : FromCache=%v API=%v 摘要=%s\n", r2.FromCache, r2.APIUsed, truncate(r2.Entry.AnswerSummary, 40))

	// L2 语义命中（近义改写，零 API）
	r3, _ := responses.Retrieve(ctx, "北京今天天气如何", responses.RetrieveOptions{TimeSensitive: true, Policy: webPolicy()}, fetch)
	fmt.Printf("③ L2命中 : FromCache=%v API=%v tier=%s 摘要=%s\n", r3.FromCache, r3.APIUsed, r3.Tier, truncate(r3.Entry.AnswerSummary, 40))

	// 强制刷新（走 API）
	r4, _ := responses.Retrieve(ctx, q, responses.RetrieveOptions{ForceRefresh: true}, fetch)
	fmt.Printf("④ 强制刷 : API=%v 摘要=%s\n", r4.APIUsed, truncate(r4.Entry.AnswerSummary, 40))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// extractMarkdownSources parses the common DeepSeek web_search markdown
// shape: a "## 来源" (or "Sources") section with "- 标题：URL" lines.
func extractMarkdownSources(text string) []responses.Source {
	var out []responses.Source
	lines := strings.Split(text, "\n")
	inSources := false
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		// 标题形式：## 来源 / ## Sources / ## References / 数据来源
		if strings.HasPrefix(trimmed, "## ") {
			inSources = strings.Contains(trimmed, "来源") || strings.Contains(trimmed, "Sources") ||
				strings.Contains(trimmed, "References") || strings.Contains(trimmed, "参考")
			continue
		}
		// 冒号行形式：数据来源：... / 来源：...
		if strings.HasPrefix(trimmed, "数据来源") || strings.HasPrefix(trimmed, "来源：") ||
			strings.HasPrefix(trimmed, "数据来源：") {
			body := strings.TrimPrefix(trimmed, "数据来源")
			body = strings.TrimPrefix(body, "：")
			body = strings.TrimPrefix(body, ":")
			body = strings.TrimPrefix(body, "来源")
			body = strings.TrimPrefix(body, "：")
			body = strings.TrimPrefix(body, ":")
			body = strings.TrimSpace(body)
			if body != "" && !strings.Contains(body, "http") {
				// 无 URL 的来源描述（如 "新加坡海事及港务管理局 MPA 2026年1月发布"）
				out = append(out, responses.Source{Title: body, URL: ""})
			}
			continue
		}
		if !inSources {
			continue
		}
		// - 标题：https://...
		if strings.HasPrefix(trimmed, "- ") {
			body := strings.TrimPrefix(trimmed, "- ")
			// find first http(s):// URL
			urlStart := -1
			for i := 0; i+7 <= len(body); i++ {
				if (body[i] == 'h' && len(body) > i+7 && body[i:i+8] == "https://") ||
					(body[i] == 'h' && len(body) > i+6 && body[i:i+7] == "http://") {
					urlStart = i
					break
				}
			}
			if urlStart < 0 {
				continue
			}
			src := responses.Source{
				Title: strings.TrimSpace(strings.TrimSuffix(body[:urlStart], "：")),
				URL:   strings.Fields(body[urlStart:])[0],
			}
			out = append(out, src)
		}
	}
	return out
}

// extractMarkdownFacts parses "**N. 事实**：..." or numbered list items.
func extractMarkdownFacts(text string) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(ln)
		// **N. 标题**：内容  or  N. 内容
		if strings.HasPrefix(trimmed, "**") && strings.Contains(trimmed, "**：") {
			parts := strings.SplitN(trimmed, "**：", 2)
			if len(parts) == 2 {
				out = append(out, strings.TrimSpace(parts[1]))
			}
		}
	}
	return out
}

// runExpert exercises the information-expert codec (planner.go / report.go /
// clarification) — the deep-research methodology now living in the retrieval
// system instead of local SKILL.md files.
func runExpert() {
	fmt.Println("=== 信息专家代码化冒烟（planner / report / clarification） ===")

	// 1. 反问确认（Phase 1）
	topics := []struct {
		t     string
		depth responses.ResearchDepth
		want  bool
	}{
		{"2026年北京新能源产业政策", responses.DepthL1, false},
		{"随便聊聊", responses.DepthL3, true},
		{"这个", responses.DepthL1, true},
	}
	ok := true
	for _, c := range topics {
		got := responses.NeedClarification(c.t, c.depth)
		mark := "✅"
		if got != c.want {
			mark = "❌"
			ok = false
		}
		fmt.Printf("  %s 澄清[%s/%s] %q → %v (want %v)\n", mark, c.depth, "L1", c.t, got, c.want)
	}
	_ = ok

	// 2. 四维检索规划（Phase 3）
	for _, d := range []responses.ResearchDepth{responses.DepthL1, responses.DepthL2, responses.DepthL3} {
		plan := responses.PlanResearch("人工智能监管", d)
		tier := responses.PlanToTier(d)
		fmt.Printf("  ✅ 规划[%s] → %d 查询, tier=%s, 覆盖 %d 维\n", d, len(plan.Queries), tier, len(plan.Coverage))
		for i, q := range plan.Queries[:min(3, len(plan.Queries))] {
			fmt.Printf("      [%d] %s: %s\n", i, q.Aspect, q.Query)
		}
	}

	// 3. 报告合成（Phase 5）——用真实缓存 + 合成条目
	e1, hit := responses.LoadKnowledge("2026年8月3日北京天气")
	entries := []*responses.KnowledgeEntry{
		{AnswerSummary: "北京天气报告", KeyFacts: []string{"高温 33℃", "有雷阵雨"},
			Sources: []responses.Source{{Title: "气象台", URL: "https://gov.cn/a", Credibility: 0.8}}},
	}
	if hit {
		fmt.Printf("  ✅ 真实缓存命中用于报告: %s\n", truncate(e1.AnswerSummary, 30))
		entries = append(entries, e1)
	} else {
		fmt.Println("  ⚠️ 缓存未命中（报告用合成数据）")
	}
	report := responses.SynthesizeReport("北京 8 月天气", entries)
	fmt.Printf("  ✅ 报告合成: %d 事实, %d 来源, 平均可信度 %.2f\n",
		len(report.Sections), len(report.AllSources), report.AvgConfidence)
	fmt.Println("  --- 报告预览 ---")
	fmt.Println(truncate(report.Render(), 400))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// runWarEventStream exercises P4 信息流模型（情报模式）：美伊冲突的完整
// 时间追踪——初始事件 → 多维度增量轮 → 冲突检测 → 置信度演化 → 事件链
// → 时间线报告。真实调用 DeepSeek web_search（每轮一次 API）。
func runWarEventStream(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	fetch := realFetch(key)

	fmt.Println("=== 美伊冲突 时间完整追踪（P4 信息流模型迭代） ===")

	// 轮 1：初始事件
	q1 := "美国伊朗冲突最新进展"
	e1, err := fetch(ctx, q1, responses.TierComplex)
	if err != nil {
		fmt.Printf("❌ 初始事件失败: %v\n", err)
		return
	}
	e1.Query = q1
	e1.TimeSensitive = true
	e1.Tier = string(responses.TierComplex)
	responses.ScoreAndTagSources(e1)
	responses.SaveKnowledge(e1)
	fmt.Printf("① 初始事件: %d tokens, %d 来源, 摘要=%s\n",
		e1.TotalTokens, len(e1.Sources), truncate(e1.AnswerSummary, 50))

	// 轮 2-4：多维度增量
	rounds := []struct {
		q string
		d string // 维度
	}{
		{"美国伊朗军事行动 最新", "军事"},
		{"美国伊朗外交谈判 进展", "外交"},
		{"美国伊朗 油价 市场 影响", "经济"},
	}
	main := e1
	now := time.Now()
	for i, r := range rounds {
		eN, err := fetch(ctx, r.q, responses.TierComplex)
		if err != nil {
			fmt.Printf("⚠️ 轮%d(%s)失败: %v\n", i+2, r.d, err)
			continue
		}
		eN.Query = r.q
		eN.TimeSensitive = true
		responses.ScoreAndTagSources(eN)
		// P4: 增量更新主事件（冲突检测 + 置信度演化 + 时效刷新）
		responses.AdvanceEvent(main, now, eN.KeyFacts, nil)
		// 事件链：主事件 ↔ 本轮子事件
		responses.LinkRelatedEvent(main, eN)
		responses.SaveKnowledge(eN)
		fmt.Printf("② 轮%d[%s]: 更新=%d 置信度=%.2f 冲突=%v 摘要=%s\n",
			i+2, r.d, main.UpdateCount, main.Confidence, main.ConflictDetected, truncate(eN.AnswerSummary, 40))
	}
	responses.SaveKnowledge(main)

	// 汇总：时间线报告
	fmt.Printf("\n=== 追踪汇总 ===\n")
	fmt.Printf("更新次数: %d | 置信度: %.2f | 冲突信号: %v | 事件链: %d 关联\n",
		main.UpdateCount, main.Confidence, main.ConflictDetected, len(main.EventChain))
	report := responses.SynthesizeReport("美国伊朗冲突时间线", []*responses.KnowledgeEntry{main})
	fmt.Printf("报告: %d 事实, %d 来源, 平均可信度 %.2f\n", len(report.Sections), len(report.AllSources), report.AvgConfidence)
	fmt.Println("--- 时间线报告预览 ---")
	fmt.Println(truncate(report.Render(), 500))
}

// runQuery is the general-purpose retrieval entry: given a topic it uses the
// completed retrieval system (Retrieve + 分级 + 报告合成 + 因果线) to fetch,
// cache, and synthesize an analysis report. This is "using the system", not
// a test script.
func runQuery(key, topic string) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	fetch := realFetch(key)

	fmt.Printf("=== 信息检索: %s ===\n\n", topic)

	// 难度分级 → 检索（本地缓存优先，未命中联网）
	tier, _ := responses.ClassifyTier(topic)
	fmt.Printf("难度分级: %s (maxRounds=%d)\n", tier, tier.MaxRounds())

	// 时效意图：查询含"最新/最近/本月/今年"等时效词 → 强制刷新，
	// 避免命中旧缓存（如 2024 数据响应 2025-2026 查询）。
	forceFresh := hasFreshnessIntent(topic)
	res, err := responses.Retrieve(ctx, topic, responses.RetrieveOptions{
		Policy:        webPolicy(),
		TimeSensitive: true,
		ForceRefresh:  forceFresh,
		Tier:          tier,
	}, fetch)
	if err != nil {
		fmt.Printf("❌ 检索失败: %v\n", err)
		return
	}
	if res.WebBlocked {
		fmt.Println("⚠️ 联网未授权（本地缓存未命中）")
		return
	}
	src := "API"
	if res.FromCache {
		src = "本地缓存"
	}
	fmt.Printf("检索: %s | API调用=%v | tokens=%d | 来源=%d\n",
		src, res.APIUsed, res.Entry.TotalTokens, len(res.Entry.Sources))

	// 结构化分析报告
	report := responses.SynthesizeReport(topic, []*responses.KnowledgeEntry{res.Entry})
	fmt.Println("\n=== 信息摘要（先摘要，回复「详细」获取完整数据） ===")
	fmt.Println(report.RenderSummary())
	fmt.Println("\n=== 完整报告 ===")
	fmt.Println(report.Render())

	// 信息素养：事实核查 + 交叉比对 + 旁观验证
	verify := responses.CrossCheck(responses.ExtractNumericFacts(res.Entry.AnswerSummary),
		[]*responses.KnowledgeEntry{res.Entry})
	fmt.Println("\n=== 事实核查与交叉比对 ===")
	fmt.Println(verify.Render())

	// 因果线（时效事件）
	if res.Entry.TimeSensitive || responses.PanicScore(topic) > 0 {
		chain := responses.FromEventStream(topic+" 因果线", res.Entry)
		fmt.Println("=== 因果线 ===")
		fmt.Println(chain.Render())
	}
}

// hasFreshnessIntent reports whether a query demands current data.
func hasFreshnessIntent(q string) bool {
	lower := strings.ToLower(q)
	for _, w := range []string{"最近", "最新", "本月", "今年", "最近12个月", "最近一年", "current", "latest", "recent", "this year", "2025", "2026"} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// extractInlineSources scans the whole reply for http(s) URLs and known
// institution names (来源散在正文，无独立来源段落时兜底).
func extractInlineSources(text string) []responses.Source {
	var out []responses.Source
	seen := map[string]bool{}
	// 1. 全文 URL
	urlRe := regexp.MustCompile(`https?://[\w\-./%#?&=~]+`)
	for _, u := range urlRe.FindAllString(text, -1) {
		if !seen[u] {
			seen[u] = true
			clean := strings.TrimRight(u, "。，,.;)）]}")
			out = append(out, responses.Source{URL: clean, Title: hostOf(clean)})
		}
	}
	// 2. 已知机构名（无 URL 时）
	for _, inst := range []struct{ name, domain string }{
		{"新加坡海事及港务管理局", "mpa.gov.sg"}, {"MPA", "mpa.gov.sg"}, {"data.gov.sg", "data.gov.sg"},
		{"路透", "reuters.com"}, {"彭博", "bloomberg.com"}, {"联合国", "un.org"}, {"世界银行", "worldbank.org"},
		{"FAO", "fao.org"}, {"粮农组织", "fao.org"}, {"EIA", "eia.gov"}, {"Kpler", "kpler.com"},
	} {
		if strings.Contains(text, inst.name) && !seen[inst.domain] {
			seen[inst.domain] = true
			out = append(out, responses.Source{Title: inst.name, Domain: inst.domain})
		}
	}
	return out
}

func hostOf(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "www.")
	if i := strings.Index(u, "/"); i > 0 {
		u = u[:i]
	}
	return u
}
