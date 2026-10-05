package boot

import (
	"net/http"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/laya"
	"reasonix/internal/tools/builtin"
)

// addSystemOne wires the decision tool to whichever model the decision role
// names. The backend is an ordinary provider entry on a decision wire, so it is
// added, keyed and edited exactly where every other model is; nothing here
// knows a vendor's name.
func addSystemOne(reg *tool.Registry, enabled []string, cfg *config.Config, client *http.Client) {
	if len(enabled) != 0 && !slices.Contains(enabled, "system_one") {
		return
	}
	spec := builtin.SystemOneSpec{}
	if entry, ok := cfg.ResolveModel(cfg.Agent.DecisionModel); ok && config.AnswersFor(entry.Kind) == config.AnswersDecision {
		spec.Name, spec.BaseURL, spec.Model = entry.Name, entry.BaseURL, entry.Model
		spec.APIKey = func() string { return decisionKey(entry) }
	}
	// The local runtime is not an endpoint — it runs a Python process and has
	// neither an address nor a key — so it stays a declared backend rather than
	// pretending to be a source.
	if lc := cfg.Tools.SystemOne.Laya; lc.Local {
		spec.LayaLocal = &laya.LocalClient{Python: lc.Python, Model: lc.Model}
	}
	if !builtin.SystemOneConfigured(spec) {
		return
	}
	spec.HTTP = client
	reg.Add(builtin.NewSystemOne(spec))
}

// decisionKey reads the source's key the way every provider does, then falls
// back to the process environment. A provider entry resolves from the Reasonix
// credential store alone; this setting was documented as reading an exported
// variable, and migrating it must not quietly stop honouring one.
func decisionKey(entry *config.ProviderEntry) string {
	if key := strings.TrimSpace(entry.APIKey()); key != "" {
		return key
	}
	name := strings.TrimSpace(entry.APIKeyEnv)
	if name == "" {
		return ""
	}
	return config.ResolveCredentialForRoot(".", name).Value
}
