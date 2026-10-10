package boot

import (
	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/websearch"
)

var searchUnavailableNotice = map[config.WebSearchReason]string{
	config.WebSearchBadRef:        event.NoticeCodeWebSearchModelBadRef,
	config.WebSearchNotAdded:      event.NoticeCodeWebSearchModelNotAdded,
	config.WebSearchModelRemoved:  event.NoticeCodeWebSearchModelRemoved,
	config.WebSearchUnsupported:   event.NoticeCodeWebSearchModelUnsupported,
	config.WebSearchNoCredentials: event.NoticeCodeWebSearchModelNoCredentials,
}

func addWebSearch(reg *tool.Registry, cfg *config.Config, proxy netclient.ProxySpec, sink event.Sink) {
	selection := cfg.ResolveWebSearch()
	if selection.Err != nil {
		reason, _ := config.WebSearchReasonOf(selection.Err)
		report(sink, event.Event{Level: event.LevelInfo, Code: searchUnavailableNotice[reason],
			Text: "The selected web search model is unavailable. Choose another search model or automatic selection in Model preferences.", Detail: cfg.Agent.WebSearchModel})
	}
	entry := selection.Entry
	if entry == nil {
		return
	}
	reg.Add(&websearch.Tool{
		Factory: func() (provider.Provider, error) {
			cfg := providerConfig(entry, proxy)
			cfg.Extra["web_search"] = true
			cfg.Extra["reject_redirects"] = true
			return provider.New(entry.Kind, cfg)
		},
		ReportUsage: func(usage *provider.Usage) {
			if sink != nil {
				sink.Emit(event.Event{Kind: event.Usage, ModelRef: modelRefFromEntry(entry), Usage: usage, Pricing: entry.Price, UsageSource: "web-search"})
			}
		},
	})
}
