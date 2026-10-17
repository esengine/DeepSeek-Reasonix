package control

import (
	"context"
	"slices"

	"reasonix/internal/contract/config"
	"reasonix/internal/tools/websearch"
)

// WebAccessSettings is where web_search and web_fetch may send things, as the
// user file holds it, with the choices a window can offer. The lists are
// ordered: the first backend is tried first, and a paid one only after the
// ones before it failed. The paid-allowed lists are the standing consent: a
// backend named there is used without asking each time.
type WebAccessSettings struct {
	SearchBackends      []string `json:"search_backends"`
	SearxngURL          string   `json:"searxng_url"`
	SearchPaidAllowed   []string `json:"search_paid_allowed"`
	FetchBackends       []string `json:"fetch_backends"`
	FetchPaidAllowed    []string `json:"fetch_paid_allowed"`
	KnownSearchBackends []string `json:"known_search_backends"`
	KnownFetchBackends  []string `json:"known_fetch_backends"`
	PaidBackends        []string `json:"paid_backends"`
	// DeepSeekAPIReady is whether a configured provider holds a key for the
	// official DeepSeek API, which the paid backend needs.
	DeepSeekAPIReady bool   `json:"deepseek_api_ready"`
	Path             string `json:"path"`
}

// WebAccessPatch carries only the fields a request set; the rest stay as the
// file has them. An empty list is a value: it switches the chain off.
type WebAccessPatch struct {
	SearchBackends    *[]string `json:"search_backends"`
	SearxngURL        *string   `json:"searxng_url"`
	SearchPaidAllowed *[]string `json:"search_paid_allowed"`
	FetchBackends     *[]string `json:"fetch_backends"`
	FetchPaidAllowed  *[]string `json:"fetch_paid_allowed"`
}

// Empty reports whether the patch sets nothing.
func (p WebAccessPatch) Empty() bool {
	return p.SearchBackends == nil && p.SearxngURL == nil && p.SearchPaidAllowed == nil && p.FetchBackends == nil && p.FetchPaidAllowed == nil
}

// WebAccessSettings reads the user file, so a window shows what the file says
// rather than what this session was built with.
func (c *Controller) WebAccessSettings() WebAccessSettings {
	path := config.UserConfigPath()
	return webAccessView(config.LoadForEdit(path), path)
}

// SaveWebAccessSettings validates the whole patch before writing any of it, so a
// refused request leaves the file as it was. A saved chain is built into the
// tools of panes opened afterwards; a pane already open keeps the one it has.
func (c *Controller) SaveWebAccessSettings(patch WebAccessPatch) (WebAccessSettings, error) {
	next := config.WebAccessSettings{}
	var err error
	set := func(dst *[]string, src *[]string, allowed []string) {
		if err != nil || src == nil {
			return
		}
		*dst, err = config.NormalizeWebBackends(*src, allowed)
	}
	set(&next.WebSearchBackends, patch.SearchBackends, config.KnownSearchBackends())
	set(&next.WebSearchPaidAllowed, patch.SearchPaidAllowed, config.PaidWebBackends())
	set(&next.WebFetchBackends, patch.FetchBackends, config.KnownFetchBackends())
	set(&next.WebFetchPaidAllowed, patch.FetchPaidAllowed, config.PaidWebBackends())
	if err == nil && patch.SearxngURL != nil && *patch.SearxngURL != "" {
		err = config.ValidateSearxngURL(*patch.SearxngURL)
	}
	if err != nil {
		return WebAccessSettings{}, err
	}

	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	tools := &cfg.Tools.WebAccessSettings
	if patch.SearchBackends != nil {
		tools.WebSearchBackends = next.WebSearchBackends
	}
	if patch.SearchPaidAllowed != nil {
		tools.WebSearchPaidAllowed = next.WebSearchPaidAllowed
	}
	if patch.FetchBackends != nil {
		tools.WebFetchBackends = next.WebFetchBackends
	}
	if patch.FetchPaidAllowed != nil {
		tools.WebFetchPaidAllowed = next.WebFetchPaidAllowed
	}
	if patch.SearxngURL != nil {
		tools.SearxngURL = *patch.SearxngURL
	}
	if err := cfg.SaveTo(path); err != nil {
		return WebAccessSettings{}, err
	}
	return webAccessView(cfg, path), nil
}

func webAccessView(cfg *config.Config, path string) WebAccessSettings {
	_, _, apiErr := cfg.ResolveDeepSeekAPI()
	w := cfg.Tools.WebAccessSettings
	return WebAccessSettings{
		SearchBackends: slices.Clone(orEmpty(w.WebSearchBackends)), SearxngURL: w.SearxngURL,
		SearchPaidAllowed: slices.Clone(orEmpty(w.WebSearchPaidAllowed)), FetchBackends: slices.Clone(orEmpty(w.WebFetchBackends)),
		FetchPaidAllowed:    slices.Clone(orEmpty(w.WebFetchPaidAllowed)),
		KnownSearchBackends: config.KnownSearchBackends(), KnownFetchBackends: config.KnownFetchBackends(),
		PaidBackends: config.PaidWebBackends(), DeepSeekAPIReady: apiErr == nil, Path: path,
	}
}

func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// ProbeSearxng sends one test search to the address a window has in its field, or
// to the saved one when that is empty, and reports why the instance did not
// answer. The address is validated first and nothing is saved.
func (c *Controller) ProbeSearxng(ctx context.Context, address string) (websearch.SearxngProbe, error) {
	if address == "" {
		address = c.WebAccessSettings().SearxngURL
	}
	return websearch.ProbeSearxng(ctx, address)
}
