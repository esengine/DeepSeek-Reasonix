package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/termrender"
	"reasonix/internal/model/catalog"
)

func testAndRefreshProvider(s *providerSetupSession, p config.ProviderEntry) {
	restore := temporarilySetCredential(p.APIKeyEnv, s.pendingCredentials[p.APIKeyEnv])
	defer restore()
	p.ResolveAPIKeyFromProcessEnvForProbe()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := catalog.FetchModels(ctx, &p)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.M.FetchModelsFailedFmt+"\n", p.Name, err)
		return
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, i18n.M.CustomFetchEmpty)
		return
	}
	items := make([]menuItem, len(models))
	for i, model := range models {
		items[i] = menuItem{name: model}
	}
	idxs, err := selectMany(fmt.Sprintf(i18n.M.SelectModelsLabel, p.Name), items)
	if err != nil || len(idxs) == 0 {
		return
	}
	selected := make([]string, 0, len(idxs))
	for _, idx := range idxs {
		selected = append(selected, models[idx])
	}
	p.Models = selected
	p.Model = ""
	if !containsString(selected, p.Default) {
		p.Default = selected[0]
	}
	if err := s.upsert([]config.ProviderEntry{p}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Printf("  %s\n", termrender.Green(fmt.Sprintf(i18n.M.FetchModelsSuccessFmt, len(models), p.Name)))
}
