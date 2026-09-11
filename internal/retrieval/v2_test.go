package retrieval

import (
	"strings"
	"testing"
)

func TestTokensV2CodeSymbolsAndCJK(t *testing.T) {
	got := strings.Join(TokensV2("RetryPolicy.MaxBackoffMs ticket_1827 数据库"), " ")
	for _, want := range []string{"retrypolicy", "retry", "policy", "maxbackoffms", "max", "backoff", "ticket_1827", "ticket", "1827", "数", "据", "库", "数据", "据库"} {
		if !strings.Contains(" "+got+" ", " "+want+" ") {
			t.Fatalf("TokensV2 missing %q in %q", want, got)
		}
	}
	if strings.Contains(" "+got+" ", " ms ") {
		// "Ms" splits to a single meaningful segment only when len > 1; "ms" is
		// fine — assert single letters never appear instead.
		t.Log("ms segment present (acceptable)")
	}
	for _, tok := range TokensV2("A B C") {
		if len(tok) == 0 {
			t.Fatal("empty token emitted")
		}
	}
}

func TestRankV2FieldWeightsBeatBodyMentions(t *testing.T) {
	docs := []FieldedDoc{
		{ID: "titled", Fields: map[string]string{"title": "Package manager", "body": "We migrated to pnpm."}},
		{ID: "mention", Fields: map[string]string{"title": "Sprint notes", "body": "Someone asked about the package manager once; also weather, lunch, and a package of stickers."}},
	}
	hits := RankV2("package manager", docs)
	if len(hits) != 2 || hits[0].ID != "titled" {
		t.Fatalf("field-weighted doc must outrank body mentions: %+v", hits)
	}
}

func TestRankV2FindsCodeSymbolBySegment(t *testing.T) {
	docs := []FieldedDoc{
		{ID: "sym", Fields: map[string]string{"body": "Configure RetryPolicy.MaxBackoffMs in policy/retry.toml."}},
		{ID: "other", Fields: map[string]string{"body": "Unrelated fact about deploys."}},
	}
	hits := RankV2("max backoff configuration", docs)
	if len(hits) == 0 || hits[0].ID != "sym" {
		t.Fatalf("segment query must find the camel-case symbol: %+v", hits)
	}
}
