package responses

import (
	"context"
	"math/rand"
	"strings"
	"time"
)

// sanitizeFresh runs gate-3 quality + manipulation screening on a fetched
// entry. It returns false when the content is manipulated (panic/marketing)
// and must NOT be persisted — callers still serve it live, it just never
// poisons the cache. Both the miss path and the stale-refresh path must call
// this (refresh used to skip it, letting polluted content into the cache).
func sanitizeFresh(e *KnowledgeEntry, minCredibility float64) bool {
	if e == nil {
		return false
	}
	ScoreAndTagSources(e)
	e.Sources = FilterSources(e.Sources, minCredibility)
	manip := emotionHits(e.AnswerSummary) + marketingHits(e.AnswerSummary)
	for _, f := range e.KeyFacts {
		manip += emotionHits(f) + marketingHits(f)
	}
	return manip == 0
}

// purgeStaleLabel removes the "信息截至…" marker before a refresh merge so a
// refreshed entry doesn't accumulate stale labels from prior rounds.
func purgeStaleLabel(s string) string {
	if i := strings.Index(s, "\n\n⚠️ 信息截至 "); i >= 0 {
		return s[:i]
	}
	return s
}

// applyFresh merges a sanitized fetch into an existing entry (refresh path),
// then persists. Shared with the miss path so both enforce the same quality
// gate. Returns true only when the refresh actually merged (manipulated or
// nil content returns false so callers don't over-report Refreshed).
func applyFresh(dst, src *KnowledgeEntry, now time.Time, minCred float64) bool {
	if src == nil {
		return false
	}
	// 质量/操纵检查：不达标不合并进缓存。
	if !sanitizeFresh(src, minCred) {
		return false
	}
	dst.AnswerSummary = purgeStaleLabel(dst.AnswerSummary)
	advanceEvent(dst, now, src.KeyFacts, nil)
	mergeEntry(dst, src)
	SaveKnowledge(dst)
	return true
}

// P5: agent 闭环检索协调器。把 P1-P4 串成完整流程：
//
//	门控0 本地命中（L1 精确 / L2 语义）
//	  ├─ 命中且新鲜（!NeedsRefresh）→ 直接返回，零 API
//	  ├─ 命中但时效过期 → 返回 stale 缓存 + 触发增量刷新（advanceEvent）
//	  └─ 未命中 → ClassifyTier 分级 → 全量检索
//	门控3 质量过滤（ScoreAndTagSources / FilterSources）
//	落盘（SaveKnowledge）→ 下次门控0 命中
//
// API 调用抽象为 FetchFunc，协调器本身零网络依赖（可单测）。

// refreshErrLabel extracts a short human-readable cause from a refresh error.
func refreshErrLabel(err error) string {
	if err == nil {
		return "未知原因"
	}
	msg := err.Error()
	if len(msg) > 60 {
		msg = msg[:57] + "..."
	}
	return msg
}

// RetrieveOptions controls one retrieval pass.
type RetrieveOptions struct {
	// TimeSensitive marks the query as time-critical (news/markets/events):
	// cached hits expire after FreshUntil and trigger incremental refresh.
	TimeSensitive bool
	// ForceRefresh skips the cache entirely and re-fetches (e.g. user asked
	// for the very latest, or a previous refresh was explicitly requested).
	ForceRefresh bool
	// MinCredibility is the gate-3 cutoff for kept sources; <=0 uses the
	// 0.5 default.
	MinCredibility float64
	// Tier overrides heuristic classification; empty uses ClassifyTier.
	Tier RetrievalTier
	// BypassProbability (0..1) is the defense-layer-3 anti-echo-chamber knob:
	// on a L2 semantic hit whose similarity is below 0.95, the cache is
	// bypassed with this probability and a live fetch refreshes the entry
	// (incremental update, so the answer stays current instead of being
	// repeatedly served from the same distilled snapshot). 0 disables
	// bypass (default, cost-first); 1 always refreshes near-synonyms.
	BypassProbability float64
	// PanicMode enables the #13 破壁引导 (wall-breaking reassurance): when
	// the query carries a disaster+time-urgency signal (PanicScore > 0),
	// the answer is NOT withheld — an authority-grounded reassurance line is
	// appended to AnswerSummary before caching. This is an explicit,
	// user-visible opt-in policy choice; it never inspects user history.
	PanicMode bool
	// Policy gates conversational auto-retrieval (2026-08-03 design):
	// local-cache hits are always free; web fetch requires per-session
	// grant (WebSearch) + frequency tier cooldown. Nil policy = safe
	// default (local only, no web). Stale entries are labeled (信息截至…)
	// instead of silently refreshed.
	Policy *RetrievalPolicy
	// Now overrides the clock for tests (nil = time.Now).
	Now func() time.Time
}

// bypassThreshold is the L2 similarity above which a hit is treated as
// effectively identical (always served from cache regardless of the bypass
// probability — re-fetching an identical query wastes money for no signal).
const bypassThreshold = 0.95

