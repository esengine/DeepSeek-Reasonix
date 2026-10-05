package mcpsetup

import (
	"maps"
	"slices"

	"reasonix/internal/base/secrets"
)

// Redact replaces a value whose key or content looks like a credential. It is
// what any surface printing an MCP config has to run first — a server block goes
// into bug reports and screenshots far more often than it gets read once.
func Redact(key, value string) string {
	return secrets.RedactConfigValue(key, value)
}

// SensitiveKey reports whether a config key names a credential.
func SensitiveKey(key string) bool {
	return secrets.CredentialKey(key)
}

// SensitiveValue reports whether a value carries a credential regardless of the
// key it is filed under.
func SensitiveValue(value string) bool {
	return secrets.CredentialValue(value)
}

// RedactURL masks credential-bearing query parameters while keeping the endpoint
// readable — the host is the part the user needs to recognise.
func RedactURL(raw string) string {
	return secrets.RedactEndpoint(raw)
}

func sortedKeys(m map[string]string) []string {
	keys := slices.Sorted(maps.Keys(m))
	return keys
}
