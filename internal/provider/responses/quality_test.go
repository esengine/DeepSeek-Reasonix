package responses

import "testing"

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"https://reuters.com/world/xyz":    "reuters.com",
		"http://www.bbc.co.uk/news":        "bbc.co.uk",
		"https://sub.domain.gov.cn/page":   "sub.domain.gov.cn",
		"https://zh.wikipedia.org/wiki/AI": "zh.wikipedia.org",
		"not a url":                        "",
		"":                                 "",
	}
	for in, want := range cases {
		if got := domainOf(in); got != want {
			t.Errorf("domainOf(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsAuthorityDomain(t *testing.T) {
	if !isAuthorityDomain("nasa.gov") || !isAuthorityDomain("university.edu.cn") || !isAuthorityDomain("army.mil") {
		t.Fatal("authority domains must be recognized")
	}
	if isAuthorityDomain("example.com") || isAuthorityDomain("junk.net") {
		t.Fatal("non-authority domains must not be recognized")
	}
}

func TestScoreAndFilterSources(t *testing.T) {
	e := &KnowledgeEntry{Sources: []Source{
		{URL: "https://reuters.com/world/a"},                      // whitelist 0.4 + cross 0.3 = 0.7
		{URL: "https://www.gov.cn/policy/b"},                      // authority 0.2 + cross 0.3 = 0.5
		{URL: "https://junk-blog.example.com/post?utm_source=ad"}, // spam hint -0.1 + cross 0.3 = 0.2
	}}
	ScoreAndTagSources(e)

	if e.Sources[0].Credibility < 0.6 || e.Sources[0].Domain != "reuters.com" {
		t.Fatalf("reuters should score >=0.6, got %.2f (%s)", e.Sources[0].Credibility, e.Sources[0].Domain)
	}
	if e.Sources[1].Credibility < 0.45 {
		t.Fatalf("gov.cn should score >=0.45, got %.2f", e.Sources[1].Credibility)
	}
	if e.Sources[2].Credibility > 0.35 {
		t.Fatalf("spam source should score <=0.35, got %.2f", e.Sources[2].Credibility)
	}

	kept := FilterSources(e.Sources, 0.5)
	if len(kept) != 2 {
		t.Fatalf("want 2 kept, got %d: %#v", len(kept), kept)
	}
	if kept[0].URL == e.Sources[2].URL {
		t.Fatal("spam source must be filtered out")
	}
}

func TestSpamDomainPenalized(t *testing.T) {
	e := &KnowledgeEntry{Sources: []Source{
		{URL: "https://medium.com/clickbait"},
		{URL: "https://reuters.com/real"},
	}}
	ScoreAndTagSources(e)
	if e.Sources[0].Credibility >= e.Sources[1].Credibility {
		t.Fatalf("spam domain must score below whitelist: %.2f vs %.2f", e.Sources[0].Credibility, e.Sources[1].Credibility)
	}
}

func TestMarketingHits(t *testing.T) {
	if h := MarketingHits("这款产品最有效，100%保证见效"); h < 2 {
		t.Fatalf("absolute claims should hit, got %d", h)
	}
	if h := MarketingHits("专家推荐，限时抢购最后机会"); h < 3 {
		t.Fatalf("blurry endorsement + urgency should hit, got %d", h)
	}
	if h := MarketingHits("北京市气象台发布暴雨蓝色预警，午后局地有强降水"); h != 0 {
		t.Fatalf("neutral fact must not hit, got %d", h)
	}
	if h := MarketingHits(""); h != 0 {
		t.Fatalf("empty must be 0, got %d", h)
	}
}

func TestMarketingSnippetPenalizesScore(t *testing.T) {
	e := &KnowledgeEntry{Sources: []Source{
		{URL: "https://blog-a.example.com/a", Snippet: "最有效的解决方案，零风险，专家推荐"}, // 3 marketing hits
		{URL: "https://news-b.example.org/b", Snippet: "今天北京多云转阴"},          // neutral
	}}
	ScoreAndTagSources(e)
	if e.Sources[0].Credibility >= e.Sources[1].Credibility {
		t.Fatalf("marketing snippet must score below neutral: %.2f vs %.2f",
			e.Sources[0].Credibility, e.Sources[1].Credibility)
	}
	// 中性 snippet 不被误伤（有交叉验证分）
	if e.Sources[1].Credibility < 0.3 {
		t.Fatalf("neutral snippet unexpectedly penalized: %.2f", e.Sources[1].Credibility)
	}
}

func TestEmotionHits(t *testing.T) {
	if h := EmotionHits("紧急预警！这场灾难即将失控，必须转发给所有人"); h < 3 {
		t.Fatalf("fear/incitement text should hit, got %d", h)
	}
	if h := EmotionHits("今日北京多云转晴，气温回升"); h != 0 {
		t.Fatalf("neutral text must not hit, got %d", h)
	}
	if h := EmotionHits(""); h != 0 {
		t.Fatalf("empty must be 0, got %d", h)
	}
}

func TestEmotionSnippetPenalizesScore(t *testing.T) {
	e := &KnowledgeEntry{Sources: []Source{
		{URL: "https://fear-a.example.com/x", Snippet: "恐慌蔓延！末日将至，十万火急"}, // emotion hits
		{URL: "https://calm-b.example.org/y", Snippet: "今日北京多云转晴"},       // neutral
	}}
	ScoreAndTagSources(e)
	if e.Sources[0].Credibility >= e.Sources[1].Credibility {
		t.Fatalf("fear snippet must score below neutral: %.2f vs %.2f",
			e.Sources[0].Credibility, e.Sources[1].Credibility)
	}
}

func TestPanicScore(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"今晚北京会地震吗", 1},   // 地震 + 今晚
		{"明天会有海啸吗", 1},    // 海啸 + 明天
		{"地震是什么原因造成的", 0}, // 无时间紧迫词（知识性）
		{"今天天气怎么样", 0},    // 无灾难词
		{"附近核电站会不会泄漏", 2}, // 核/泄漏 + 附近 + 会不会
		{"", 0},
	}
	for _, c := range cases {
		if got := PanicScore(c.query); got != c.want {
			t.Errorf("PanicScore(%q)=%d want %d", c.query, got, c.want)
		}
	}
}

func TestWhitelistDomainSurvivesSanitize(t *testing.T) {
	// 修复：extractInlineSources 提取的 MPA（Domain 预设，无 URL）经过
	// sanitizeFresh（ScoreAndTagSources + FilterSources）后必须保留
	//（白名单豁免），否则来源被 0.5 阈值过滤。
	entry := &KnowledgeEntry{
		AnswerSummary: "新加坡港口数据",
		Sources:       []Source{{Title: "MPA", Domain: "mpa.gov.sg"}}, // 无 URL
	}
	if !sanitizeFresh(entry, 0.5) {
		t.Fatal("whitelisted source must pass sanitize")
	}
	if len(entry.Sources) != 1 {
		t.Fatalf("whitelisted source must survive FilterSources, got %d", len(entry.Sources))
	}
	if entry.Sources[0].Domain != "mpa.gov.sg" {
		t.Fatalf("domain must survive scoring: %q", entry.Sources[0].Domain)
	}
	// 非白名单无 URL 源 → 被过滤（无法验证来源不保留）
	entry2 := &KnowledgeEntry{Sources: []Source{{Title: "未知", Domain: "unknown.example"}}}
	sanitizeFresh(entry2, 0.5)
	if len(entry2.Sources) != 0 {
		t.Fatalf("non-whitelisted source should be filtered: %v", entry2.Sources)
	}
}
