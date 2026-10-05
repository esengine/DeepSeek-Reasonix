// systemone_migrate.go — the decision backend moves onto the general mechanism.
package config

import "strings"

// systemOneHostedURL is where the hosted decision backend answered when this
// migration was written; a migration records that, not wherever it moves next.
const systemOneHostedURL = "https://api.typesafe.ai"

// migrateSystemOneToDecisionRole turns the decision settings into what every
// other model already is: a source with an address and a key, and a role naming
// one. Both halves were special — a hosted endpoint and a "gateway" that spoke
// the same wire at another address — so both become ordinary entries. It runs
// once: a second pass finds the fields cleared and the sources already there.
func migrateSystemOneToDecisionRole(c *Config) bool {
	if c == nil {
		return false
	}
	one := c.Tools.SystemOne
	hosted := anySet(one.BaseURL, one.Model, one.APIKeyEnv)
	gateway := strings.TrimSpace(one.Laya.HTTPBaseURL) != ""
	if !hosted && !gateway {
		return false
	}
	if hosted {
		c.adoptDecisionSource("typesafe", one.BaseURL, systemOneHostedURL, one.Model, "system-one", one.APIKeyEnv, "TYPESAFE_API_KEY")
	}
	if gateway {
		// The gateway never carried a model of its own: the old client sent
		// "auto" whatever the Laya section said, and that field belongs to the
		// local runtime, which is staying where it is.
		c.adoptDecisionSource("laya", one.Laya.HTTPBaseURL, "", "auto", "auto", one.Laya.HTTPAPIKeyEnv, "")
	}
	c.Tools.SystemOne.BaseURL = ""
	c.Tools.SystemOne.Model = ""
	c.Tools.SystemOne.APIKeyEnv = ""
	c.Tools.SystemOne.Laya.HTTPBaseURL = ""
	c.Tools.SystemOne.Laya.HTTPAPIKeyEnv = ""
	return true
}

// adoptDecisionSource adds one decision endpoint and points the role at it when
// the role is still unset. A name already taken is left alone: the person's own
// source outranks anything a migration would write over it.
func (c *Config) adoptDecisionSource(name, baseURL, defaultURL, model, defaultModel, keyEnv, defaultKeyEnv string) {
	if _, taken := c.Provider(name); taken {
		return
	}
	entry := ProviderEntry{
		Name:      name,
		Kind:      "typesafe",
		BaseURL:   firstSet(baseURL, defaultURL),
		Models:    []string{firstSet(model, defaultModel)},
		APIKeyEnv: firstSet(keyEnv, defaultKeyEnv),
	}
	if strings.TrimSpace(entry.BaseURL) == "" {
		return
	}
	c.Providers = append(c.Providers, entry)
	if strings.TrimSpace(c.Agent.DecisionModel) == "" {
		c.Agent.DecisionModel = entry.Name + "/" + entry.Models[0]
	}
}

func anySet(values ...string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func firstSet(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
