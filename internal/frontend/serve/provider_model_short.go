package serve

import "reasonix/internal/contract/config"

// modelShorts is how each listed model reads with the entry's shared prefix left
// out; a model drawn as it is has no entry.
func modelShorts(p *config.ProviderEntry) map[string]string {
	var out map[string]string
	for _, model := range p.ModelList() {
		if short := p.ModelShort(model); short != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[model] = short
		}
	}
	return out
}
