package provider

import "fmt"

// AuthError reports that a provider rejected the API key (HTTP 401/403).
// Itsmessageisalreadyuser-facingandactionable — it names the provider and,
// when known, the environment variable the key comes from — and it carries theserver's own reason as Body,
// because relay gateways explain *why* the key wasrejected ("token expired", key not entitled to the model)
// in the responsebody. Body is deliberately NOTpart of Error(): servers echo maskedkeyfragmentsinauthbodies,
// and the ambient error string flows intologs,
// status lines, and traces where key material must never propagate. Displaylayers that want the reason read
// Body and extract it themselves. Providersshould return this (rather than a generic status error)
// forauthfailures.
type AuthError struct {
	Provider            string // stable provider instance id, e.g. "deepseek"
	ProviderDisplayName string // user-editable display label
	Protocol            string // configured wire adapter id
	KeyEnv              string // the api_key_env the key is read from, when known
	KeySource           string // human-readable source of KeyEnv, when known
	Status              int    // the HTTP status (401 or 403)
	HasKey              bool   // a non-empty key was sent — the server rejected it, vs. no key configured at all
	Body                string // trimmed response-body snippet, the server's verbatim reason when it gave one
}

func (e *AuthError) Error() string {
	key := "the API key"
	if e.KeyEnv != "" {
		key = e.KeyEnv
	}
	if e.KeySource != "" {
		key += " from " + e.KeySource
	}
	return fmt.Sprintf("authentication failed for provider %q (HTTP %d): %s is invalid or expired — update it (in .env or your environment) and retry, or run `reasonix setup`",
		ProviderDisplayLabel(e.Provider, e.ProviderDisplayName, e.Protocol), e.Status, key)
}
