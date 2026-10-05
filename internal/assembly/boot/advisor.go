package boot

import (
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/advisor"
)

// addAdvisor offers the advise tool when advisor_model names a model. It is
// decided once at boot, so whether the schema carries it is prefix
// configuration; a model that does not resolve leaves it out with a notice.
func addAdvisor(reg *tool.Registry, cfg *config.Config, proxy netclient.ProxySpec, sink event.Sink) {
	ref := strings.TrimSpace(cfg.Agent.AdvisorModel)
	if ref == "" {
		return
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		report(sink, event.Event{Level: event.LevelWarn, Text: "The advisor was disabled because its model was not found.",
			Detail: fmt.Sprintf("advisor_model %q not found — advise tool not offered", ref)})
		return
	}
	prov, err := NewProviderWithProxy(entry, proxy)
	if err != nil {
		slog.Warn("advisor provider construction failed — advise tool not offered", "model", ref, "err", err)
		report(sink, event.Event{Level: event.LevelWarn, Text: "The advisor was disabled because it could not start.",
			Detail: fmt.Sprintf("advisor construction failed: %v", err)})
		return
	}
	reg.Add(advisor.New(advisor.Spec{Provider: prov, ModelRef: modelRefFromEntry(entry), Pricing: entry.Price, Sink: sink}))
}
