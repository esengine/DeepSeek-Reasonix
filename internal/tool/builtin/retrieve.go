package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/provider/responses"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(retrieveInfo{}) }

// retrieveInfo exposes the knowledge-cache lookup as a model-visible tool so
// conversational auto-retrieval (2026-08-03 design) works: the model can
// consult previously distilled web_search results with zero cost and zero
// network. On a cache miss it reuses the system's deepseek-responses
// provider pipeline (the API key the user already configured for the app) —
// no separate key is required. Web fetch is gated by the session grant +
// cooldown policy; without a configured deepseek-responses provider it
// returns a needs_grant notice instead (§9 no-silent-web guardrail).
type retrieveInfo struct{}

func (retrieveInfo) Name() string { return "retrieve_info" }

func (retrieveInfo) Description() string {
	return "查询本地知识缓存（此前 web_search 蒸馏并落盘的检索结果）。零成本、不联网。命中返回缓存摘要与来源；未命中且系统已配置 deepseek-responses 时自动走该管道联网检索并落盘（复用系统 API 凭据，无需单独提供）；未配置时返回 needs_grant 标志，此时应使用 ask 工具询问用户是否允许联网检索。避免重复联网查询已检索过的事实。**返回的来源（Sources）含 URL——如需对单一来源深度阅读，可对 URL 调用 web_fetch 工具接续抓取原文。**"
}

func (retrieveInfo) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "query":{"type":"string","description":"要查询的问题（与之前的检索问题相同或语义相近）"},
  "budget_override":{"type":"boolean","description":"人工确认：小时预算超限后用户明确要继续检索（费用由用户确认），默认 false"},
  "hourly_budget":{"type":"integer","description":"本次调用的小时预算覆盖（默认 30 次/小时；调高需谨慎——付费按次计量）"}
},
"required":["query"]
}`)
}

func (retrieveInfo) ReadOnly() bool { return true }

func (retrieveInfo) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 60, Tail: 10, HeadChars: 6000, TailChars: 1500}
}

// Execute runs a policy-gated lookup. The zero policy (local cache only, no
// web) is the safe default: this tool never triggers a network fetch. A
// blocked notice tells the model the answer needs a web refresh instead of
// silently going online.
func (retrieveInfo) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
		// BudgetOverride 人工确认：预算超限时用户明确要继续检索（付费
		// 责任在用户），不受小时配额限制。
		BudgetOverride bool `json:"budget_override"`
		// HourlyBudget 本次调用的小时预算覆盖（默认 30；调高请谨慎——
		// 付费按次计量，预算只是保险丝）。
		HourlyBudget int `json:"hourly_budget"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("retrieve_info: parse args: %w", err)
	}
	p.Query = strings.TrimSpace(p.Query)
	if p.Query == "" {
		return "", fmt.Errorf("retrieve_info: query is required")
	}

	pol := budgetPolicy(p.HourlyBudget)
	if exceeded, used, limit := pol.BudgetExceeded(time.Now()); exceeded && !p.BudgetOverride {
		return fmt.Sprintf("本地知识缓存未命中；付费联网检索已达小时预算上限（%d/%d 次）。如需继续（费用由你确认），请携带 budget_override=true 重试。", used, limit), nil
	}
	res, err := responses.Retrieve(ctx, p.Query, responses.RetrieveOptions{
		Policy:    pol,
		PanicMode: true, // #13 破壁引导（中立护栏，仅当前问题文本，无用户画像）
	}, retrieveFetch)
	if err == nil && res.Entry != nil {
		// 动态冷却反馈：L1/L2 命中（无联网）→ hitRate 高 → 冷却拉长；
		// 真检索（APIUsed）→ hitRate 低 → 冷却缩短（下一轮更快放行）。
		hitRate := 1.0
		if res.APIUsed {
			hitRate = 0.2
		}
		pol.ApplyDynamic(hitRate, 0.5)
	}
	if err != nil {
		// 未配置 deepseek-responses / 凭据不可用 → 授权提示而非报错；
		// 其余管道错误原样返回。
		if errors.Is(err, errNoResponsesProvider) {
			return blockedNotice(), nil
		}
		return "", err
	}

	if res.Entry == nil {
		if res.WebBlocked {
			// 已授权但冷却中：提示剩余等待（RemainingCooldown 接线）；
			// 未授权：授权引导。
			if pol.IsGranted(time.Now()) {
				wait := pol.RemainingCooldown(time.Now())
				if wait > 0 {
					return fmt.Sprintf("本地知识缓存未命中，联网检索冷却中（约 %s 后可用）。", wait.Round(time.Minute)), nil
				}
			}
			return blockedNotice(), nil
		}
		return "本地知识缓存未命中。", nil
	}

	if res.StaleServed {
		return "⚠️ " + p.Query + "\n\n（缓存信息可能过期，标注见下文）\n" + res.Entry.AnswerSummary, nil
	}

	var b strings.Builder
	if res.FromCache {
		b.WriteString("【本地知识缓存命中】\n\n")
	} else {
		b.WriteString("【联网检索完成（deepseek-responses 管道）】\n\n")
	}
	b.WriteString(res.Entry.AnswerSummary)
	if len(res.Entry.KeyFacts) > 0 {
		b.WriteString("\n\n关键事实：\n")
		for _, f := range res.Entry.KeyFacts {
			b.WriteString("- " + f + "\n")
		}
	}
	if len(res.Entry.Sources) > 0 {
		b.WriteString("\n来源：\n")
		for _, s := range res.Entry.Sources {
			line := "- " + s.Title
			if s.URL != "" {
				line += " (" + s.URL + ")"
			}
			b.WriteString(line + "\n")
		}
	}
	if res.Entry.TimeSensitive && res.Entry.FreshUntil.After(res.Entry.CreatedAt) {
		fmt.Fprintf(&b, "\n（时效信息，截至 %s，如需最新请联网刷新）", res.Entry.FreshUntil.Format("2006-01-02 15:04"))
	}
	return b.String(), nil
}

