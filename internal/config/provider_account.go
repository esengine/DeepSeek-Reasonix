package config

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"unicode"
)

const (
	MainProviderAccountID         = "main"
	maxProviderAccountIDLen       = 32
	legacyAccountIDPrefix         = "legacy-"
	providerAccountsConfigVersion = 8
)

var providerAccountIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ProviderAccount is one independently credentialed account for a curated
// provider family. Secrets stay in Reasonix home .env under APIKeyEnv.
type ProviderAccount struct {
	ProviderID     string   `toml:"provider_id"`
	PresetID       string   `toml:"preset_id,omitempty"`
	ID             string   `toml:"id"`
	Label          string   `toml:"label"`
	APIKeyEnv      string   `toml:"api_key_env"`
	Enabled        *bool    `toml:"enabled,omitempty"`
	Default        bool     `toml:"default,omitempty"`
	Retired        bool     `toml:"retired,omitempty"`
	DisabledRoutes []string `toml:"disabled_routes,omitempty"`
}

func (a ProviderAccount) IsEnabled() bool {
	return !a.Retired && (a.Enabled == nil || *a.Enabled)
}

func (a ProviderAccount) key() providerAccountKey {
	return providerAccountKey{ProviderID: a.ProviderID, ID: a.ID}
}

type providerAccountKey struct {
	ProviderID string
	ID         string
}

var providerAccountLabelAliases = map[string]string{
	"main":     MainProviderAccountID,
	"default":  MainProviderAccountID,
	"primary":  MainProviderAccountID,
	"主账号":      MainProviderAccountID,
	"主帐户":      MainProviderAccountID,
	"默认":       MainProviderAccountID,
	"默认账号":     MainProviderAccountID,
	"backup":   "backup",
	"spare":    "backup",
	"备用":       "backup",
	"备用账号":     "backup",
	"team":     "team",
	"团队":       "team",
	"团队账号":     "team",
	"personal": "personal",
	"个人":       "personal",
	"个人账号":     "personal",
}

func IsProviderAccountID(id string) bool {
	return providerAccountIDPattern.MatchString(strings.TrimSpace(id))
}

func SuggestProviderAccountID(providerID, label string) string {
	label = strings.TrimSpace(label)
	if alias, ok := providerAccountLabelAliases[label]; ok {
		return alias
	}
	if alias, ok := providerAccountLabelAliases[strings.ToLower(label)]; ok {
		return alias
	}
	if slug := slugifyProviderAccountID(label); slug != "" {
		return slug
	}
	return "a" + providerIdentityHash(strings.TrimSpace(providerID) + "\x00" + label)[:7]
}

func slugifyProviderAccountID(label string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(label)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '_' || r == '-':
			if b.Len() > 0 && !lastDash {
				b.WriteByte(byte(r))
				lastDash = true
			}
		case unicode.IsSpace(r):
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-_")
	if len(slug) > maxProviderAccountIDLen {
		slug = slug[:maxProviderAccountIDLen]
		slug = strings.Trim(slug, "-_")
	}
	if !IsProviderAccountID(slug) {
		return ""
	}
	return slug
}

func uniqueProviderAccountID(providerID, suggested string, used map[providerAccountKey]bool) string {
	suggested = strings.TrimSpace(suggested)
	if !IsProviderAccountID(suggested) {
		suggested = SuggestProviderAccountID(providerID, suggested)
	}
	if !used[providerAccountKey{ProviderID: providerID, ID: suggested}] {
		return suggested
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", suggested, i)
		if len(candidate) > maxProviderAccountIDLen {
			candidate = suggested
			if len(candidate) > maxProviderAccountIDLen-3 {
				candidate = candidate[:maxProviderAccountIDLen-3]
			}
			candidate = fmt.Sprintf("%s-%d", strings.Trim(candidate, "-_"), i)
		}
		if IsProviderAccountID(candidate) && !used[providerAccountKey{ProviderID: providerID, ID: candidate}] {
			return candidate
		}
	}
	base := "a" + providerIdentityHash(providerID + "\x00" + suggested)[:7]
	if !used[providerAccountKey{ProviderID: providerID, ID: base}] {
		return base
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", strings.TrimSuffix(base, "-"), i)
		if len(candidate) > maxProviderAccountIDLen {
			candidate = candidate[:maxProviderAccountIDLen]
		}
		if IsProviderAccountID(candidate) && !used[providerAccountKey{ProviderID: providerID, ID: candidate}] {
			return candidate
		}
	}
	return base
}

