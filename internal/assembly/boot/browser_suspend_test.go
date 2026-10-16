package boot

import (
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

// A host with no browser engine must not offer the browser tools at all.
//
// Binding them to a nil session was not enough, and this is the regression the
// check exists for: `addBuiltins` seeds every registered built-in first, so the
// three browser tools were already in the registry when `bindBrowser` declined
// to bind them. The unbound copies stayed, the model saw them offered, called
// one, and was told to install a browser — which on Android is the one platform
// where it cannot. Reported from a phone as every browser call answering
// `browser.engine_missing`.
func TestBindBrowserKeepsToolsOutOfTheSchemaOnAndroid(t *testing.T) {
	// Not a phone here, so the host check has to be told what to answer.
	restore := browserGoos
	browserGoos = func() string { return "android" }
	defer func() { browserGoos = restore }()

	reg := tool.NewRegistry()
	for _, t := range builtin.BrowserTools(nil) {
		reg.Add(t)
	}
	if got := browserToolNames(reg); len(got) != 3 {
		t.Fatalf("browser tools seeded = %v, want all three in the registry first", got)
	}

	bindBrowser(reg, &config.Config{}, "/tmp/workspace", nil)

	if got := browserToolNames(reg); len(got) != 0 {
		t.Fatalf("browser tools still offered on a host with no engine: %v", got)
	}
}

// And they stay out. A later Add — an MCP handshake, a preset binding its own
// set — must not put the unbound copies back, which is why the registry
// suspends the names rather than only removing them.
func TestSuspendedBrowserToolsStayOut(t *testing.T) {
	reg := tool.NewRegistry()
	suspendBrowserTools(reg)

	for _, tool := range builtin.BrowserTools(nil) {
		reg.Add(tool)
	}
	if got := browserToolNames(reg); len(got) != 0 {
		t.Fatalf("a later Add put the browser tools back: %v", got)
	}
}

func browserToolNames(reg *tool.Registry) []string {
	want := map[string]bool{}
	for _, t := range builtin.BrowserTools(nil) {
		want[t.Name()] = true
	}
	var out []string
	for _, name := range reg.Names() {
		if want[name] {
			out = append(out, name)
		}
	}
	return out
}
