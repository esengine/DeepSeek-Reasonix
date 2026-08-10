package memory

import "testing"

// The shadow contract: V2 rankings ride the result for telemetry and never
// change what production recall serves.
func TestAutoRecallAttachesShadowWithoutChangingProduction(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-sym", Name: "retry-config", Title: "Retry configuration",
		Description: "Where retry limits live", Type: TypeProject, Scope: FactScopeProject,
		Body: "Configure RetryPolicy.MaxBackoffMs in policy/retry.toml.",
	})
	result := AutoRecall(store, "where do I configure retry limits policy", RecallOptions{})
	if len(result.Hits) == 0 {
		t.Fatalf("production recall should hit: %+v", result)
	}
	if len(result.ShadowHits) == 0 || result.ShadowHits[0].ID != "mem-sym" {
		t.Fatalf("shadow ranking must ride the result: %+v", result.ShadowHits)
	}
	if result.Block() == "" || len(result.Block()) < 10 {
		t.Fatal("production block must be unaffected")
	}
	// The V2 code-symbol split finds what V1 unigrams cannot.
	segQuery := AutoRecall(store, "max backoff tuning", RecallOptions{})
	if len(segQuery.ShadowHits) == 0 {
		t.Fatalf("V2 shadow should match camel-case segments: %+v", segQuery)
	}
}
