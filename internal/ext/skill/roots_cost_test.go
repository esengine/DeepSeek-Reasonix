package skill

import (
	"testing"

	"reasonix/internal/base/testenv"
)

func countRootResolutions(t *testing.T) *int {
	t.Helper()
	calls := 0
	prev := canonicalSkillPath
	canonicalSkillPath = func(path string) string {
		calls++
		return prev(path)
	}
	t.Cleanup(func() { canonicalSkillPath = prev })
	return &calls
}

func TestEachDiscoveryRootIsResolvedOncePerPass(t *testing.T) {
	store := New(Options{HomeDir: testenv.TempDir(t), ProjectRoot: testenv.TempDir(t)})
	calls := countRootResolutions(t)

	roots := store.roots()

	if want := len(roots) + 1 + 4; *calls > want {
		t.Fatalf("one pass over %d roots resolved paths %d times, want at most %d", len(roots), *calls, want)
	}
}

func TestBuiltinSubagentToolsShareOneDiscoveryPass(t *testing.T) {
	store := New(Options{HomeDir: testenv.TempDir(t), ProjectRoot: testenv.TempDir(t)})
	calls := countRootResolutions(t)
	store.roots()
	onePass := *calls
	*calls = 0

	got := BuiltinSubagentTools(store, nil)

	if len(got) == 0 {
		t.Fatal("no built-in subagent tool registered")
	}
	if *calls > onePass {
		t.Fatalf("registering %d built-in subagent tools resolved paths %d times, one discovery pass costs %d", len(got), *calls, onePass)
	}
}
