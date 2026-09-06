package boot

import (
	"slices"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/websearch"
)

func addWebSearch(reg *tool.Registry, cfg *config.Config, current *config.ProviderEntry, proxy netclient.ProxySpec, sink event.Sink) {
	if cfg.Environment.Offline || (len(cfg.Tools.Enabled) > 0 && !slices.Contains(cfg.Tools.Enabled, "web_search")) {
		return
	}
	entry := cfg.ResolveWebSearchProvider(current)
	if entry == nil {
		return
	}
	reg.Add(&websearch.Tool{
		ReportSourcesStatus: func(status string) {
			if sink != nil && status == provider.SourcesNotProvided {
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: "search_sources_not_provided", Text: i18n.M.SearchSourcesNotProvided})
			}
		},
		Factory: func() (provider.Provider, error) {
			return newProviderWithSearchMode(entry, proxy, nil, false)
		},
		ReportUsage: func(usage *provider.Usage) {
			if sink != nil {
				sink.Emit(event.Event{Kind: event.Usage, ModelRef: modelRefFromEntry(entry), Usage: usage, Pricing: entry.Price, UsageSource: "web-search"})
			}
		},
	})
}
