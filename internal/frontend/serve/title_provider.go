package serve

import (
	"fmt"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/stats"
)

type titleProviderState struct {
	prov  provider.Provider
	price *provider.Pricing
	ref   string
	sink  event.Sink
	// quote prices a title call at emit time. Titles record into their own
	// recorder instead of the assembled sink, so no CostQuoteSink sits in front
	// of that recorder to fill the quote in; see tryRequestTitle.
	quote *event.QuoteContext
}

func (s *Server) currentTitleProvider() titleProviderState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.titleModel
}

func (s *Server) initTitleProvider() error {
	cfg, err := config.LoadForRootReadOnly(".")
	if err != nil {
		return err
	}
	ref := strings.TrimSpace(cfg.Agent.TitleModel)
	if ref == "" {
		if start, ok := cfg.ResolveStartupChatModel(); ok {
			ref = start.Ref
		}
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok || !entry.Configured() {
		return fmt.Errorf("title model %q is not configured", ref)
	}
	// Titles are deliberately plain-chat requests: the short output budget is
	// for the visible title, not hidden reasoning. Do not inherit the role effort
	// from the selected model or agent.role_efforts.title.
	entry.Effort = ""
	entry.DefaultEffort = ""
	entry.SupportedEfforts = nil
	entry.Thinking = "disabled"
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		return err
	}
	model := titleProviderState{prov: prov, price: entry.Price, ref: entry.Name + "/" + entry.Model,
		sink:  stats.NewRecorder(event.Discard, config.StatsDir(), "serve"),
		quote: boot.QuoteContextFor(cfg)}
	s.mu.Lock()
	s.titleModel = model
	s.mu.Unlock()
	return nil
}