func SuggestAccountAPIKeyEnv(baseEnv, accountID string, used map[string]bool) string {
	baseEnv = strings.TrimSpace(baseEnv)
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || accountID == MainProviderAccountID {
		if baseEnv != "" && IsValidCredentialKey(baseEnv) && !used[baseEnv] {
			return baseEnv
		}
	}
	suffix := strings.ToUpper(strings.ReplaceAll(accountID, "-", "_"))
	if suffix == "" {
		suffix = "ACCOUNT"
	}
	candidate := baseEnv
	if candidate == "" {
		candidate = "PROVIDER_API_KEY"
	}
	if !strings.HasSuffix(candidate, "_"+suffix) {
		candidate += "_" + suffix
	}
	if IsValidCredentialKey(candidate) && !used[candidate] {
		return candidate
	}
	hashed := candidate + "_" + strings.ToUpper(providerIdentityHash(candidate)[:6])
	if IsValidCredentialKey(hashed) && !used[hashed] {
		return hashed
	}
	return candidate
}

func uniqueProviderName(base string, taken map[string]bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "provider"
	}
	if !taken[base] {
		return base
	}
	short := providerIdentityHash(base)[:6]
	candidate := base + "-" + short
	if !taken[candidate] {
		return candidate
	}
	for i := 2; i < 100; i++ {
		candidate = fmt.Sprintf("%s-%s-%d", base, short, i)
		if !taken[candidate] {
			return candidate
		}
	}
	return base + "-" + providerIdentityHash(base)
}

// providerIdentityHash creates a deterministic, non-cryptographic identity
// suffix for generated account/provider names. It must never be used for
// credential hashing or security decisions.
func providerIdentityHash(value string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%08x", h.Sum32())
}

func validateProviderAccount(a ProviderAccount) error {
	if strings.TrimSpace(a.ProviderID) == "" {
		return fmt.Errorf("provider account: provider_id is required")
	}
	if !IsProviderAccountID(a.ID) {
		return fmt.Errorf("provider account %s/%s: id must match %s", a.ProviderID, a.ID, providerAccountIDPattern.String())
	}
	if strings.TrimSpace(a.Label) == "" {
		return fmt.Errorf("provider account %s/%s: label is required", a.ProviderID, a.ID)
	}
	if env := strings.TrimSpace(a.APIKeyEnv); env != "" && !IsValidCredentialKey(env) {
		return fmt.Errorf("provider account %s/%s: api_key_env %q is not a valid environment variable name", a.ProviderID, a.ID, env)
	}
	return nil
}

func cloneProviderAccount(a ProviderAccount) ProviderAccount {
	if a.Enabled != nil {
		value := *a.Enabled
		a.Enabled = &value
	}
	if a.DisabledRoutes != nil {
		a.DisabledRoutes = append([]string(nil), a.DisabledRoutes...)
	}
	return a
}

func cloneProviderAccounts(in []ProviderAccount) []ProviderAccount {
	if in == nil {
		return nil
	}
	out := make([]ProviderAccount, 0, len(in))
	for _, a := range in {
		out = append(out, cloneProviderAccount(a))
	}
	return out
}

func stampAccountMetadata(e *ProviderEntry, account ProviderAccount, routeID string) {
	if e == nil {
		return
	}
	e.AccountProviderID = account.ProviderID
	e.AccountID = account.ID
	e.AccountRouteID = strings.TrimSpace(routeID)
	e.AccountLabel = account.Label
}

func (c *Config) lookupProviderAccount(providerID, accountID string) (int, ProviderAccount, bool) {
	if c == nil {
		return -1, ProviderAccount{}, false
	}
	providerID = strings.TrimSpace(providerID)
	accountID = strings.TrimSpace(accountID)
	for i, a := range c.ProviderAccounts {
		if a.ProviderID == providerID && a.ID == accountID {
			return i, a, true
		}
	}
	return -1, ProviderAccount{}, false
}

func (c *Config) providerAccountUsedIDs() map[providerAccountKey]bool {
	used := map[providerAccountKey]bool{}
	if c == nil {
		return used
	}
	for _, a := range c.ProviderAccounts {
		used[a.key()] = true
	}
	return used
}

func (c *Config) usedAPIKeyEnvs() map[string]bool {
	used := map[string]bool{}
	if c == nil {
		return used
	}
	for _, a := range c.ProviderAccounts {
		if env := strings.TrimSpace(a.APIKeyEnv); env != "" {
			used[env] = true
		}
	}
	return used
}

func (c *Config) userOwnedProvider(name string) bool {
	if c == nil {
		return false
	}
	if len(c.providerSources) == 0 {
		return true
	}
	return c.providerSources[providerMergeKey(ProviderEntry{Name: name})] != providerSourceProject
}

func (c *Config) markUserProvider(name string) {
	if c == nil || strings.TrimSpace(name) == "" {
		return
	}
	if c.providerSources == nil {
		return
	}
	c.providerSources[strings.TrimSpace(name)] = providerSourceUser
}
