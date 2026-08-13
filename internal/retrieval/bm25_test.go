package retrieval

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTokensHandlesLatinAndCJK(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"BM25 检索 cache-first", []string{"bm25", "检索", "cache", "first"}},
		{"数据库迁移", []string{"数据", "据库", "库迁", "迁移"}},
		{"库", []string{"库"}},
		{"用pnpm装依赖", []string{"用", "pnpm", "装依", "依赖"}},
	} {
		got := Tokens(tc.in)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("Tokens(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestBM25ScoreRanksMatchingDocument(t *testing.T) {
	query := Unique(Tokens("prompt cache"))
	doc1 := Counts(Tokens("prompt cache cache stability"))
	doc2 := Counts(Tokens("dashboard colors"))
	df := DocumentFrequency([]map[string]int{doc1, doc2})
	score1 := BM25Score(doc1, 4, query, df, 2, 3)
	score2 := BM25Score(doc2, 2, query, df, 2, 3)
	if score1 <= score2 {
		t.Fatalf("matching score %.3f should exceed unrelated score %.3f", score1, score2)
	}
}

func TestKeepTopRelativeScoreKeepsTopAndDropsWeakTail(t *testing.T) {
	items := []struct {
		name  string
		score float64
	}{
		{name: "top", score: 10},
		{name: "near", score: 2},
		{name: "noise", score: 1.4},
		{name: "zero", score: 0},
	}
	got := KeepTopRelativeScore(items, 0.15, func(item struct {
		name  string
		score float64
	}) float64 {
		return item.score
	})
	if len(got) != 2 || got[0].name != "top" || got[1].name != "near" {
		t.Fatalf("KeepTopRelativeScore() = %#v, want top and near", got)
	}
}

func TestMakeSnippetHandlesMultibyteBoundary(t *testing.T) {
	text := strings.Repeat("前缀", 80) + "稳定结论 synthesis cache " + strings.Repeat("后缀", 80)
	out := MakeSnippet(text, "synthesis cache", QueryTermsForTest(t, "synthesis cache"), 60)
	if !strings.Contains(out, "synthesis cache") {
		t.Fatalf("snippet missing query: %q", out)
	}
	if strings.ContainsRune(out, utf8.RuneError) {
		t.Fatalf("snippet contains replacement rune: %q", out)
	}
}

func QueryTermsForTest(t *testing.T, query string) []string {
	t.Helper()
	terms, err := QueryTerms(query)
	if err != nil {
		t.Fatal(err)
	}
	return terms
}
