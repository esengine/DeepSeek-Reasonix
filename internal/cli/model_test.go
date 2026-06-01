package cli

import (
	"strings"
	"testing"
)

// TestModelRefsFromConfig verifies the /model picker enumerates configured
// provider/model refs (built-in defaults when no reasonix.toml is present), and
// only those whose provider API key is set.
func TestModelRefsFromConfig(t *testing.T) {
	t.Chdir(t.TempDir()) // no reasonix.toml → built-in default providers
	// Only DeepSeek keyed → MiMo refs must be filtered out.
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("MIMO_API_KEY", "")
	refs := modelRefs()
	if len(refs) == 0 {
		t.Fatal("expected default provider/model refs, got none")
	}
	for _, r := range refs {
		if !strings.Contains(r, "/") {
			t.Errorf("ref %q should be provider/model", r)
		}
		if strings.HasPrefix(r, "mimo") {
			t.Errorf("ref %q from a provider without an API key should be filtered out", r)
		}
	}
}

// TestModelRefsSkipsUnconfigured verifies that with no provider keys set, the
// picker offers nothing rather than listing models the user can't select.
func TestModelRefsSkipsUnconfigured(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MIMO_API_KEY", "")
	if refs := modelRefs(); len(refs) != 0 {
		t.Errorf("no keys set → no refs, got %v", refs)
	}
}

// TestModelArgCompletion verifies "/model " completes to the configured refs
// through the shared completion path.
func TestModelArgCompletion(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	m := newTestChatTUI()
	items, _, ok := m.slashArgItems("/model ")
	if !ok || len(items) == 0 {
		t.Fatalf("/model arg completion should offer refs, ok=%v n=%d", ok, len(items))
	}
}
