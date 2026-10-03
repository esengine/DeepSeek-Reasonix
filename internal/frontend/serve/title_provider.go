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
}

func (s *Server) currentTitleProvider() titleProviderState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.titleModel
}

func (s *Server) initTitleProvider() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	model := titleProviderState{}
	if ref := strings.TrimSpace(cfg.Agent.TitleModel); ref != "" {
		entry, ok := cfg.ResolveModel(ref)
		if !ok || !entry.Configured() {
			return fmt.Errorf("title model %q is not configured", ref)
		}
		entry.Effort = cfg.Agent.RoleEfforts["title"]
		prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
		if err != nil {
			return err
		}
		model = titleProviderState{
			prov:  prov,
			price: entry.Price,
			ref:   entry.Name + "/" + entry.Model,
			sink:  stats.NewRecorder(event.Discard, config.StatsDir(), "serve"),
		}
	}
	s.mu.Lock()
	s.titleModel = model
	s.mu.Unlock()
	return nil
}
