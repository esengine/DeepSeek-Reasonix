package provider

import (
	"encoding/json"
	"testing"
)

func TestParseServerSearchHitsIgnoresEncryptedContent(t *testing.T) {
	raw := json.RawMessage(`[{"type":"web_search_result","title":"Change Log","url":"https://api-docs.deepseek.com/updates/","encrypted_content":"xxx"},{"title":"No URL"},{"text":"body only"}]`)
	got := ParseServerSearchHits(raw)
	if len(got) != 2 || got[0].Title != "Change Log" || got[0].URL != "https://api-docs.deepseek.com/updates/" || got[1].Title != "No URL" || got[1].URL != "" {
		t.Fatalf("hits = %#v", got)
	}
}

func TestParseServerSearchQuery(t *testing.T) {
	if got := ParseServerSearchQuery(`{"query":"bitcoin price"}`); got != "bitcoin price" {
		t.Fatalf("query = %q", got)
	}
	if got := ParseServerSearchQuery(`{`); got != "" {
		t.Fatalf("malformed query = %q", got)
	}
}

func TestMergeServerSearch(t *testing.T) {
	var dst []ServerSearchCall
	dst = MergeServerSearch(dst, ServerSearchCall{ID: "s1", Query: "q"})
	dst = MergeServerSearch(dst, ServerSearchCall{ID: "s1", Results: []ServerSearchHit{{Title: "A", URL: "https://a.example"}}, Raw: json.RawMessage(`[{"title":"A"}]`)})
	if len(dst) != 1 || dst[0].Query != "q" || len(dst[0].Results) != 1 || string(dst[0].Raw) != `[{"title":"A"}]` {
		t.Fatalf("merged = %#v", dst)
	}
}

func TestFormatServerSearchFootnotes(t *testing.T) {
	got := FormatServerSearchFootnotes([]ServerSearchHit{{Title: "Change Log", URL: "https://api-docs.deepseek.com/updates/"}, {Title: "No URL"}})
	want := "\n\n- **Change Log**\n  <https://api-docs.deepseek.com/updates/>\n- **No URL**\n"
	if got != want {
		t.Fatalf("footnotes = %q, want %q", got, want)
	}
}

func TestParseServerSearchOutput(t *testing.T) {
	got := ParseServerSearchOutput("Change Log\nhttps://api-docs.deepseek.com/updates/\nNo URL")
	if len(got) != 2 || got[0].Title != "Change Log" || got[0].URL != "https://api-docs.deepseek.com/updates/" || got[1].Title != "No URL" {
		t.Fatalf("parsed = %#v", got)
	}
}

func TestFormatServerSearchOutput(t *testing.T) {
	got := FormatServerSearchOutput([]ServerSearchHit{{Title: "A", URL: "https://a.example"}, {Title: "B"}})
	if got != "A\nhttps://a.example\nB" {
		t.Fatalf("output = %q", got)
	}
}

func TestFormatServerSearchArgs(t *testing.T) {
	if got := FormatServerSearchArgs("bitcoin"); got != `{"query":"bitcoin"}` {
		t.Fatalf("args = %q", got)
	}
	if got := FormatServerSearchArgs("  "); got != "" {
		t.Fatalf("blank query args = %q", got)
	}
}

func TestWalkServerSearchEstimateOmitsEncryptedRaw(t *testing.T) {
	var got []string
	WalkServerSearchEstimate(ServerSearchCall{
		ID: "s1", Query: "latest",
		Results: []ServerSearchHit{{Title: "Change Log", URL: "https://api-docs.deepseek.com/updates/"}},
		Raw:     json.RawMessage(`[{"encrypted_content":"xxx"}]`),
	}, func(s string) {
		if s != "" {
			got = append(got, s)
		}
	})
	if len(got) != 4 || got[0] != "s1" || got[1] != "latest" || got[2] != "Change Log" || got[3] != "https://api-docs.deepseek.com/updates/" {
		t.Fatalf("estimate fields = %#v", got)
	}
}

func TestServerSearchFromResponsesItem(t *testing.T) {
	raw := json.RawMessage(`{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"latest","sources":[{"title":"Change Log","url":"https://api-docs.deepseek.com/updates/"}]}}`)
	got := ServerSearchFromResponsesItem(raw)
	if got == nil || got.ID != "ws_1" || got.Query != "latest" || len(got.Results) != 1 || got.Results[0].Title != "Change Log" {
		t.Fatalf("search = %#v", got)
	}
	if ServerSearchFromResponsesItem(json.RawMessage(`{"id":"x","type":"function_call"}`)) != nil {
		t.Fatal("non-search item should be ignored")
	}
}

func TestServerSearchFromResponsesVisitedPages(t *testing.T) {
	for _, action := range []string{"open_page", "find_in_page"} {
		raw := json.RawMessage(`{"id":"ws_page","type":"web_search_call","status":"completed","action":{"type":"` + action + `","url":"https://api-docs.deepseek.com/guides/thinking_mode"}}`)
		got := ServerSearchFromResponsesItem(raw)
		if got == nil || len(got.Results) != 1 || got.Results[0].URL != "https://api-docs.deepseek.com/guides/thinking_mode" || string(got.Raw) != string(raw) {
			t.Fatalf("lost visited source or replay: %+v", got)
		}
	}
	got := ServerSearchFromResponsesItem(json.RawMessage(`{"id":"ws_search","type":"web_search_call","status":"completed","action":{"type":"search","queries":["first query","second query"]}}`))
	if got == nil || got.Query != "first query\nsecond query" || len(got.Results) != 0 {
		t.Fatalf("unexpected search: %+v", got)
	}
	got = ServerSearchFromResponsesItem(json.RawMessage(`{"id":"ws_failed","type":"web_search_call","status":"failed","action":{"type":"open_page","url":"https://example.com"}}`))
	if got == nil || len(got.Results) != 0 {
		t.Fatalf("failed page counted as source: %+v", got)
	}
}
