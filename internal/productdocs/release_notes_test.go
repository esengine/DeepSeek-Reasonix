package productdocs

import (
	"strings"
	"testing"
)

func TestTargetedReleaseDocumentsSplitDesktopAndCLIUpdates(t *testing.T) {
	documents, _, err := renderReleaseDocuments([]byte(`{
		"releases": [{
			"version": "2.0.0",
			"targetingVersion": 1,
			"date": "2026-01-01",
			"channel": "stable",
			"status": "reviewed",
			"title": {"en": "Targeted", "zh": "分类版本"},
			"summary": {"en": "Summary", "zh": "摘要"},
			"surfaces": ["desktop", "cli", "service"],
			"highlights": [
				{"kind": "fixed", "targets": ["desktop", "cli"], "title": {"en": "Shared fix", "zh": "共享修复"}, "body": {"en": "Shared body.", "zh": "共享正文。"}},
				{"kind": "improved", "targets": ["service"], "title": {"en": "Service work", "zh": "服务端改进"}, "body": {"en": "Service body.", "zh": "服务正文。"}}
			],
			"changes": {
				"new": [],
				"improved": [],
				"fixed": [{"targets": ["desktop"], "title": {"en": "Desktop fix", "zh": "桌面端修复"}, "body": {"en": "Desktop body.", "zh": "桌面端正文。"}}]
			},
			"upgrade": [],
			"risks": []
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var chinese string
	for _, document := range documents {
		if document.locale == "zh-CN" {
			chinese = document.content
			break
		}
	}
	for _, want := range []string{"## 桌面端更新", "## CLI 端更新", "## 其他项目更新", "[桌面端 · CLI]", "[服务端]"} {
		if !strings.Contains(chinese, want) {
			t.Fatalf("targeted release document missing %q:\n%s", want, chinese)
		}
	}
	if got := strings.Count(chinese, "共享修复"); got != 2 {
		t.Fatalf("shared item rendered %d times, want once per client section:\n%s", got, chinese)
	}
}

func TestLegacyReleaseDocumentsKeepOriginalSections(t *testing.T) {
	documents, _, err := renderReleaseDocuments([]byte(`{
		"releases": [{
			"version": "1.0.0",
			"title": {"en": "Legacy", "zh": "旧版"},
			"summary": {"en": "Summary", "zh": "摘要"},
			"highlights": [{"kind": "fixed", "title": {"en": "Fix", "zh": "修复"}, "body": {"en": "Body.", "zh": "正文。"}}],
			"changes": {"new": [], "improved": [], "fixed": []}
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	english := documents[0].content
	if !strings.Contains(english, "## Highlights") {
		t.Fatalf("legacy release lost original sections:\n%s", english)
	}
	if strings.Contains(english, "## Desktop updates") || strings.Contains(english, "## CLI updates") {
		t.Fatalf("legacy release unexpectedly used targeted sections:\n%s", english)
	}
}

func TestTargetedReleaseDocumentsExplainEmptyCLISection(t *testing.T) {
	release := releaseRecord{
		Version:          "2.0.1",
		TargetingVersion: 1,
		Title:            localizedText{English: "Desktop only", Chinese: "仅桌面端"},
		Summary:          localizedText{English: "Summary", Chinese: "摘要"},
		Surfaces:         []string{"desktop"},
		Highlights: []releaseItem{{
			Kind:    "fixed",
			Targets: []string{"desktop"},
			Title:   localizedText{English: "Desktop fix", Chinese: "桌面端修复"},
			Body:    localizedText{English: "Body.", Chinese: "正文。"},
		}},
	}
	markdown := renderReleaseMarkdown(release, "zh-CN")
	if !strings.Contains(markdown, "CLI 用户可按需跳过") {
		t.Fatalf("empty CLI section missing actionable guidance:\n%s", markdown)
	}
}
