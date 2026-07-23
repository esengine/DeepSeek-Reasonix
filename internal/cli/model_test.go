package cli

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
)

// TestModelRefsFromConfig verifies the /model picker enumerates configured
// provider/model refs (built-in defaults when no reasonix.toml is present), and
// only those whose provider API key is set.
func TestModelRefsFromConfig(t *testing.T) {
	isolateUserConfig(t) // no reasonix.toml -> built-in default providers
	// Only DeepSeek keyed → MiMo refs must be filtered out.
	if _, err := config.SetCredential("DEEPSEEK_API_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
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

func TestBareModelOpensKeyboardPicker(t *testing.T) {
	isolateUserConfig(t)
	if _, err := config.SetCredential("DEEPSEEK_API_KEY", "test-key"); err != nil {
		t.Fatal(err)
	}
	m := newTestChatTUI()
	m.runModelSubcommand("/model")
	if m.quickPick == nil || m.quickPick.kind != quickPickerModel {
		t.Fatalf("bare /model picker = %+v", m.quickPick)
	}
	if m.renderQuickPicker() == "" || !m.hideComposer() {
		t.Fatal("model picker should render as a modal panel")
	}
	next, _ := m.handleQuickPickerKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if next.(chatTUI).quickPick != nil {
		t.Fatal("Esc should close model picker")
	}
}

// TestModelRefsSkipsUnconfigured verifies that with no provider keys set, the
// picker offers nothing rather than listing models the user can't select.
func TestModelRefsSkipsUnconfigured(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MIMO_API_KEY", "")
	if refs := modelRefs(); len(refs) != 0 {
		t.Errorf("no keys set → no refs, got %v", refs)
	}
}

// TestModelArgCompletion verifies "/model " completes to the configured refs
// through the shared completion path.
func TestModelArgCompletion(t *testing.T) {
	isolateUserConfig(t)
	if _, err := config.SetCredential("DEEPSEEK_API_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	m := newTestChatTUI()
	items, _, ok := m.slashArgItems("/model ")
	if !ok || len(items) == 0 {
		t.Fatalf("/model arg completion should offer refs, ok=%v n=%d", ok, len(items))
	}
}

// TestPersistModelWritesDefaultModel verifies that calling persistModel with a
// "provider/model" ref writes default_model = "<ref>" to the user config file
// in TOML form. This is the fix for the "default model resets on every launch"
// regression: previously /model only mutated the in-memory controller and the
// next startup read the global default.
func TestPersistModelWritesDefaultModel(t *testing.T) {
	isolateUserConfig(t)
	if _, err := config.SetCredential("DEEPSEEK_API_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	t.Setenv("MIMO_API_KEY", "")

	m := newTestChatTUI()
	m.persistModel("deepseek-flash/deepseek-v4-flash")

	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(body), `default_model = "deepseek-flash/deepseek-v4-flash"`) {
		t.Fatalf("saved config missing default_model ref:\n%s", body)
	}
}

// TestPersistModelRejectsUnknownRef verifies that an unresolvable ref is
// silently dropped (logged to slog, not pushed to the TUI notice channel)
// and never lands in the config file. Reason: surface a "persist failed"
// notice on the input box would make /model feel broken to users whose
// stored config doesn't list the exact model ref they picked; the in-
// memory switch still goes through.
func TestPersistModelRejectsUnknownRef(t *testing.T) {
	isolateUserConfig(t)
	if _, err := config.SetCredential("DEEPSEEK_API_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}

	m := newTestChatTUI()
	m.persistModel("ghost/never-existed")

	if _, err := os.Stat(config.UserConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("unknown ref must not create config file, stat err=%v", err)
	}
}
