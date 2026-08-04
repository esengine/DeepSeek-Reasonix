package responses

import (
	"strings"
	"testing"
)

func TestClassifyScene(t *testing.T) {
	cases := map[string]InfoDomain{
		"股票今天涨了吗":     DomainEconomic,
		"这篇论文的方法是什么":  DomainResearch,
		"这个 api 怎么调用": DomainCode,
		"考研数学怎么复习":    DomainStudent,
		"工厂产能怎么提升":    DomainIndustrial,
		"今天天气怎么样":     DomainGeneral,
	}
	for q, want := range cases {
		if got := ClassifyScene(q); got != want {
			t.Errorf("ClassifyScene(%q)=%s want %s", q, got, want)
		}
	}
}

func TestSceneAudiences(t *testing.T) {
	if len(AudiencesFor(DomainEconomic)) != 4 {
		t.Errorf("economic audiences=%d want 4", len(AudiencesFor(DomainEconomic)))
	}
	if len(AudiencesFor(DomainResearch)) != 3 {
		t.Errorf("research audiences=%d want 3", len(AudiencesFor(DomainResearch)))
	}
	if len(AudiencesFor(DomainGeneral)) != 0 {
		t.Errorf("general audiences must be empty")
	}
}

func TestSceneQueryMultilingual(t *testing.T) {
	// 场景提示词 + 语言后缀
	q := SceneQuery("AI", DomainCode, "ja")
	if !strings.Contains(q, "ドキュメント") && !strings.Contains(q, "API") && !strings.Contains(q, "最新") {
		t.Errorf("code+ja query wrong: %q", q)
	}
	// 语言后缀存在
	for _, lang := range Languages {
		q := SceneQuery("topic", DomainGeneral, lang)
		if q == "" {
			t.Errorf("lang %s empty query", lang)
		}
	}
}

func TestMergeFrames(t *testing.T) {
	f1 := &InfoFrame{Domain: DomainEconomic, Language: "zh", Topic: "AI",
		Facts: []string{"市场 500 亿", "增长 20%"}, Confidence: 0.8,
		Sources: []Source{{Title: "中经网", URL: "https://ce.cn/a", Domain: "ce.cn", Credibility: 0.8}}}
	f2 := &InfoFrame{Domain: DomainEconomic, Language: "en", Topic: "AI",
		Facts: []string{"market $50B", "增长 20%"}, Confidence: 0.7,
		Sources: []Source{{Title: "Reuters", URL: "https://reuters.com/b", Domain: "reuters.com", Credibility: 0.9}}}
	f3 := &InfoFrame{Domain: DomainCode, Language: "ja", Topic: "AI",
		Facts: []string{"OSS 活跃"}, Confidence: 0.6}

	v := MergeFrames("AI 产业", []*InfoFrame{f1, f2, f3})
	// 3 帧合并
	if len(v.Frames) != 3 {
		t.Fatalf("frames=%d want 3", len(v.Frames))
	}
	// 事实去重：增长 20% 只出现一次
	count := 0
	for _, f := range v.MergedFacts {
		if strings.Contains(f, "增长 20%") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("facts must dedupe: %v", v.MergedFacts)
	}
	// 来源聚合 2 个
	if len(v.AllSources) != 2 {
		t.Fatalf("sources=%d want 2", len(v.AllSources))
	}
	// 语言覆盖 zh/en/ja
	if len(v.Languages) != 3 {
		t.Fatalf("languages=%v want 3", v.Languages)
	}
	// 场景覆盖 economic/code
	if len(v.Domains) != 2 {
		t.Fatalf("domains=%v want 2", v.Domains)
	}
	// 平均置信度
	if v.AvgConfidence < 0.69 || v.AvgConfidence > 0.71 {
		t.Fatalf("avg confidence=%.2f want ~0.7", v.AvgConfidence)
	}
	// 渲染要素
	md := v.Render()
	for _, want := range []string{"信息拼图", "覆盖语言", "拼图事实", "来源", "增长 20%"} {
		if !strings.Contains(md, want) {
			t.Fatalf("render must contain %q", want)
		}
	}
}

func TestMergeFramesEmpty(t *testing.T) {
	v := MergeFrames("空", nil)
	if v.Topic != "空" || len(v.Frames) != 0 {
		t.Fatalf("empty merge wrong: %+v", v)
	}
	if v.Render() == "" {
		t.Fatal("empty render must produce header")
	}
}

func TestPlanAndSceneComposeMultilingual(t *testing.T) {
	// planner 四维模板 × scene 多语言 = 完整多语言检索计划
	plan := PlanResearch("AI 监管", DepthL2)
	for _, lang := range []string{"zh", "en", "ja"} {
		for _, q := range plan.Queries {
			scoped := SceneQuery(q.Query, DomainResearch, lang)
			if scoped == "" {
				t.Fatalf("composed query empty for %s", lang)
			}
		}
	}
}