func blockedNotice() string {
	return `{"needs_grant":true,"reason":"local cache miss and no deepseek-responses provider configured; web fetch requires user grant","options":["year"],"message":"本地知识缓存未命中，且系统未配置 deepseek-responses 供应商。请使用 ask 工具询问用户：允许联网检索吗？确认后长期授权（1 年），拒绝则不联网。"}`
}

// errNoResponsesProvider marks a missing/unresolvable deepseek-responses
// provider — the tool reports needs_grant instead of a hard error.
var errNoResponsesProvider = errors.New("deepseek-responses provider not configured")

// retrievePolicy is the process-wide retrieval policy: configured
// deepseek-responses implies session-level web authorization (pipeline reuse,
// no separate key), granted for one year with a medium frequency cooldown.
// Process-lifetime state makes the cooldown actually gate repeated calls
// (a per-Execute policy would reset it every turn — review finding).
var retrievePolicyOnce sync.Once
var retrievePol responses.RetrievalPolicy

const defaultHourlyBudget = 30 // 默认：30 次付费 web_search/小时（保险丝，非收费阀）

// retrievePolicy returns the process-wide retrieval policy. The hourly
// budget defaults to defaultHourlyBudget but a per-call override (tool param
// hourly_budget) raises/lowers it for that request without touching the
// shared policy.
func retrievePolicy() *responses.RetrievalPolicy {
	retrievePolicyOnce.Do(func() {
		retrievePol = responses.DefaultPolicy()
		// 预算上限机制（用户 2026-08-04）：1 小时 N 次付费 web_search——
		// 防 AI 循环触发付费导致破产；人工确认（budget_override）不受限。
		// 冷却仅轻节流（medium 5min），不阻碍人工工作（30min 动态值移除）。
		retrievePol.Frequency = responses.FrequencyMedium
		retrievePol.HourlyBudget = defaultHourlyBudget
		retrievePol.Approve(responses.GrantYear, time.Now())
	})
	return &retrievePol
}

