package agentpreset

import (
	"maps"
	"sort"

	"reasonix/internal/base/secrets"
)

// CaptureEnvironment retains parameters and records credential keys for live lookup.
func CaptureEnvironment(values map[string]string) (map[string]string, []string) {
	frozen := maps.Clone(values)
	keys := []string{}
	for key, value := range values {
		if !reference(value) && (secrets.EnvKeySensitive(key) || secrets.Redact(value) != value) {
			delete(frozen, key)
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return frozen, keys
}

func ResolveEnvironment(frozen map[string]string, keys []string, live map[string]string) (map[string]string, error) {
	return resolveCredentialMap(frozen, keys, live)
}
