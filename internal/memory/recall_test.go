package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRecallToolSearchesSavedMemories(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	saveMemory(t, store, Memory{
		Name:        "cache-first-history",
		Title:       "Cache first history",
		Description: "History retrieval must preserve prompt cache stability",
		Type:        TypeProject,
		Body:        "Use a read-only BM25 retrieval tool instead of injecting dynamic history into the system prompt.",
	})
	saveMemory(t, store, Memory{
		Name:        "frontend-colors",
		Description: "Dashboard color preference",
		Type:        TypeUser,
		Body:        "Avoid one-note palettes.",
	})

	tl := NewRecallTool(store)
	if tl.Name() != "memory" || !tl.ReadOnly() {
		t.Fatalf("unexpected tool identity: name=%q readonly=%v", tl.Name(), tl.ReadOnly())
	}
	if !json.Valid(tl.Schema()) {
		t.Fatal("memory schema is not valid JSON")
	}

	out, err := tl.Execute(context.Background(), []byte(`{"operation":"search","query":"BM25 prompt cache","limit":5}`))
	if err != nil {
		t.Fatalf("Execute search: %v", err)
	}
	if !strings.Contains(out, "cache-first-history") {
		t.Fatalf("search output missing expected memory:\n%s", out)
	}
	if strings.Contains(out, "frontend-colors") {
		t.Fatalf("unrelated memory should not match strongly enough:\n%s", out)
	}
}

func TestRecallToolSchemaIsCacheStable(t *testing.T) {
	tl := NewRecallTool(Store{Dir: t.TempDir()})
	if got, want := tl.Description(), "Search, list, and read saved project memories. Use this before saving a new memory to avoid duplicates, and when a saved memory from the index looks relevant but needs its full body. This tool is read-only; use remember to save or update a memory, and forget to delete one."; got != want {
		t.Fatalf("memory description changed; this is provider-visible and affects prompt-cache shape.\nwant: %q\n got: %q", want, got)
	}
	const wantSchema = `{
		"type": "object",
		"properties": {
			"operation": {"type": "string", "enum": ["search", "read", "list"], "description": "search ranks saved memories; read returns one full memory by name; list returns the saved-memory index."},
			"query": {"type": "string", "description": "Search query for operation=search."},
			"name": {"type": "string", "description": "Memory slug for operation=read, e.g. the name in [Label](name.md)."},
			"type": {"type": "string", "enum": ["user", "feedback", "project", "reference"], "description": "Optional memory type filter for search or list."},
			"limit": {"type": "integer", "description": "Maximum search/list results to return, default 8, max 20."}
		},
		"required": ["operation"]
	}`
	if got := string(tl.Schema()); got != wantSchema {
		t.Fatalf("memory schema changed; this is provider-visible and affects prompt-cache shape.\nwant:\n%s\n got:\n%s", wantSchema, got)
	}
}

func TestRecallToolDropsCommonWordNoise(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	saveMemory(t, store, Memory{
		Name:        "rare-cache-rule",
		Description: "Rare synthesis-cache rule",
		Type:        TypeProject,
		Body:        "rareterm common common common",
	})
	for i := 0; i < 12; i++ {
		saveMemory(t, store, Memory{
			Name:        "common-note-" + string(rune('a'+i)),
			Description: "Common note",
			Type:        TypeProject,
			Body:        "common",
		})
	}

	out, err := NewRecallTool(store).Execute(context.Background(), []byte(`{"operation":"search","query":"rareterm common","limit":20}`))
	if err != nil {
		t.Fatalf("Execute search: %v", err)
	}
	if !strings.Contains(out, "rare-cache-rule") {
		t.Fatalf("top rare hit missing:\n%s", out)
	}
	if strings.Contains(out, "common-note-") {
		t.Fatalf("common-word-only noise should be dropped:\n%s", out)
	}
}

func TestRecallToolNoResultsGuidesFallbackSearches(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	out, err := NewRecallTool(store).Execute(context.Background(), []byte(`{"operation":"search","query":"postgres://host:5433"}`))
	if err != nil {
		t.Fatalf("Execute search: %v", err)
	}
	for _, want := range []string{"0 results does not prove", "Retry with 1-3 distinctive terms", "use the history tool"} {
		if !strings.Contains(out, want) {
			t.Fatalf("no-result output missing %q:\n%s", want, out)
		}
	}
}

func TestRecallToolExcludesArchivedMemories(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	saveMemory(t, store, Memory{
		Name:        "stale-synthesis-cache",
		Description: "Stale synthesis-cache conclusion",
		Type:        TypeProject,
		Body:        "This archived conclusion should no longer affect agent recall.",
	})
	if _, err := store.Archive("stale-synthesis-cache"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	tl := NewRecallTool(store)
	for _, args := range []string{
		`{"operation":"search","query":"stale synthesis cache","limit":5}`,
		`{"operation":"list"}`,
	} {
		out, err := tl.Execute(context.Background(), []byte(args))
		if err != nil {
			t.Fatalf("Execute(%s): %v", args, err)
		}
		if strings.Contains(out, "stale-synthesis-cache") {
			t.Fatalf("archived memory leaked into active recall for %s:\n%s", args, out)
		}
	}
	if _, err := tl.Execute(context.Background(), []byte(`{"operation":"read","name":"stale-synthesis-cache"}`)); err == nil {
		t.Fatal("read should not find archived memory as active memory")
	}
}

func TestRecallToolReadsMemoryByName(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	saveMemory(t, store, Memory{
		Name:        "user-prefers-tabs",
		Title:       "Prefers tabs",
		Description: "User prefers tabs for indentation",
		Type:        TypeUser,
		Body:        "Use tabs unless the repository style clearly says otherwise.",
	})

	out, err := NewRecallTool(store).Execute(context.Background(), []byte(`{"operation":"read","name":"user-prefers-tabs"}`))
	if err != nil {
		t.Fatalf("Execute read: %v", err)
	}
	for _, want := range []string{"Memory user-prefers-tabs", "type: user", "Use tabs"} {
		if !strings.Contains(out, want) {
			t.Fatalf("read output missing %q:\n%s", want, out)
		}
	}
}

func TestRecallToolListsAndFiltersByType(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	saveMemory(t, store, Memory{Name: "one", Description: "project fact", Type: TypeProject, Body: "body"})
	saveMemory(t, store, Memory{Name: "two", Description: "user fact", Type: TypeUser, Body: "body"})

	out, err := NewRecallTool(store).Execute(context.Background(), []byte(`{"operation":"list","type":"user"}`))
	if err != nil {
		t.Fatalf("Execute list: %v", err)
	}
	if !strings.Contains(out, "two") || strings.Contains(out, "one") {
		t.Fatalf("type filter did not apply:\n%s", out)
	}
}

func TestRecallToolValidatesInputs(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	tl := NewRecallTool(store)
	if _, err := tl.Execute(context.Background(), []byte(`{"operation":"search"}`)); err == nil {
		t.Fatal("search without query should fail")
	}
	if _, err := tl.Execute(context.Background(), []byte(`{"operation":"read"}`)); err == nil {
		t.Fatal("read without name should fail")
	}
	if _, err := tl.Execute(context.Background(), []byte(`{"operation":"list","type":"unknown"}`)); err == nil {
		t.Fatal("unknown type should fail")
	}
}

func saveMemory(t *testing.T, store Store, m Memory) {
	t.Helper()
	if _, err := store.Save(m); err != nil {
		t.Fatalf("Save(%s): %v", m.Name, err)
	}
}
