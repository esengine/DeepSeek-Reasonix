package responses

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
)

// KnowledgeEntry is the persisted result of a server-side web_search turn.
// It lets a repeated query short-circuit the API: the search results are
// already distilled into AnswerSummary/KeyFacts/Sources by the model, so a
// cache hit answers instantly with zero token cost.
type KnowledgeEntry struct {
	Query         string   `json:"query"`
	QueryHash     string   `json:"query_hash"`
	AnswerSummary string   `json:"answer_summary"`
	KeyFacts      []string `json:"key_facts,omitempty"`
	Sources       []Source `json:"sources,omitempty"`
	TotalTokens   int      `json:"total_tokens,omitempty"`

	// TimeSensitive marks time-critical content (news, markets, live
	// events). FreshUntil bounds how long a hit may be served without a
	// refresh; after it, the entry is served as a stale fallback while an
	// incremental web_search refresh is triggered (retrieval tier P1).
	TimeSensitive bool `json:"time_sensitive,omitempty"`
	// FreshUntil is the freshness deadline for TimeSensitive entries. Zero
	// falls back to ExpiresAt. Non-sensitive entries ignore it (facts do
	// not go stale within the TTL).
	FreshUntil time.Time `json:"fresh_until,omitempty"`
	// Tier records the retrieval difficulty that produced this entry:
	// simple / general / complex / deep (retrieval tier P2 routing).
	Tier string `json:"tier,omitempty"`

	// ---- P4: 信息流模型（动态事件追踪，情报模式）----
	// EventChain links this entry to related event queries (初始事件→后续
	// 更新→关联事件)。元素是相关查询的原始文本。
	EventChain []string `json:"event_chain,omitempty"`
	// Confidence 是 0..1 的事件置信度，随增量更新上升、随冲突信号下降。
	Confidence float64 `json:"confidence,omitempty"`
	// UpdateCount 是该事件被增量刷新/更新的次数。
	UpdateCount int `json:"update_count,omitempty"`
	// ConflictDetected 标记多源矛盾（冲突信号），提示答案可能不稳定。
	ConflictDetected bool `json:"conflict_detected,omitempty"`
	// LastUpdatedAt 记录最近一次增量更新的时间。
	LastUpdatedAt time.Time `json:"last_updated_at,omitempty"`

	// SourceRequestID 是产生本缓存记录的上游请求标识（审计第四层）：用于
	// 追溯是哪一次检索/哪个 web_search 结果引入了内容，污染爆发时可
	// 按 request id 定位并回滚。
	SourceRequestID string `json:"source_request_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	// LastAccess is the last time this entry served a hit (L1 or L2).
	// Capacity governance uses it as the LRU key: when the cache exceeds
	// MaxKnowledgeEntries, the least-recently-accessed entries are evicted.
	LastAccess time.Time `json:"last_access,omitempty"`
	// Language is the detected language of the query ("zh"/"en"). Empty
	// means unknown and never constrains hits. L2 semantic hits require
	// language compatibility (both empty, equal, or one empty) so an "en"
	// frame never gets served a Chinese snapshot.
	Language string `json:"language,omitempty"`
	// QueryVariants are near-synonym queries that hit this entry via L2 and
	// were promoted to L1 (learning loop): each is recorded in a
	// variant_<hash>.json mapping file so the next identical query resolves
	// in O(1) instead of a full-directory semantic scan. The more the cache
	// is used, the more variants promote — 越使用越好, cost goes down.
	QueryVariants []string `json:"query_variants,omitempty"`
}

// NeedsRefresh reports whether a cached hit should trigger an incremental
// update rather than being served as final. Time-sensitive entries whose
// FreshUntil (or ExpiresAt fallback) has passed need a refresh; static facts
// never do within their TTL. Callers still receive the stale entry, but
// should kick off a web_search refresh and merge.
func (e *KnowledgeEntry) NeedsRefresh(now time.Time) bool {
	if e == nil {
		return false
	}
	if !e.TimeSensitive {
		return false
	}
	deadline := e.FreshUntil
	if deadline.IsZero() {
		deadline = e.ExpiresAt
	}
	return !deadline.IsZero() && now.After(deadline)
}

// Source is one citation surfaced by the model for a web_search turn.
type Source struct {
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	// Domain is the normalized registrable domain (e.g. reuters.com).
	// Populated by quality.go scoring when the entry is saved.
	Domain string `json:"domain,omitempty"`
	// Credibility is the P3 gate-3 quality score (0..1) assigned to this
	// source: whitelist + authority + cross-check + spam penalty.
	Credibility float64 `json:"credibility,omitempty"`
}

// DefaultKnowledgeTTL bounds how long a search result is treated as fresh.
// Web results go stale fast; 7 days matches the knowledge-extraction design.
const DefaultKnowledgeTTL = 7 * 24 * time.Hour

var errCacheDirUnavailable = errors.New("knowledge cache dir unavailable")

// knowledgeDirOverride lets tests redirect the cache root to an isolated
// temp dir so test runs never touch the real user cache (side-effect fix:
// the 100-round hammer test previously wiped ~/.cache/reasonix/websearch).
var knowledgeDirOverride string

// SetKnowledgeDirOverride redirects the knowledge cache root. Used by tests
// in OTHER packages (e.g. internal/tool/builtin) whose process has no
// TestMain of their own — without it their cache-cleaning helpers wipe the
// real user cache. Empty restores the default.
func SetKnowledgeDirOverride(dir string) { knowledgeDirOverride = dir }

// KnowledgeCacheDir exposes the effective cache root (honoring the test
// override) so cross-package helpers can clean exactly what the cache code
// reads. Returns errCacheDirUnavailable when the root is unset.
func KnowledgeCacheDir() (string, error) { return knowledgeDir() }

// knowledgeDir is the per-user cache root for web_search knowledge entries.
func knowledgeDir() (string, error) {
	root := config.CacheDir()
	if knowledgeDirOverride != "" {
		root = knowledgeDirOverride
	}
	if root == "" {
		return "", errCacheDirUnavailable
	}
	dir := filepath.Join(root, "websearch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// KnowledgeHash derives the cache key for a query (SHA-256, stable across
// runs; the full hash is used so collisions are practically impossible).
func KnowledgeHash(query string) string {
	sum := sha256.Sum256([]byte(query))
	return hex.EncodeToString(sum[:])
}

// LoadKnowledge returns the cached entry for query when present and unexpired.
// The boolean reports a hit; err is nil on any non-fatal path (missing file,
// corrupt JSON, expired entry all count as a miss).
func LoadKnowledge(query string) (*KnowledgeEntry, bool) {
	dir, err := knowledgeDir()
	if err != nil {
		return nil, false
	}
	e, path, ok := loadKnowledgeEntry(dir, query)
	if !ok {
		return nil, false
	}
	// L1 命中更新 LastAccess（LRU 治理的访问标记）。
	touchEntry(path, e)
	return e, true
}

// loadKnowledgeEntry resolves query to an entry: primary hash file first,
// then the variant map (learning loop: a near-synonym promoted to L1 after
// a prior L2 hit resolves in O(1) without scanning the directory).
func loadKnowledgeEntry(dir, query string) (*KnowledgeEntry, string, bool) {
	now := time.Now()
	path := filepath.Join(dir, KnowledgeHash(query)+".json")
	if data, err := os.ReadFile(path); err == nil {
		var e KnowledgeEntry
		if json.Unmarshal(data, &e) == nil && (e.ExpiresAt.IsZero() || !now.After(e.ExpiresAt)) {
			return &e, path, true
		}
		_ = os.Remove(path)
	}
	// 变体映射：variant_<hash>.json 内容 = 主条目 hash。
	vpath := filepath.Join(dir, "variant_"+KnowledgeHash(query)+".json")
	data, err := os.ReadFile(vpath)
	if err != nil {
		return nil, "", false
	}
	target := strings.TrimSpace(string(data))
	// 校验目标为 64-hex（主条目 hash 形态）——损坏/篡改的 variant
	// 内容不得拼出目录外路径（review 发现）。
	if len(target) != 64 {
		_ = os.Remove(vpath)
		return nil, "", false
	}
	for _, ch := range target {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			_ = os.Remove(vpath)
			return nil, "", false
		}
	}
	mainPath := filepath.Join(dir, target+".json")
	md, err := os.ReadFile(mainPath)
	if err != nil {
		_ = os.Remove(vpath) // 主条目已淘汰，映射失效
		return nil, "", false
	}
	var e KnowledgeEntry
	if err := json.Unmarshal(md, &e); err != nil || (!e.ExpiresAt.IsZero() && now.After(e.ExpiresAt)) {
		_ = os.Remove(vpath)
		return nil, "", false
	}
	// 变体路径同样执行语言门：变体映射是 L2 命中的提升，但后续查询
	// 可能来自另一语言（英文查询经变体 L1 命中中文条目——2026-08-03
	// verify_math 发现）。语言不兼容 → 映射作废（不再服务该查询）。
	if ql, el := DetectLanguage(query), e.Language; ql != "" && el != "" && ql != el {
		_ = os.Remove(vpath)
		return nil, "", false
	}
	// 时效查询不服务本地静态 domain 条目（变体路径同样适用——
	// 数论检索 2026-08-03：英文时效查询经变体命中律算合一条目）。
	if e.Tier == string(TierDomain) && HasFreshnessIntent(query) {
		return nil, "", false
	}
	// 主题一致性验证（变体路径兜底）：变体映射可能由历史宽松阈值建立
	// （ABC→CRT 错误映射，2026-08-03）——命中后仍要求主题词交集，
	// 不信任历史映射文件。
	if !topicsOverlap(query, e.Query) || !placesOverlap(query, e.Query) {
		_ = os.Remove(vpath)
		return nil, "", false
	}
	return &e, mainPath, true
}

// SaveKnowledge persists a distilled search result. Failures are swallowed:
// caching is best-effort and must never break the calling turn. The cache key
// is always re-derived from Query (never trusted from persisted JSON), so a
// malicious stored query_hash cannot escape the cache dir via path traversal.
func SaveKnowledge(e *KnowledgeEntry) {
	dir, err := knowledgeDir()
	if err != nil {
		return
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	if e.ExpiresAt.IsZero() {
		e.ExpiresAt = time.Now().Add(DefaultKnowledgeTTL)
	}
	if e.LastAccess.IsZero() {
		e.LastAccess = time.Now()
	}
	// Language detection on write so L2 semantic hits can enforce language
	// compatibility (an "en" frame must not be served a Chinese snapshot).
	if e.Language == "" {
		e.Language = DetectLanguage(e.Query)
	}
	// Re-derive unconditionally: QueryHash in persisted JSON is advisory
	// metadata, not a path component source.
	e.QueryHash = KnowledgeHash(e.Query)
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(dir, e.QueryHash+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
	enforceKnowledgeCapacity(dir)
}

// MaxKnowledgeEntries caps the knowledge cache. Beyond this, the
// least-recently-accessed entries are evicted on write (LRU governance —
// cost-first: the cache must not grow without bound on a 1M-token vendor).
var MaxKnowledgeEntries = 500

// enforceKnowledgeCapacity evicts LRU entries when the cache exceeds the
// cap. Expired entries are always removed first (they are dead weight).
func enforceKnowledgeCapacity(dir string) {
	now := time.Now()
	type stat struct {
		path    string
		access  time.Time
		expired bool
	}
	var st []stat
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") || strings.HasPrefix(de.Name(), "variant_") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		info, err := de.Info()
		if err != nil {
			continue
		}
		s := stat{path: path, access: info.ModTime()}
		if data, err := os.ReadFile(path); err == nil {
			var e KnowledgeEntry
			if json.Unmarshal(data, &e) == nil {
				if !e.LastAccess.IsZero() {
					s.access = e.LastAccess
				}
				s.expired = !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt)
			}
		}
		st = append(st, s)
	}
	if len(st) <= MaxKnowledgeEntries {
		return
	}
	// 过期优先删；其余按 LastAccess 最旧先删（LRU）。
	over := len(st) - MaxKnowledgeEntries
	var evict []stat
	for _, s := range st {
		if s.expired {
			evict = append(evict, s)
		}
	}
	if len(evict) < over {
		var rest []stat
		for _, s := range st {
			if !s.expired {
				rest = append(rest, s)
			}
		}
		sort.Slice(rest, func(i, j int) bool { return rest[i].access.Before(rest[j].access) })
		evict = append(evict, rest[:min(over-len(evict), len(rest))]...)
	}
	for _, s := range evict {
		_ = os.Remove(s.path)
	}
}

// DetectLanguage heuristically classifies a query as zh or en. Any CJK
// character makes it zh (Chinese is the dominant semantic carrier even in
// mixed queries like "Operation Midnight Hammer 轰炸"); only pure-ASCII
// queries are en. A query with neither returns "" (unknown) so it never
// constrains semantic hits.
func DetectLanguage(s string) string {
	cjk, ascii := 0, 0
	for _, r := range s {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
			cjk++
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			ascii++
		}
	}
	switch {
	case cjk > 0:
		return "zh"
	case ascii > 0:
		return "en"
	default:
		return ""
	}
}

// DefaultSemanticThreshold is the character-set similarity cutoff for L2
// semantic hits. Near-synonym Chinese queries (今天北京天气 vs 北京今天天气)
// score ~0.5 under unigram Jaccard; unrelated queries score near 0. Tune per
// corpus: higher = fewer false positives but lower recall.
const DefaultSemanticThreshold = 0.35

// NgramSimilarity returns the Dice coefficient of character sets between a
// and b (0..1): 2·|A∩B| / (|A|+|B|). It is a cheap, local, dependency-free
// proxy for "semantic" matching on short Chinese/English queries. Unlike
// Jaccard it normalizes by the average length rather than the union, so a
// verbose query ("2026年8月3日北京天气怎么样") still scores well against a
// terse near-synonym ("北京今天天气如何") instead of being diluted by the
// extra characters. Word-order changes keep the same set, so near-synonym
// phrasings score high; punctuation and whitespace are ignored.
func NgramSimilarity(a, b string) float64 {
	ga := charSet(a)
	gb := charSet(b)
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	inter := 0
	for g := range ga {
		if gb[g] {
			inter++
		}
	}
	denom := len(ga) + len(gb)
	if denom == 0 {
		return 0
	}
	return 2 * float64(inter) / float64(denom)
}

func charSet(s string) map[rune]bool {
	out := make(map[rune]bool)
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r >= 0x4E00 && r <= 0x9FFF: // CJK unified ideographs
			out[r] = true
		}
	}
	return out
}

// LoadKnowledgeSemantic scans the cache for the unexpired entry whose query is
// most similar to q (L2 fallback after LoadKnowledge's exact hash miss). It
// returns the best match, its similarity, and true when that similarity meets
// or exceeds threshold. Zero-dependency local matching — no vector DB, no
// embedding API.
func LoadKnowledgeSemantic(q string, threshold float64) (*KnowledgeEntry, float64, bool) {
	dir, err := knowledgeDir()
	if err != nil {
		return nil, 0, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, false
	}
	var (
		best     *KnowledgeEntry
		bestSim  float64
		bestPath string
	)
	now := time.Now()
	for _, de := range entries {
		if de.IsDir() || de.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var e KnowledgeEntry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			_ = os.Remove(path)
			continue
		}
		sim := NgramSimilarity(q, e.Query)
		// 地理一致性：双方都含地名但不同（温州 vs 北京）→ 拦截。
		if !placesOverlap(q, e.Query) {
			sim = 0
		}
		// 时效查询不服务本地静态 domain 条目：用户要"最新论文/2026 进展"，
		// 本地注入的库快照不是最新（数论检索污染教训 2026-08-03）。
		if e.Tier == string(TierDomain) && HasFreshnessIntent(q) {
			sim = 0
		}
		// 主题一致性：Dice 相似度高但领域词无交集 = 误命中（如"霍尔木兹
		// 化肥"命中"霍尔木兹石油"）。无领域词的查询退化为纯相似度。
		if !topicsOverlap(q, e.Query) {
			sim = 0
		}
		// 语言一致性：zh 查询不命中 en 缓存（反之亦然）——跨语言语义
		// 命中会把 en 帧错误地喂给中文快照。未知语言（""）不约束。
		ql, el := DetectLanguage(q), e.Language
		if ql != "" && el != "" && ql != el {
			sim = 0
		}
		if sim > bestSim {
			bestSim = sim
			best = &e
			bestPath = path
		}
	}
	if best != nil && bestSim >= threshold {
		// L2 命中同样更新 LastAccess（LRU 治理）。
		if bestPath != "" {
			touchEntry(bestPath, best)
			// 学习闭环：把本次查询提升为 L1 变体（下次 O(1) 命中），
			// 并关联进事件链——越使用越好，成本随使用下降。
			// 变体建立必须用与本次命中相同的语言阈值（英文 0.55）：
			// 旧 0.35 英文误命中曾建立 ABC→CRT 错误映射（2026-08-03）。
			if bestSim >= SemanticThresholdFor(q) {
				learnVariant(bestPath, dir, q, best)
			}
		}
		return best, bestSim, true
	}
	return nil, 0, false
}

// learnVariant promotes query to L1 for the entry that just L2-hit it
// (learning loop): writes a variant_<hash>.json mapping file and records the
// variant in QueryVariants. Zero cost, no network — the more the cache is
// used, the more near-synonyms resolve in O(1). Also links the query into
// EventChain (related-topic graph accumulates with use).
func learnVariant(mainPath, dir, query string, e *KnowledgeEntry) {
	if query == e.Query || e.Query == "" {
		return
	}
	// 中立护栏：操纵/煽动查询（恐慌、营销话术）不建立变体关联——
	// 避免敏感查询被提升为正常条目的 L1 变体（R65 场景：manip 查询
	// 绕过"不落盘"隔离）。
	if emotionHits(query)+marketingHits(query) > 0 {
		return
	}
	e.QueryVariants = appendUnique(e.QueryVariants, query)
	e.EventChain = appendUnique(e.EventChain, query)
	if data, err := json.MarshalIndent(e, "", "  "); err == nil {
		if err := os.WriteFile(mainPath+".tmp", data, 0o600); err == nil {
			_ = os.Rename(mainPath+".tmp", mainPath)
		}
	}
	vpath := filepath.Join(dir, "variant_"+KnowledgeHash(query)+".json")
	_ = os.WriteFile(vpath, []byte(KnowledgeHash(e.Query)), 0o600)
}

// touchEntry updates LastAccess and persists it (LRU governance bookkeeping).
// Failures are swallowed: the hit still counts, only the access stamp may lag.
func touchEntry(path string, e *KnowledgeEntry) {
	e.LastAccess = time.Now()
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// ListKnowledge returns all unexpired cache entries (audit layer 4: daily
// sampling to re-check credibility, or operator inspection before rollback).
// Expired entries are pruned during the scan.
func ListKnowledge() []KnowledgeEntry {
	dir, err := knowledgeDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	now := time.Now()
	var out []KnowledgeEntry
	for _, de := range entries {
		if de.IsDir() || de.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var e KnowledgeEntry
		if err := json.Unmarshal(data, &e); err != nil {
			_ = os.Remove(path)
			continue
		}
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			_ = os.Remove(path)
			continue
		}
		out = append(out, e)
	}
	return out
}

// DeleteKnowledge removes cache entries matching pred. It returns the number
// of deleted entries and is the rollback primitive for pollution outbreaks
// (audit layer 4): delete by request id, by tier, or sweep everything.
func DeleteKnowledge(pred func(*KnowledgeEntry) bool) int {
	dir, err := knowledgeDir()
	if err != nil {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	deleted := 0
	for _, de := range entries {
		if de.IsDir() || de.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var e KnowledgeEntry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		if pred(&e) {
			if err := os.Remove(path); err == nil {
				deleted++
			}
		}
	}
	return deleted
}

// domainVocabulary maps domain-significant words; two queries sharing any
// such word are the same topic, sharing none means the semantic hit is a
// false positive (fix: 语义缓存误命中不同主题). Order matters: more
// specific words first.
var domainVocabulary = map[string][]string{
	"农业": {"化肥", "农业", "小麦", "粮食", "收成", "农产品", "氮磷钾", "尿素", "大豆", "玉米"},
	"能源": {"石油", "港口", "吞吐量", "霍尔木兹", "马六甲", "原油", "天然气", "lng", "海峡"},
	"军事": {"战争", "冲突", "军事", "航母", "轰炸", "导弹", "制裁"},
	"经济": {"经济", "gdp", "通胀", "油价", "市场", "衰退", "贸易", "gdp"},
	"科技": {"ai", "芯片", "模型", "算法", "代码", "编程", "开源"},
	"气候": {"天气", "气候", "台风", "暴雨", "干旱", "气温", "降水"},
	// 数论/数学（2026-08-03 数论检索误命中修复：ABC 猜想命中 CRT 拍频）
	"数论": {"数论", "素数", "质数", "猜想", "黎曼", "孪生素数", "conjecture", "prime", "number theory", "riemann", "bsd", "p-adic", "模形式", "算术"},
	"代数": {"代数", "hodge", "霍奇", "伽罗瓦", "代数几何", "同调", "上同调"},
	"分析": {"调和", "傅里叶", "泛函", "测度", "复分析", "harmonic", "functional"},
	"拓扑": {"同伦", "拓扑", "纤维", "homotopy", "陈类", "chern", "环面"},
}

// ExtractTopics returns the domain-significant words present in a query.
func ExtractTopics(query string) []string {
	var out []string
	lower := strings.ToLower(query)
	for _, words := range domainVocabulary {
		for _, w := range words {
			if strings.Contains(lower, w) {
				out = append(out, w)
			}
		}
	}
	return out
}

// topicsOverlap reports whether a and b share at least one domain word.
// 语义：仅当查询本身带明确领域词时才要求交集（跨主题防误命中）；查询
// 无领域词（如英文缩写/代号）退化为纯相似度（保守命中，不拦截）。
func topicsOverlap(a, b string) bool {
	wordsA := ExtractTopics(a)
	if len(wordsA) == 0 {
		return true // 查询无领域信号：不拦截
	}
	wordsB := ExtractTopics(b)
	if len(wordsB) == 0 {
		return true // 缓存无领域标签：无法判断主题，保守命中
	}
	for _, wa := range wordsA {
		for _, wb := range wordsB {
			if wa == wb {
				return true
			}
		}
	}
	return false
}

// placesVocabulary 是常见地名（中国主要城市 + 常用国家/地区），用于语义
// 缓存的地理一致性校验：查询与缓存条目都含地名但不同（温州 vs 北京）
// → 拦截（2026-08-03 温州天气误命中北京天气教训）。
var placesVocabulary = []string{
	"北京", "上海", "广州", "深圳", "温州", "杭州", "南京", "苏州", "天津", "重庆",
	"成都", "武汉", "西安", "长沙", "郑州", "青岛", "大连", "厦门", "福州", "宁波",
	"合肥", "南昌", "贵阳", "昆明", "兰州", "沈阳", "长春", "哈尔滨", "石家庄", "太原",
	"济南", "南宁", "海口", "乌鲁木齐", "拉萨", "呼和浩特", "银川", "西宁", "香港", "澳门",
	"台北", "高雄", "台中", "东京", "纽约", "伦敦", "巴黎", "柏林", "莫斯科", "新加坡",
	"首尔", "曼谷", "悉尼", "迪拜", "温哥华", "旧金山", "洛杉矶", "美国", "日本", "中国",
}

// ExtractPlaces returns the place names present in a query (deduped, order
// preserved). Empty when the query names no known place.
func ExtractPlaces(query string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range placesVocabulary {
		if strings.Contains(query, p) && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// placesOverlap reports whether both sides name places and share at least
// one. Two placeful queries with no common place are different geographies
// (温州 vs 北京) and must not share a semantic cache hit.
func placesOverlap(a, b string) bool {
	pa, pb := ExtractPlaces(a), ExtractPlaces(b)
	if len(pa) == 0 || len(pb) == 0 {
		return true // 至少一方无地名：不拦截
	}
	for _, x := range pa {
		for _, y := range pb {
			if x == y {
				return true
			}
		}
	}
	return false
}
