package main

import "reasonix/internal/provider"

// historyServerSearch copies card-visible search fields and drops encrypted
// replay payloads. Those stay on the session message for the next provider turn.
func historyServerSearch(in []provider.ServerSearchCall) []provider.ServerSearchCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]provider.ServerSearchCall, 0, len(in))
	for _, search := range in {
		copied := provider.ServerSearchCall{ID: search.ID, Query: search.Query, SourcesStatus: search.SourcesStatus}
		if len(search.Results) > 0 {
			copied.Results = append([]provider.ServerSearchHit(nil), search.Results...)
		}
		out = append(out, copied)
	}
	return out
}
