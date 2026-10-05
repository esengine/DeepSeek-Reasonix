package memory

import (
	"strings"
	"testing"
)

// #7995: the stable index and automatic recall must share one world view. On a
// name collision both scopes stay visible with qualified references, and the
// shadowed global entry says which project fact overrides it.
func TestIndexShowsBothScopesWithOverrideAnnotation(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-proj", Name: "deploy-region", Title: "Deploy region",
		Description: "This project deploys to eu-central-1", Type: TypeProject,
		Scope: FactScopeProject, Body: "eu-central-1",
	})
	recallTestWrite(t, store.GlobalDir, Memory{
		ID: "mem-glob", Name: "deploy-region", Title: "Deploy region",
		Description: "Default deploy region", Type: TypeProject,
		Scope: FactScopeGlobal, Body: "us-east-1",
	})

	index := store.Index()
	for _, want := range []string{
		"(project/deploy-region.md)",
		"(global/deploy-region.md)",
		"(overridden by project/deploy-region.md)",
	} {
		if !strings.Contains(index, want) {
			t.Fatalf("index missing %q:\n%s", want, index)
		}
	}
	if strings.Contains(index, "](deploy-region.md)") {
		t.Fatalf("provider index must never use unqualified references:\n%s", index)
	}
	// The project entry (the recall winner) must not carry the annotation.
	for line := range strings.SplitSeq(index, "\n") {
		if strings.Contains(line, "(project/deploy-region.md)") && strings.Contains(line, "overridden") {
			t.Fatalf("winning project entry wrongly annotated: %s", line)
		}
	}
}
