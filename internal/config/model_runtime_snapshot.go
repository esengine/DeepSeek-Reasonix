package config

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Model fingerprints are process-local opaque values. Including credentials in
// a keyed digest detects rotations without publishing a password oracle.
var modelSnapshotKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}()

// LoadModelRuntimeSnapshot reads the config and credentials under their writer
// locks. In particular, boot cannot mix half of two connection transactions.
func LoadModelRuntimeSnapshot(root string, sessionRefs ...string) (*Config, error) {
	unlock := LockUserConfigEdits()
	defer unlock()
	unlockCredentials, err := LockUserCredentialEdits()
	if err != nil {
		return nil, err
	}
	defer unlockCredentials()
	if err := currentUserConfigEditLockError(); err != nil {
		return nil, err
	}
	c, err := LoadForRootReadOnly(root)
	if err != nil {
		return nil, err
	}
	// Restored session aliases may introduce providers absent from the file.
	// Expand them while both locks are held, before freezing their credentials.
	NormalizeLegacyMimoCustomProvidersForRefs(c, sessionRefs...)
	c.FreezeProviderCredentials()
	return c, nil
}

// FreezeProviderCredentials fixes both present and absent credentials for the
// lifetime of a runtime. Lazy child/model resolvers must never reread .env.
func (c *Config) FreezeProviderCredentials() {
	for i := range c.Providers {
		p := &c.Providers[i]
		p.resolvedAPIKey = p.APIKey()
		p.credentialsFrozen = true
	}
}

// ModelRuntimeFingerprint describes the model resolver built for a session.
// DefaultModel is deliberately excluded: the session owns its selected model.
// All available providers participate because child/vision/search selection may
// choose one later in the same run. UI-only provider labels do not participate.
func (c *Config) ModelRuntimeFingerprint(model string) string {
	type providerSnapshot struct {
		Entry ProviderEntry
		Key   string
	}
	providers := make([]providerSnapshot, 0, len(c.Providers))
	for _, entry := range c.Providers {
		key := entry.APIKey()
		entry.DisplayName, entry.PresetID, entry.BalanceURL, entry.ModelsURL = "", "", "", ""
		entry.APIKeyEnv = "" // a changed reference with the same resolved key is not a runtime change
		entry.PresetVersion = 0
		providers = append(providers, providerSnapshot{entry, key})
	}
	data, err := json.Marshal(struct {
		Model                                                         string
		Providers                                                     []providerSnapshot
		Access                                                        []string
		Planner, Vision, Search, Guardian, Recovery, Subagent, Effort string
		SubagentModels, SubagentEfforts                               map[string]string
		Depth, Concurrency, Writers                                   int
	}{model, providers, c.Desktop.ProviderAccess,
		c.Agent.PlannerModel, c.Agent.VisionModel, c.Agent.WebSearchModel,
		c.Agent.GuardianModel, c.Agent.RecoveryModel, c.Agent.SubagentModel,
		c.Agent.SubagentEffort, c.Agent.SubagentModels, c.Agent.SubagentEfforts,
		c.Agent.MaxSubagentDepth, c.Agent.MaxSubagentConcurrency, c.Agent.MaxParallelWriters})
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, modelSnapshotKey)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