// FetchFunc performs one real retrieval for query (web_search + json_schema
// extraction) and returns the distilled entry. tier is the classification the
// executor may use to pick round count.
type FetchFunc func(ctx context.Context, query string, tier RetrievalTier) (*KnowledgeEntry, error)

// RetrieveResult reports how a pass resolved.
type RetrieveResult struct {
	Entry       *KnowledgeEntry // final answer (cache or fresh)
	FromCache   bool            // served from L1/L2 without API
	StaleServed bool            // stale cache served while refresh ran
	Bypassed    bool            // defense-layer-3 probabilistic bypass fired
	Refreshed   bool            // incremental refresh applied
	WebBlocked  bool            // web fetch denied by policy (no grant / cooldown)
	Tier        RetrievalTier   // difficulty used (heuristic or override)
	APIUsed     bool            // any fetch was invoked
}

// Retrieve runs the closed loop. fetch must be non-nil; a nil ctx uses
// context.Background.
func Retrieve(ctx context.Context, query string, opts RetrieveOptions, fetch FetchFunc) (*RetrieveResult, error) {
	if fetch == nil {
		return nil, errFetchFuncRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	if opts.Now != nil {
		now = opts.Now()
	}
	res := &RetrieveResult{}

	// webAllowed decides whether an automatic (non-ForceRefresh) web fetch may
	// run: explicit per-session grant + frequency cooldown. ForceRefresh is a
	// user-initiated action and counts as its own authorization.
	webAllowed := opts.ForceRefresh || (opts.Policy != nil && opts.Policy.CanWebSearch(now))

	// staleLabel marks expired cached data instead of silently refreshing it
	// (gate D): the user sees "信息截至 …" and must authorize a refresh.
	staleLabel := func(e *KnowledgeEntry) string {
		t := e.LastUpdatedAt
		if t.IsZero() {
			t = e.CreatedAt
		}
		return t.Format("2006-01-02 15:04")
	}

	// 门控 0：本地命中（L1 精确，L2 语义兜底）
	if !opts.ForceRefresh {
		if e, hit := LoadKnowledge(query); hit {
			res.Entry = e
			res.FromCache = true
			res.Tier = tierOf(e)
			if e.NeedsRefresh(now) {
				// 时效过期：serve stale + 标注；联网刷新需授权+冷却。
				res.StaleServed = true
				e.AnswerSummary += "\n\n⚠️ 信息截至 " + staleLabel(e) + "，如需最新动态请允许联网刷新。"
				if webAllowed {
					fresh, err := fetch(ctx, query, tierOf(e))
					if err != nil {
						// 刷新失败：仍服务 stale 内容（含"信息截至"标注），
						// 附刷新失败提示——不丢用户可见信息换裸错误。
						e.AnswerSummary += "\n\n⚠️ 联网刷新失败（" + refreshErrLabel(err) + "），以上为缓存内容。"
						return res, nil
					}
					if applyFresh(e, fresh, now, opts.MinCredibility) {
						res.Refreshed = true
						res.APIUsed = true
						if opts.Policy != nil {
							opts.Policy.MarkWebUsed(now)
						}
					}
				} else {
					res.WebBlocked = true
				}
			}
			return res, nil
		}
		// 概念别名索引：短查询（"霍奇"）直接解析到模块条目（L1 命中，
		// 零扫描零联网）——修复短查询对长 Query 条目的 L2 召回不足。
		// 时效查询跳过（用户要"黎曼 2026 最新"，不是本地快照）。
		if !HasFreshnessIntent(query) {
			if mod, ok := ResolveConcept(query); ok {
				if e, hit := LoadKnowledge(mod); hit {
					res.Entry = e
					res.FromCache = true
					res.Tier = tierOf(e)
					return res, nil
				}
			}
		}
		if e, sim, hit := LoadKnowledgeSemantic(query, SemanticThresholdFor(query)); hit {
			res.Entry = e
			res.FromCache = true
			res.Tier = tierOf(e)
			stale := e.NeedsRefresh(now)
			// Defense layer 3 (anti-echo-chamber): a near-synonym hit below
			// the identity threshold is bypassed with the configured
			// probability so the cache is refreshed by live data instead of
			// repeatedly serving the same distilled snapshot.
			bypass := !stale && !opts.ForceRefresh &&
				opts.BypassProbability > 0 &&
				sim < bypassThreshold &&
				rand.Float64() < opts.BypassProbability
			if stale || bypass {
				res.StaleServed = stale
				res.Bypassed = bypass
				if stale {
					e.AnswerSummary += "\n\n⚠️ 信息截至 " + staleLabel(e) + "，如需最新动态请允许联网刷新。"
				}
				if !webAllowed {
					// 自动刷新（stale 或 bypass）被 policy 门控拒绝：
					// 返回当前缓存 + 标注，绝不静默联网。
					res.WebBlocked = true
					return res, nil
				}
				fresh, err := fetch(ctx, query, tierOf(e))
				if err != nil {
					e.AnswerSummary += "\n\n⚠️ 联网刷新失败（" + refreshErrLabel(err) + "），以上为缓存内容。"
					return res, nil
				}
				if applyFresh(e, fresh, now, opts.MinCredibility) {
					res.Refreshed = true
					res.APIUsed = true
				}
			}
			return res, nil
		}
	}

	// 未命中：无联网授权（含频率档被关）→ 不能自动发起 web fetch。
	// 与刷新路径一致用 CanWebSearch（含 FrequencyOff 检查），ForceRefresh
	// 是用户主动动作例外。
	if !opts.ForceRefresh && (opts.Policy == nil || !opts.Policy.CanWebSearch(now)) {
		res.WebBlocked = true
		return res, nil
	}

	// 未命中 / 强制刷新：分级 → 全量检索 → 质量过滤 → 落盘
	tier := opts.Tier
	if tier == "" {
		tier, _ = ClassifyTier(query)
	}
	res.Tier = tier

	fresh, err := fetch(ctx, query, tier)
	if err != nil {
		return res, err
	}
	if fresh == nil {
		return res, errEmptyFetchResult
	}
	if fresh.Query == "" {
		fresh.Query = query
	}
	if !fresh.TimeSensitive && opts.TimeSensitive {
		fresh.TimeSensitive = true
	}
	if fresh.Tier == "" {
		fresh.Tier = string(tier)
	}
	ScoreAndTagSources(fresh)
	fresh.Sources = FilterSources(fresh.Sources, opts.MinCredibility)
	// Defense layer 2, check 1: engineered panic/marketing content must not
	// be persisted (cache would "fix" pollution and amplify it on later
	// L1/L2 hits). Score the distilled answer BEFORE appending the
	// reassurance line so the guide itself never trips the gate.
	manip := emotionHits(fresh.AnswerSummary) + marketingHits(fresh.AnswerSummary)
	for _, f := range fresh.KeyFacts {
		manip += emotionHits(f) + marketingHits(f)
	}
	if manip > 0 {
		// Do not cache manipulated content; the caller still receives the
		// live answer, it just never poisons the cache.
		res.Entry = fresh
		res.APIUsed = true
		return res, nil
	}
	// 破壁引导 (#13): 显式开启时，对灾难+时间紧迫查询追加权威核实安抚行。
	// 不拦截、不训斥——只补充一句有温度的事实核查。
	if opts.PanicMode && PanicScore(query) > 0 {
		fresh.AnswerSummary += "\n\n" + panicReassurance
	}
	if fresh.CreatedAt.IsZero() {
		fresh.CreatedAt = now
	}
	SaveKnowledge(fresh)
	res.Entry = fresh
	res.APIUsed = true
	return res, nil
}

// tierOf reads an entry's stored tier, defaulting to heuristic.
func tierOf(e *KnowledgeEntry) RetrievalTier {
	if e == nil || e.Tier == "" {
		return TierSimple
	}
	return RetrievalTier(e.Tier)
}

// mergeEntry folds fresh data into an existing stale entry (incremental
// update semantics): replace answer/sources, keep event-chain history.
func mergeEntry(dst, src *KnowledgeEntry) {
	if src == nil {
		return
	}
	if src.AnswerSummary != "" {
		dst.AnswerSummary = src.AnswerSummary
	}
	if len(src.KeyFacts) > 0 {
		dst.KeyFacts = src.KeyFacts
	}
	if len(src.Sources) > 0 {
		dst.Sources = src.Sources
	}
	if src.TotalTokens > 0 {
		dst.TotalTokens = src.TotalTokens
	}
	// Audit: a refresh supersedes the origin, so the provenance ID follows
	// the freshest request (rollback by request id must catch replaced
	// content, not just the original).
	if src.SourceRequestID != "" {
		dst.SourceRequestID = src.SourceRequestID
	}
}

var (
	errFetchFuncRequired = errCacheAPI("fetch function required")
	errEmptyFetchResult  = errCacheAPI("fetch returned nil entry")
)

// panicReassurance is the #13 wall-breaking guide appended to disaster queries
// under explicit PanicMode. It grounds the answer in official sources and
// gently redirects anxiety toward actionable safety behavior — never blocks or
// lectures. Wording is deliberately calm and concrete.
const panicReassurance = "⏳ 温馨提示：以上信息截至检索时刻。突发灾害动态请以官方应急渠道为准（如应急管理部门、气象/地震台网官方公告）。反复搜索灾难信息可能放大焦虑——掌握科学避险知识比持续刷新更有效。如感到不安，可联系身边亲友或当地援助热线。"

type errCacheAPI string

func (e errCacheAPI) Error() string { return string(e) }
