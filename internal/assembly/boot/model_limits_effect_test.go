package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// A model with its own context_window is compacted against that window, not
// the connection's: asserted as the budget notice reaching the provider request.
func budgetNoticesFor(t *testing.T, kind, model, overrides string) int {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &budgetEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "conn/`+model+`"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "conn"
kind = "`+kind+`"
models = ["small", "large"]
context_window = 1000000
`+overrides)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	for _, prompt := range []string{"start the task", "keep going", "keep going", "keep going", "keep going"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}
	n := 0
	for _, req := range rec.requests() {
		n += countBudgetNotices(req)
	}
	return n
}

func TestEffectModelContextWindowOverrideDrivesCompaction(t *testing.T) {
	const ov = "model_overrides = { small = { context_window = 32000 } }\n"
	if got := budgetNoticesFor(t, "boot-limits-small", "small", ov); got == 0 {
		t.Fatal("the model with a 32000 override was never warned; the connection's window was used")
	}
	if got := budgetNoticesFor(t, "boot-limits-large", "large", ov); got != 0 {
		t.Fatalf("a model without an override was warned %d times; it inherits 1000000", got)
	}
}
