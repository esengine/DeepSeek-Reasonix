package configbackup

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/instruction"
)

// consentFor is recomputed at apply time against the machine as it is then;
// the preview's answer is only what the person was shown.
func consentFor(it Item, s *Snapshot, local *localState) string {
	if executes(it.Kind) {
		return ConsentExecutes
	}
	switch it.Kind {
	case KindProvider:
		var incoming config.ProviderEntry
		if decodeTOML(it.Data, &incoming) != nil {
			return ConsentEndpoint
		}
		if have, ok := local.providers[it.Name]; ok && endpoints(have) == endpoints(incoming) {
			return ""
		}
		return ConsentEndpoint
	case KindGeneral:
		return generalConsent(it, s, local)
	case KindSecret:
		var d secretData
		if json.Unmarshal(it.Data, &d) != nil {
			return ConsentReplacesSecret
		}
		if config.CredentialStored(d.Key) && config.ResolveCredential(d.Key).Value != d.Value {
			return ConsentReplacesSecret
		}
	case KindMemory:
		var m memoryData
		if json.Unmarshal(it.Data, &m) == nil && len(instruction.ImportTargets(string(m.Data))) > 0 {
			return ConsentImports
		}
	}
	return ""
}

// generalConsent covers the default model: pointing it at a provider this
// machine does not already reach at the same endpoint sends every
// conversation there, whether or not any key goes with it.
func generalConsent(it Item, s *Snapshot, local *localState) string {
	var g generalData
	if json.Unmarshal(it.Data, &g) != nil {
		return ConsentEndpoint
	}
	name, _, ok := strings.Cut(g.DefaultModel, "/")
	if !ok || name == "" {
		return ""
	}
	have, known := local.providers[name]
	if !known {
		return ConsentEndpoint
	}
	for _, other := range s.Items {
		if other.Kind != KindProvider || other.Name != name {
			continue
		}
		var incoming config.ProviderEntry
		if decodeTOML(other.Data, &incoming) != nil || endpoints(have) != endpoints(incoming) {
			return ConsentEndpoint
		}
	}
	return ""
}

func endpoints(p config.ProviderEntry) string {
	return strings.Join([]string{p.BaseURL, p.ChatURL, p.RequestURL, p.ModelsURL, p.BalanceURL, p.APIKeyEnv}, "\x00")
}

// Environment names a restored key may never take, and a restored provider
// may never read its key from. Setting one rewrites how this process reaches
// the network or loads code, and the account token is this machine's login.
var deniedKeyNames = []string{
	"PATH", "HOME", "USERPROFILE", "SHELL", "COMSPEC", "TMPDIR", "TEMP", "TMP",
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_OPTIONS", "NODE_EXTRA_CA_CERTS",
	"REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "PYTHONPATH", "PYTHONSTARTUP",
	"GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL", "BASH_ENV", "ENV",
}

var deniedKeyPrefixes = []string{"REASONIX_", "LD_", "DYLD_", "GIT_"}

func deniedKey(key string) bool {
	if config.IsPrivateCredentialSlot(strings.TrimSpace(key)) {
		return false
	}
	upper := strings.ToUpper(strings.TrimSpace(key))
	if slices.Contains(deniedKeyNames, upper) {
		return true
	}
	return slices.ContainsFunc(deniedKeyPrefixes, func(p string) bool { return strings.HasPrefix(upper, p) })
}

// validateRestorable rejects a snapshot this build would never have written:
// a unit whose payload names something other than its item, a key no carried
// provider reads, or a key name that steers the host rather than a model.
func (s *Snapshot) validateRestorable() error {
	readers := map[string]bool{}
	for _, it := range s.Items {
		switch it.Kind {
		case KindProvider:
			var p config.ProviderEntry
			if err := decodeTOML(it.Data, &p); err != nil || p.Name != it.Name {
				return fmt.Errorf("%w: provider %q", ErrMalformed, it.Name)
			}
			if key := strings.TrimSpace(p.APIKeyEnv); key != "" {
				if deniedKey(key) {
					return fmt.Errorf("%w: provider %q reads %s", ErrMalformed, it.Name, key)
				}
				readers[key] = true
			}
		case KindMCP:
			var p config.PluginEntry
			if err := decodeTOML(it.Data, &p); err != nil || p.Name != it.Name {
				return fmt.Errorf("%w: MCP server %q", ErrMalformed, it.Name)
			}
		}
	}
	for _, it := range s.Items {
		if it.Kind != KindSecret {
			continue
		}
		var d secretData
		if err := json.Unmarshal(it.Data, &d); err != nil || d.Key != it.Name || !readers[d.Key] || deniedKey(d.Key) ||
			!slices.Contains(s.Categories, CategorySecrets) {
			return fmt.Errorf("%w: key %q", ErrMalformed, it.Name)
		}
	}
	return nil
}
