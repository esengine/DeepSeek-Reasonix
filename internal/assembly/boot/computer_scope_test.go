package boot

import (
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/computer"
	"reasonix/internal/tools/builtin"
)

func TestComputerDriverBindingIsScopedToTheAssembly(t *testing.T) {
	driver := computer.NewDriver("/unused", nil)
	defer driver.Close()
	bound, headless := tool.NewRegistry(), tool.NewRegistry()
	bindMachineTools(bound, &config.Config{}, "", nil, driver)
	bindMachineTools(headless, &config.Config{}, "", nil, nil)
	for _, name := range ComputerToolNames() {
		got, ok := bound.Get(name)
		if !ok || !builtin.ComputerBound(got) {
			t.Fatalf("explicit driver did not bind %s", name)
		}
		if _, ok := headless.Get(name); ok {
			t.Fatalf("unrelated assembly inherited %s", name)
		}
	}
}

func TestComputerDriverReachesStudioUnattendedChildSchema(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Chdir(root)
	rec := &browserScriptProvider{}
	provider.Register("cua-child-fixture", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeUserConfig(t, "")
	writeFile(t, root, "reasonix.toml", `
 default_model = "test-model"
 tool_approval = "yolo"
 [agent]
 system_prompt = "BASE"
 [codegraph]
 enabled = false
 [[providers]]
 name = "test-model"
 kind = "cua-child-fixture"
 model = "x"
 `)
	approveWorkspace(t, root)
	driver := computer.NewDriver("/unused", nil)
	defer driver.Close()
	_, _, err := runUnattended(t.Context(), Options{ComputerDriver: driver}, event.Discard, "deny", root, "answer briefly", provider.Selection{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	requests := agentRequests(rec.requests())
	if len(requests) == 0 {
		t.Fatal("child sent no provider request")
	}
	for _, name := range ComputerToolNames() {
		if !toolNames(requests[0])[name] {
			t.Fatalf("child schema lost %s", name)
		}
	}
}
