package memory

import (
	"strings"
	"testing"
)

// CJK bigram selectivity: a query phrase must match a real two-character
// subsequence, not scattered common characters (the retired per-rune scorer
// matched the latter).
func TestAutoRecallCJKBigramsRequireRealSubsequence(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-migration", Name: "db-migration-compat", Title: "数据库迁移兼容性",
		Description: "数据库迁移必须保持向后兼容", Type: TypeProject,
		Scope: FactScopeProject, Body: "所有 schema 变更先加列后删列。",
	})
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-scattered", Name: "warehouse-stats", Title: "仓库统计口径",
		Description: "数值统计以据点仓库的库存为准", Type: TypeProject,
		Scope: FactScopeProject, Body: "库存数字每周核对一次。",
	})

	result := AutoRecall(store, "数据库迁移怎么保证兼容", RecallOptions{})
	if len(result.Hits) == 0 {
		t.Fatalf("AutoRecall missed the real 数据库迁移 fact: %+v", result)
	}
	for _, hit := range result.Hits {
		if hit.Memory.ID == "mem-scattered" {
			t.Fatalf("scattered common characters must not match: %+v", result.Hits)
		}
	}
}

// Keywords bridge the lexical gap AutoRecall cannot cross: the fact's own
// words never appear in the query, its saved aliases do.
func TestAutoRecallMatchesThroughSavedKeywords(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-pnpm", Name: "package-manager", Title: "Package manager is pnpm",
		Description: "The project migrated from npm to pnpm", Type: TypeProject,
		Scope: FactScopeProject, Keywords: "依赖 安装 包管理 install dependencies",
		Body: "Use pnpm for every workspace task; npm and yarn lockfiles are gone.",
	})

	result := AutoRecall(store, "依赖应该用什么工具安装", RecallOptions{})
	if len(result.Hits) != 1 || result.Hits[0].Memory.ID != "mem-pnpm" {
		t.Fatalf("keywords should carry the paraphrased query to the fact, got %+v", result.Hits)
	}
	if !strings.Contains(result.Hits[0].Reason, "依赖") && !strings.Contains(result.Hits[0].Reason, "安装") {
		t.Fatalf("reason should surface the matched alias, got %q", result.Hits[0].Reason)
	}
}

func TestSaveRoundTripsKeywordsAndPreservesThemOnUpdate(t *testing.T) {
	store := recallTestStore(t)
	saved, err := store.SaveWithOptions(Memory{
		Name: "package-manager", Description: "The project uses pnpm",
		Keywords: "依赖 包管理", Body: "pnpm everywhere.",
	}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := store.Read(saved.Memory.ID)
	if !ok || loaded.Keywords != "依赖 包管理" {
		t.Fatalf("keywords lost in round trip: ok=%v %+v", ok, loaded)
	}

	if _, err := store.SaveWithOptions(Memory{
		ID: saved.Memory.ID, Description: "The project uses pnpm 9",
		Body: "pnpm 9 everywhere.",
	}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	updated, ok := store.Read(saved.Memory.ID)
	if !ok || updated.Keywords != "依赖 包管理" {
		t.Fatalf("an update omitting keywords must preserve them, got %+v", updated)
	}
	if updated.Revision != 2 || !strings.Contains(updated.Body, "pnpm 9") {
		t.Fatalf("update did not land: %+v", updated)
	}
}
