package responses

import (
	"net/url"
	"strings"
)

// Gate-3 quality filtering for knowledge-cache sources. The score weights
// follow the retrieval-tier design (5.1):
//
//	whitelist (verified professional source)   +0.40
//	multi-source cross-check (>=2 distinct)    +0.30
//	authority domain (gov/edu/mil/official)    +0.20
//	spam pattern hit                           -0.10 each
//
// Scores are clamped to [0, 1]. Callers decide a cutoff; FilterSources
// defaults to 0.50 (a whitelisted or cross-checked authoritative source
// passes; unknown junk does not).

// authorityTLDs are registrable top-level segments that imply an official
// operator. 中国政府/机构 and edu/research institutions. ".org" is NOT
// included: anyone can register an .org, so it conveys no authority (fix:
// .org 权威分滥用).
var authorityTLDs = []string{"gov.cn", "gov", "edu.cn", "edu", "mil", "ac.cn"}

// trustedDomains is a compact built-in whitelist of verified professional
// sources (subset of deep-research sources.md). Extend as sources.md grows.
var trustedDomains = map[string]bool{
	// 国际权威媒体
	"reuters.com": true, "apnews.com": true, "bbc.com": true, "bbc.co.uk": true,
	"nytimes.com": true, "wsj.com": true, "economist.com": true, "ft.com": true,
	"bloomberg.com": true, "theguardian.com": true, "aljazeera.com": true,
	// 中文权威
	"people.com.cn": true, "xinhuanet.com": true, "cctv.com": true, "gov.cn": true,
	"chinadaily.com.cn": true, "caixin.com": true, "yicai.com": true, "thepaper.cn": true,
	// 学术/知识库
	"wikipedia.org": true, "arxiv.org": true, "nature.com": true, "science.org": true,
	"springer.com": true, "ieee.org": true, "acm.org": true, "semanticscholar.org": true,
	"github.com": true,
	// 官方/组织
	"who.int": true, "un.org": true, "imf.org": true, "worldbank.org": true,
	"nasa.gov": true, "noaa.gov": true, "fda.gov": true,
	// 港口/海事官方（新加坡）
	"mpa.gov.sg": true, "data.gov.sg": true, "psa.gov.sg": true, "mti.gov.sg": true,
}

// spamDomains are ad/farm/low-quality sources that pollute results.
var spamDomains = map[string]bool{
	"medium.com": true, "quora.com": true, "reddit.com": true, "baidu.com": true,
	"zhihu.com": true, "sohu.com": true, "toutiao.com": true, "weibo.com": true,
	"163.com": true, "sina.com.cn": true, "qq.com": true, "bilibili.com": true,
	"douyin.com": true, "spam-site.com": true, "advertorial.com": true,
}

// spamURLHints are substrings that mark aggregator/advertorial junk even on
// unknown domains.
var spamURLHints = []string{"utm_source=ad", "sponsored", "affiliate", "advertorial", "/ads/", "click.php"}

// marketingHints are content-level manipulation markers (#11 细节检验法):
// absolute claims, blurry authority endorsements, over-perfect descriptions.
// Unlike domain-based signals these apply to the snippet/answer text itself,
// catching polished-but-unverifiable content on otherwise unknown domains.
var marketingHints = []string{
	"最有效", "零风险", "100%保证", "100%有效", "绝对安全", "包治", "立竿见影",
	"专家推荐", "官方认证", "权威背书", "不可错过", "限时抢购", "最后机会",
	"guaranteed", "100% effective", "zero risk", "miracle", "cure-all",
	"expert recommended", "act now", "limited time",
}

// emotionHints are fear/incitement/extremism markers (defense layer 2, check
// 1 of #12): attack content engineered to stoke panic. Marketing hints catch
// hype; these catch manipulation that triggers fear responses. Hits lower the
// source credibility and block cache persistence at a threshold.
var emotionHints = []string{
	"恐慌", "灾难", "末日", "崩溃", "致命", "失控", "威胁", "紧急预警",
	"不寒而栗", "触目惊心", "必须转发", "十万火急", "群发", "删前速看",
	"panic", "catastrophe", "doomsday", "collapse", "fatal", "out of control",
	"must share", "forward to everyone",
}

// emotionHits counts fear/incitement markers in text.
func emotionHits(text string) int {
	if text == "" {
		return 0
	}
	lower := strings.ToLower(text)
	hits := 0
	for _, h := range emotionHints {
		if strings.Contains(lower, strings.ToLower(h)) {
			hits++
		}
	}
	return hits
}

// EmotionHits is the exported form of emotionHits for gate-2 pre-write checks.
func EmotionHits(text string) int { return emotionHits(text) }

// panicHints are high-destruction disaster nouns. PanicScore combines these
// with time-urgency words to flag anxiety-amped queries (defense #13 layer 1).
// IMPORTANT (compliance): PanicScore is stateless — it inspects the current
// query text only and never tracks user behavior/frequency. Any user-profile
// features must be explicitly opted-in by the user (画像需用户主动提供或可拒绝).
var panicHints = []string{
	"地震", "海啸", "爆炸", "泄漏", "核", "辐射", "毒", "疫情", "战争", "袭击",
	"火灾", "洪水", "坍塌", "空难", "事故", "earthquake", "tsunami", "explosion",
	"radiation", "chemical", "attack", "war", "disaster",
}

