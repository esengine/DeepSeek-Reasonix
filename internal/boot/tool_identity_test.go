package boot

import (
	"context"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/tool"
)

func TestBootRegisteredToolsHaveDiagnosticIdentities(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("identity", testutil.Turn{Text: "done"}))
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	known := map[string]bool{}
	for _, name := range tool.KnownToolNames() {
		known[name] = true
	}
	registered := map[string]bool{}
	for _, entry := range ctrl.AllToolContractEntries() {
		registered[entry.Name] = true
		if !known[entry.Name] {
			t.Errorf("registered tool %q missing diagnostic identity", entry.Name)
		}
	}
	for _, name := range []string{"use_capability", "grep", "set_session_title", "review", "security_review"} {
		if !registered[name] {
			t.Errorf("missing runtime coverage for %s", name)
		}
	}
}
