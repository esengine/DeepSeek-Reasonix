package responses

import (
	"strings"
	"testing"
)

func TestSynthesizeReport(t *testing.T) {
	e1 := &KnowledgeEntry{
		Query:         "北京天气",
		AnswerSummary: "test-summary",
		KeyFacts:      []string{"高温 33℃", "有雷阵雨"},
		Sources: []Source{
			{Title: "气象台", URL: "https://gov.cn/a", Credibility: 0.8},
			{Title: "天气网", URL: "https://weather.com/b", Credibility: 0.6},
		},
	}
	e2 := &KnowledgeEntry{
		Query:            "北京天气补充",
		KeyFacts:         []string{"高温 33℃", "局地暴雨"},
		Sources:          []Source{{Title: "应急局", URL: "https://gov.cn/c", Credibility: 0.9}},
		ConflictDetected: true,
	}
	r := SynthesizeReport("北京 8 月天气", []*KnowledgeEntry{e1, e2})

	if !r.HasConflict {
		t.Fatal("conflict must be flagged")
	}
	// 事实去重：高温 33℃ 只出现一次
	facts := 0
	for _, f := range r.Sections[0].Facts {
		if strings.Contains(f, "高温") {
			facts++
		}
	}
	if facts != 1 {
		t.Fatalf("facts must dedupe, got %d", facts)
	}
	// 来源聚合 3 个
	if len(r.AllSources) != 3 {
		t.Fatalf("want 3 sources, got %d", len(r.AllSources))
	}
	// 渲染包含关键要素
	md := r.Render()
	for _, want := range []string{"# 北京 8 月天气", "摘要", "关键事实", "来源", "矛盾"} {
		if !strings.Contains(md, want) {
			t.Fatalf("render must contain %q, got:\n%s", want, md)
		}
	}
}

func TestSynthesizeReportEmpty(t *testing.T) {
	r := SynthesizeReport("空课题", nil)
	if r.Summary != "" || len(r.Sections) != 0 {
		t.Fatalf("empty input must yield minimal report: %+v", r)
	}
	// 空报告也能渲染（不 panic）
	if r.Render() == "" {
		t.Fatal("render must produce header")
	}
}
