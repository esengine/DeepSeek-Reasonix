package responses

import "testing"

// TestExtractMarkdownSourcesCoversCommonShapes：来源提取覆盖三种常见
// DeepSeek web_search markdown 形态（## 来源 / 冒号行 / 无 URL 描述）。
func TestExtractMarkdownSourcesCoversCommonShapes(t *testing.T) {
	md := `摘要：石油运输咽喉。

## 来源
- 新加坡海事及港务管理局：https://www.mpa.gov.sg/
- Reuters：https://www.reuters.com/markets/

正文后续...`
	got := extractMarkdownSources(md)
	if len(got) != 2 {
		t.Fatalf("sources = %d, want 2 (标题行: 标题+URL)", len(got))
	}
	if got[0].Title != "新加坡海事及港务管理局" || got[0].URL != "https://www.mpa.gov.sg/" {
		t.Fatalf("src[0] = %+v", got[0])
	}
	if got[1].URL != "https://www.reuters.com/markets/" {
		t.Fatalf("src[1] = %+v", got[1])
	}

	// 冒号行形态：数据来源：xxx（无 URL 描述）
	col := extractMarkdownSources("结论：xxx\n数据来源：新加坡海事及港务管理局 MPA 2026年1月发布\n")
	if len(col) != 1 || col[0].Title != "新加坡海事及港务管理局 MPA 2026年1月发布" {
		t.Fatalf("colon shape = %+v", col)
	}

	// 无来源段：返回空
	none := extractMarkdownSources("普通正文，无来源段落。")
	if len(none) != 0 {
		t.Fatalf("no-source = %+v", none)
	}
}

// TestDistillPreservesStructuredSources：structured JSON 形态的来源保留
// （含 domain/credibility 字段——P3 门控数据）。
func TestDistillPreservesStructuredSources(t *testing.T) {
	text := `{"sources":[{"title":"MPA","url":"https://www.mpa.gov.sg/","snippet":"官方公告"}],"summary":"测试"}`
	entry := DistillEntry("测试查询", text, 100, "simple")
	if len(entry.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(entry.Sources))
	}
	if entry.Sources[0].URL != "https://www.mpa.gov.sg/" || entry.Sources[0].Title != "MPA" || entry.Sources[0].Snippet != "官方公告" {
		t.Fatalf("src = %+v (Domain 由保存路径 quality 评分填充，此处不设)", entry.Sources[0])
	}
}
