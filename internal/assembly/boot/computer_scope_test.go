package boot

import (
	"testing"

	"reasonix/internal/contract/config"
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