// timeUrgencyHints amplify a disaster noun into "imminent threat" phrasing.
var timeUrgencyHints = []string{
	"今晚", "明天", "马上", "即将", "要来了", "会不会", "几点", "现在", "附近",
	"tonight", "tomorrow", "imminent", "coming", "nearby",
}

// PanicScore returns 0..N where N counts (disaster noun, time-urgency word)
// pairs in the query. 0 = no panic signal; >=1 flags the query for the
// optional 破壁引导 (wall-breaking guide) — the answer is never withheld,
// only supplemented with an authority-grounded reassurance line.
func PanicScore(query string) int {
	if query == "" {
		return 0
	}
	lower := strings.ToLower(query)
	hasDisaster := false
	for _, h := range panicHints {
		if strings.Contains(lower, strings.ToLower(h)) {
			hasDisaster = true
			break
		}
	}
	if !hasDisaster {
		return 0
	}
	score := 0
	for _, h := range timeUrgencyHints {
		if strings.Contains(lower, strings.ToLower(h)) {
			score++
		}
	}
	return score
}

// marketingHits counts content-level manipulation markers in text. A nonzero
// count is a strong signal the source is promotional rather than factual.
func marketingHits(text string) int {
	if text == "" {
		return 0
	}
	lower := strings.ToLower(text)
	hits := 0
	for _, h := range marketingHints {
		if strings.Contains(lower, strings.ToLower(h)) {
			hits++
		}
	}
	return hits
}

// MarketingHits is the exported form of marketingHits for callers that want
// to gate answers on promotional content (agent layer, UI badge, etc.).
func MarketingHits(text string) int { return marketingHits(text) }

// domainOf extracts the normalized registrable domain (host minus scheme,
// port, and www. prefix) from a URL. Empty on unparsable input.
func domainOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	host = strings.ToLower(host)
	host = strings.TrimPrefix(host, "www.")
	return host
}

// isAuthorityDomain reports whether host ends in an official TLD segment.
func isAuthorityDomain(host string) bool {
	if host == "" {
		return false
	}
	for _, tld := range authorityTLDs {
		if host == tld || strings.HasSuffix(host, "."+tld) {
			return true
		}
	}
	return false
}

// hasSpamHint reports whether the raw URL carries an ad/affiliate marker.
func hasSpamHint(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	for _, h := range spamURLHints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	return false
}

// scoreSource computes the gate-3 credibility for one source. Cross-check
// evidence (nDistinct distinct domains among allSources) is folded in so a
// claim backed by several independent outlets scores higher than a lone one.
func scoreSource(s Source, allSources []Source) float64 {
	score := 0.0
	host := domainOf(s.URL)
	if host != "" {
		if trustedDomains[host] {
			score += 0.40
		}
		if isAuthorityDomain(host) {
			score += 0.20
		}
		if spamDomains[host] {
			score -= 0.10
		}
	}
	if hasSpamHint(s.URL) {
		score -= 0.10
	}
	// Content-level manipulation: absolute claims / blurry endorsements in
	// the snippet are promotional, not factual.
	if hits := marketingHits(s.Snippet); hits > 0 {
		score -= 0.10 * float64(hits)
	}
	// Fear/incitement manipulation (defense layer 2 check 1): engineered
	// panic content is a harder negative than marketing hype.
	if hits := emotionHits(s.Snippet); hits > 0 {
		score -= 0.15 * float64(hits)
	}
	// Cross-check: at least two distinct domains among all sources.
	seen := map[string]bool{}
	for _, o := range allSources {
		d := domainOf(o.URL)
		if d != "" {
			seen[d] = true
		}
	}
	if len(seen) >= 2 {
		score += 0.30
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

// ScoreAndTagSources fills Domain/Credibility on every source of an entry.
func ScoreAndTagSources(entry *KnowledgeEntry) {
	if entry == nil {
		return
	}
	for i := range entry.Sources {
		s := &entry.Sources[i]
		// URL 非空时从 URL 重算 Domain；URL 空时保留提取器预设的机构
		// Domain（如 extractInlineSources 的 mpa.gov.sg），否则白名单
		// 豁免会因 Domain 清空而失效。
		if d := domainOf(s.URL); d != "" {
			s.Domain = d
		}
		s.Credibility = scoreSource(*s, entry.Sources)
	}
}

// FilterSources keeps sources scoring at or above minScore (default 0.5 when
// minScore <= 0). A source is also kept when it is whitelisted regardless of
// score — verified professional outlets beat a generic cutoff.
func FilterSources(sources []Source, minScore float64) []Source {
	if minScore <= 0 {
		minScore = 0.5
	}
	out := make([]Source, 0, len(sources))
	for _, s := range sources {
		if s.Credibility >= minScore || (s.Domain != "" && trustedDomains[s.Domain]) {
			out = append(out, s)
		}
	}
	return out
}