// budgetPolicy returns a policy honoring a per-call budget override: a COPY
// of the shared process policy with an adjusted HourlyBudget (bounded to
// [1, 1000] to stay a fuse, not a valve). Copying — never mutating the shared
// policy — keeps per-call overrides non-sticky and avoids a data race when
// retrieve_info runs concurrently (review finding).
func budgetPolicy(override int) *responses.RetrievalPolicy {
	base := retrievePolicy()
	if override <= 0 {
		return base // 无覆盖：共享策略本身只读使用
	}
	cp := *base
	if override > 1000 {
		override = 1000
	}
	cp.HourlyBudget = override
	return &cp
}

// systemFetchTestHook lets tests replace the real network pipeline. The zero
// value uses systemFetch (real deepseek-responses pipeline).
var systemFetchTestHook responses.FetchFunc

// retrieveFetch is the fetch indirection used by Execute: tests inject a
// fake via systemFetchTestHook, production runs the system pipeline.
func retrieveFetch(ctx context.Context, query string, tier responses.RetrievalTier) (*responses.KnowledgeEntry, error) {
	if systemFetchTestHook != nil {
		return systemFetchTestHook(ctx, query, tier)
	}
	return systemFetch(ctx, query, tier)
}

// systemFetch performs one real web_search retrieval through the system's
// deepseek-responses provider pipeline — the same API key / endpoint the app
// already uses for chat, so the tool needs no separate credentials. Returns
// a distilled KnowledgeEntry (JSON extraction with markdown fallback).
func systemFetch(ctx context.Context, query string, tier responses.RetrievalTier) (*responses.KnowledgeEntry, error) {
	entry := responsesEntry()
	if entry == nil {
		return nil, errNoResponsesProvider
	}
	key := entry.APIKey()
	if key == "" {
		return nil, errNoResponsesProvider
	}
	p := responses.New(responses.Config{
		Name: entry.Name, APIKey: key,
		BaseURL: entry.BaseURL, Model: entry.Model,
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
		return nil, fmt.Errorf("retrieve_info: web_search stream: %w", err)
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
		case provider.ChunkError:
			// 流中途失败：返回真实错误（不得把部分/无效文本当结果蒸馏
			// 落盘——review 发现：忽略 ChunkError 会让调用方拿到
			// "no text" 并可能缓存无效蒸馏）。
			if err := c.Err; err != nil {
				return nil, fmt.Errorf("retrieve_info: web_search stream failed: %w", err)
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("retrieve_info: web_search returned no text for %q", query)
	}
	return responses.DistillEntry(query, text, tokens, string(tier)), nil
}

// responsesEntry finds the system's deepseek-responses provider entry
// (kind="responses" on api.deepseek.com). LoadForRootReadOnly never writes
// config files.
func responsesEntry() *config.ProviderEntry {
	cfg, err := config.LoadForRootReadOnly("")
	if err != nil {
		return nil
	}
	for i := range cfg.Providers {
		e := &cfg.Providers[i]
		if e.Kind == "responses" && strings.Contains(e.BaseURL, "api.deepseek.com") {
			// 手动添加的 entry 常只有 models 列表、无顶层 model（如
			// deepseek-responses preset #7103）：取第一个模型兜底。
			if e.Model == "" && len(e.Models) > 0 {
				e.Model = e.Models[0]
			}
			if e.Model == "" {
				e.Model = "deepseek-v4-flash"
			}
			return e
		}
	}
	return nil
}

// knowledgeSchema instructs the model to return structured knowledge; the
// extraction is advisory (DeepSeek often replies markdown — DistillEntry
// falls back to markdown source/fact extraction).
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
