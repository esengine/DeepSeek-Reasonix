package memory

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/retrieval"
)

type recallQualityMetrics struct {
	recallAtK     float64
	duplicateRate float64
	injectedTerms int
	top1          string
}

func seedRecallQualityCorpus(t testing.TB) (Store, time.Time) {
	t.Helper()
	store := Store{Dir: t.TempDir()}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	memories := []Memory{
		{Name: "cache-prefix-a", Title: "Prefix cache tuning", Description: "cache prefix hit rate tuning", Type: TypeProject, Body: "Keep stable tool schemas and inspect cache hit metrics.", SourceKind: "remember_tool"},
		{Name: "cache-prefix-b", Title: "Prefix cache tuning", Description: "cache prefix hit rate tuning", Type: TypeProject, Body: "Keep stable tool schemas and inspect cache hit metrics.", SourceKind: "remember_tool"},
		{Name: "cache-prefix-c", Title: "Prefix cache tuning", Description: "cache prefix hit rate tuning", Type: TypeProject, Body: "Keep stable tool schemas and inspect cache hit metrics.", SourceKind: "remember_tool"},
		{Name: "cache-debugging", Title: "Cache miss diagnostics", Description: "debug cache prefix misses", Type: TypeProject, Body: "Inspect request headers, provider logs, and dynamic prompt tails.", SourceKind: "user_confirmed"},
		{Name: "zh-command", Title: "中文命令参数", Description: "排查 sandbox-exec 命令失败", Type: TypeFeedback, Body: "检查命令参数、文件路径和错误短语，不要重复失败命令。", SourceKind: "user_confirmed"},
		{Name: "truth-locked-old", Title: "Durable port truth", Description: "FRP remote port 10023 belongs to mini SSH", Type: TypeProject, Body: "This old confirmed fact remains true and may rank lower, but must not be discarded.", SourceKind: "user_confirmed", CreatedAt: now.AddDate(-3, 0, 0), LastConfirmedAt: now.AddDate(-3, 0, 0)},
	}
	for _, m := range memories {
		if _, err := store.SaveAt(m, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.SaveAt(Memory{Name: "archived-cache", Title: "Prefix cache tuning", Description: "cache prefix hit rate tuning", Type: TypeProject, Body: "archived duplicate", SourceKind: "remember_tool"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Archive("archived-cache"); err != nil {
		t.Fatal(err)
	}
	return store, now
}

func measureRecallQuality(hits []memoryHit, relevant map[string]bool) recallQualityMetrics {
	metrics := recallQualityMetrics{}
	if len(hits) > 0 {
		metrics.top1 = hits[0].Memory.Name
	}
	seenRelevant := 0
	for _, hit := range hits {
		if relevant[hit.Memory.Name] {
			seenRelevant++
		}
		metrics.injectedTerms += len(retrieval.Tokens(memorySearchText(hit.Memory)))
	}
	if len(relevant) > 0 {
		metrics.recallAtK = float64(seenRelevant) / float64(len(relevant))
	}
	duplicatePairs := 0
	pairs := 0
	for i := range hits {
		for j := i + 1; j < len(hits); j++ {
			pairs++
			if tokenJaccard(hits[i].terms, hits[j].terms) >= defaultDuplicateThreshold {
				duplicatePairs++
			}
		}
	}
	if pairs > 0 {
		metrics.duplicateRate = float64(duplicatePairs) / float64(pairs)
	}
	return metrics
}

func TestMemoryRecallQualityCorpus(t *testing.T) {
	store, now := seedRecallQualityCorpus(t)
	off := false
	legacy, err := searchMemoriesWithOptions(context.Background(), store, "cache prefix hit rate diagnostics", "", 4, RecallOptions{Diversity: &off, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	diverse, err := searchMemoriesWithOptions(context.Background(), store, "cache prefix hit rate diagnostics", "", 4, RecallOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	relevant := map[string]bool{"cache-prefix-a": true, "cache-debugging": true}
	before := measureRecallQuality(legacy, relevant)
	after := measureRecallQuality(diverse, relevant)
	if after.top1 != before.top1 || after.top1 != "cache-prefix-a" {
		t.Fatalf("top-1 changed: before=%+v after=%+v", before, after)
	}
	if after.recallAtK < before.recallAtK || after.recallAtK != 1 {
		t.Fatalf("Recall@K regressed: before=%+v after=%+v", before, after)
	}
	if after.duplicateRate >= before.duplicateRate || after.duplicateRate != 0 {
		t.Fatalf("duplicate rate did not improve: before=%+v after=%+v", before, after)
	}
	if after.injectedTerms >= before.injectedTerms {
		t.Fatalf("injected token proxy did not shrink: before=%+v after=%+v", before, after)
	}
	zh, err := searchMemoriesWithOptions(context.Background(), store, "sandbox-exec 命令失败 文件路径", "", 3, RecallOptions{Now: func() time.Time { return now }})
	if err != nil || len(zh) == 0 || zh[0].Memory.Name != "zh-command" {
		t.Fatalf("CJK/error phrase recall failed: hits=%+v err=%v", zh, err)
	}
	old, err := searchMemoriesWithOptions(context.Background(), store, "FRP remote port 10023 mini SSH", "", 3, RecallOptions{Now: func() time.Time { return now }})
	if err != nil || len(old) == 0 || old[0].Memory.Name != "truth-locked-old" || old[0].StalenessFactor >= 1 {
		t.Fatalf("old truth was lost or not explained: hits=%+v err=%v", old, err)
	}
	for _, hit := range append(append([]memoryHit{}, diverse...), append(zh, old...)...) {
		if hit.Memory.Name == "archived-cache" {
			t.Fatal("archived memory entered active recall")
		}
	}
}

func BenchmarkMemoryRecallQualityCorpus(b *testing.B) {
	store, now := seedRecallQualityCorpus(b)
	options := RecallOptions{Now: func() time.Time { return now }}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := searchMemoriesWithOptions(context.Background(), store, "cache prefix hit rate diagnostics", "", 4, options); err != nil {
			b.Fatal(err)
		}
	}
}
