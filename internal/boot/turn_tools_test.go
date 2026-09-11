package boot

import (
	"slices"
	"testing"

	"reasonix/internal/tool"
)

func TestInteractiveAgentToolsEndNaturallyWithoutFinish(t *testing.T) {
	reg := tool.NewRegistry()
	registerInteractiveAgentTools(reg)

	if _, ok := reg.Get("ask"); !ok {
		t.Fatal("interactive registry is missing ask")
	}
	if _, ok := reg.Get("finish"); ok {
		t.Fatal("interactive registry exposes finish; ordinary turns must end naturally")
	}
	if slices.Contains(HostControlToolNames(), "finish") || slices.Contains(UnifiedProviderToolNames(), "finish") {
		t.Fatal("provider-visible allowlist still contains finish")
	}
}
