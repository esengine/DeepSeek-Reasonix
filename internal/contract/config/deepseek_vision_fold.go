// deepseek_vision_fold.go — retiring the connection that existed only to reach
// an image-taking model, now that the flash model beside it reads images itself.
package config

import (
	"slices"
	"strings"
)

// foldRetiredDeepSeekVisionProvider drops that connection once the entry beside
// it reaches the same model on the same key. Anything the user configured on it
// keeps it: folding would answer their edit by deleting where it lived.
func foldRetiredDeepSeekVisionProvider(c *Config) bool {
	if c == nil {
		return false
	}
	vision, ok := c.Provider(retiredDeepSeekVisionProvider)
	if !ok || !isFoldableDeepSeekVisionEntry(vision) {
		return false
	}
	survivor := deepSeekFlashPeerName(c, vision)
	if survivor == "" {
		return false
	}
	kept := make([]ProviderEntry, 0, len(c.Providers))
	for _, p := range c.Providers {
		if p.Name != retiredDeepSeekVisionProvider {
			kept = append(kept, p)
		}
	}
	c.Providers = kept
	drop := map[string]bool{retiredDeepSeekVisionProvider: true}
	foldProviderAccessInto(c, drop, survivor)
	retargetModelRefs(c, func(ref string) string {
		prov, model, hasModel := strings.Cut(ref, "/")
		if prov != retiredDeepSeekVisionProvider {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			return survivor
		}
		return survivor + "/" + model
	})
	return true
}

// isFoldableDeepSeekVisionEntry reports whether the entry is still the one that
// shipped: the official endpoint, the flash model alone, and no configuration of
// the user's own. Per-model maps count as theirs — they only exist once edited.
func isFoldableDeepSeekVisionEntry(p *ProviderEntry) bool {
	return p != nil &&
		officialProviderHost(p.BaseURL) == "api.deepseek.com" &&
		slices.Equal(p.ModelList(), []string{DeepSeekFlashModel}) &&
		len(p.Prices) == 0 && len(p.ModelOverrides) == 0 &&
		len(p.Headers) == 0 && len(p.ExtraBody) == 0
}

// deepSeekFlashPeerName is the entry that keeps reaching the model after the
// fold. Same host and same key: another account's entry serving the same model
// is not a route this one's callers already have.
func deepSeekFlashPeerName(c *Config, vision *ProviderEntry) string {
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == retiredDeepSeekVisionProvider ||
			officialProviderHost(p.BaseURL) != "api.deepseek.com" ||
			p.APIKeyEnv != vision.APIKeyEnv ||
			!p.HasModel(DeepSeekFlashModel) {
			continue
		}
		return p.Name
	}
	return ""
}

// migrateRetiredDeepSeekModels renames the retired flash models everywhere one
// load can still see them under their own provider — the entries that list them
// and the refs that name them. Both have to happen before a fold renames the
// provider, which is what a ref is matched against.
func migrateRetiredDeepSeekModels(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		changed = migrateRetiredDeepSeekFlashModels(&c.Providers[i]) || changed
	}
	return migrateRetiredDeepSeekModelRefs(c) || changed
}

// migrateRetiredDeepSeekModelRefs points a stored ref at the model its provider
// now lists. The endpoint still answers a retired name, but the entry no longer
// offers one, so the ref would resolve to nothing before it ever reached a wire.
func migrateRetiredDeepSeekModelRefs(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	retargetModelRefs(c, func(ref string) string {
		prov, model, hasModel := strings.Cut(ref, "/")
		if !hasModel {
			bare := bareRetiredDeepSeekRef(c, ref)
			changed = changed || bare != ref
			return bare
		}
		if !slices.Contains(retiredDeepSeekFlashModels, strings.TrimSpace(model)) {
			return ref
		}
		if p, ok := c.Provider(prov); !ok || officialProviderHost(p.BaseURL) != "api.deepseek.com" {
			return ref
		}
		changed = true
		return prov + "/" + DeepSeekFlashModel
	})
	return changed
}

// bareRetiredDeepSeekRef names the official entry that now lists the flash
// model, for a bare retired name nothing else still serves. A bare name has no
// provider to stay with, and the official list stopped offering it.
func bareRetiredDeepSeekRef(c *Config, ref string) string {
	if !slices.Contains(retiredDeepSeekFlashModels, ref) {
		return ref
	}
	if _, served := c.ResolveModel(ref); served {
		return ref
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderHost(p.BaseURL) == "api.deepseek.com" && p.HasModel(DeepSeekFlashModel) {
			return p.Name + "/" + DeepSeekFlashModel
		}
	}
	return ref
}

// retargetModelRefs applies rewrite to every field holding a provider/model ref.
// Missing one leaves a ref pointing at what the others just stopped naming.
func retargetModelRefs(c *Config, rewrite func(string) string) {
	for _, field := range append([]*string{&c.DefaultModel}, c.roleModelRefTargets()...) {
		if ref := strings.TrimSpace(*field); ref != "" {
			*field = rewrite(ref)
		}
	}
	for skill, ref := range c.Agent.SubagentModels {
		if ref = strings.TrimSpace(ref); ref != "" {
			c.Agent.SubagentModels[skill] = rewrite(ref)
		}
	}
}
