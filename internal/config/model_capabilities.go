package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

type CapabilityState string

const (
	CapabilitySupported   CapabilityState = "supported"
	CapabilityUnsupported CapabilityState = "unsupported"
	CapabilityUnknown     CapabilityState = "unknown"
)

var modelCapabilityCacheWriteMu sync.Mutex

type CapabilitySource string

const (
	CapabilitySourceOverride CapabilitySource = "override"
	CapabilitySourcePreset   CapabilitySource = "preset"
	CapabilitySourceLegacy   CapabilitySource = "legacy"
	CapabilitySourceAdapter  CapabilitySource = "adapter"
	CapabilitySourceCache    CapabilitySource = "cache"
	CapabilitySourceDefault  CapabilitySource = "adapter_default"
	CapabilitySourceUnknown  CapabilitySource = "unknown"
	CapabilitySourceProtocol CapabilitySource = "protocol"
)

type ResolvedModelCapability struct {
	Model                   string
	InputModalities         []provider.ModelModality
	State                   CapabilityState
	Source                  CapabilitySource
	ModelInfo               provider.ModelInfo
	AutomaticState          CapabilityState
	AutomaticSource         CapabilitySource
	ImageInputEnableAllowed bool
	ImageInputBlockReason   string
}

type ModelCapabilityCacheFile struct {
	Version int                         `json:"version"`
	Entries []ModelCapabilityCacheEntry `json:"entries"`
}

type ModelCapabilityCacheEntry struct {
	ProviderFingerprint string                   `json:"providerFingerprint"`
	ModelID             string                   `json:"modelID"`
	InputModalities     []provider.ModelModality `json:"inputModalities"`
	Source              CapabilitySource         `json:"source"`
	FetchedAt           time.Time                `json:"fetchedAt"`
	ExpiresAt           time.Time                `json:"expiresAt"`
}

const (
	modelCapabilityCacheVersion  = 2
	modelCapabilityCacheTTL      = 24 * time.Hour
	modelCapabilityCacheMaxSize  = 2 << 20
	modelCapabilityCacheMaxItems = 4096
)

// ModelCapabilityResolver owns the single capability decision used by boot,
// controller, and settings. Dynamic entries are process-local until explicitly
// hydrated from the sidecar cache; user config remains the higher-priority
// source and is never rewritten by discovery.
type ModelCapabilityResolver struct {
	mu                  sync.RWMutex
	entries             map[string]ModelCapabilityCacheEntry
	path                string
	credentialsRevision string
}

func NewModelCapabilityResolver() *ModelCapabilityResolver {
	r := &ModelCapabilityResolver{
		entries:             map[string]ModelCapabilityCacheEntry{},
		credentialsRevision: CredentialStoreRevision(),
	}
	if dir := CacheDir(); dir != "" {
		r.path = filepath.Join(dir, "model-capabilities-v2.json")
		r.load()
	}
	return r
}

func (r *ModelCapabilityResolver) Resolve(entry *ProviderEntry) ResolvedModelCapability {
	return r.resolveWithCredentialRevision(entry, r.credentialRevision())
}

func (r *ModelCapabilityResolver) credentialRevision() string {
	if r != nil && r.credentialsRevision != "" {
		return r.credentialsRevision
	}
	return CredentialStoreRevision()
}

func (r *ModelCapabilityResolver) resolveWithCredentialRevision(entry *ProviderEntry, credentialsRevision string) ResolvedModelCapability {
	resolved := r.resolveAutomatic(entry, credentialsRevision)
	resolved.AutomaticState, resolved.AutomaticSource = resolved.State, resolved.Source
	resolved.ImageInputEnableAllowed = entry != nil
	if entry == nil {
		return resolved
	}
	// Read the exact model's override even when a catalog caller has not gone
	// through Config.ResolveModel. Never reuse another selected model's value.
	override := entry.visionOverride
	if len(entry.ModelOverrides) > 0 {
		override = nil
		if ov, ok := entry.modelOverrideForModel(entry.Model); ok {
			override = ov.Vision
		}
	}
	if override != nil {
		value := capabilityFromBool(resolved.Model, *override, CapabilitySourceOverride)
		resolved.State, resolved.Source, resolved.InputModalities = value.State, value.Source, value.InputModalities
	}
	requestURL := entry.RequestURL
	if requestURL == "" && entry.Kind == "openai" {
		requestURL = entry.ChatURL
	}
	if (openai.IsDeepSeek(entry.BaseURL) || openai.IsDeepSeek(requestURL)) && !openai.IsOfficialDeepSeekVisionModel(entry.Model) {
		resolved.State, resolved.Source = CapabilityUnsupported, CapabilitySourceProtocol
		resolved.InputModalities = []provider.ModelModality{provider.ModalityText}
		resolved.AutomaticState, resolved.AutomaticSource = CapabilityUnsupported, CapabilitySourceProtocol
		resolved.ImageInputEnableAllowed = false
		resolved.ImageInputBlockReason = "official_deepseek_text_model"
	}
	resolved.ModelInfo.ID = resolved.Model
	resolved.ModelInfo.InputModalities = append([]provider.ModelModality(nil), resolved.InputModalities...)
	return resolved
}

func (r *ModelCapabilityResolver) resolveAutomatic(entry *ProviderEntry, credentialsRevision string) ResolvedModelCapability {
	if entry == nil {
		return ResolvedModelCapability{State: CapabilityUnknown, Source: CapabilitySourceUnknown}
	}
	model := strings.TrimSpace(entry.Model)
	if model == "" {
		return ResolvedModelCapability{State: CapabilityUnknown, Source: CapabilitySourceUnknown}
	}
	// Resolve catalog facts separately so a vision override or legacy declaration
	// cannot erase context/output/protocol metadata.
	facts, hasFacts := provider.PiCatalogModelInfoForProvider(entry.Name, entry.Kind, entry.BaseURL, model)
	if !hasFacts {
		facts, hasFacts = provider.BuiltinModelInfo(entry.Kind, entry.BaseURL, model)
	}
	if info, ok := presetModelInfo(entry, model); ok {
		resolved := capabilityFromModalities(model, info.InputModalities, CapabilitySourcePreset)
		resolved.ModelInfo = info
		if hasFacts {
			resolved.ModelInfo = facts
		}
		return resolved
	}
	if entry.Vision {
		resolved := capabilityFromBool(model, true, CapabilitySourceLegacy)
		resolved.ModelInfo = facts
		return resolved
	}
	if entry.HasVisionModel(model) {
		resolved := capabilityFromBool(model, true, CapabilitySourceLegacy)
		resolved.ModelInfo = facts
		return resolved
	}
	if hasFacts {
		resolved := capabilityFromModalities(model, facts.InputModalities, CapabilitySourceAdapter)
		resolved.ModelInfo = facts
		return resolved
	}
	if r != nil {
		key := r.entryKeyWithCredentialRevision(entry, model, credentialsRevision)
		r.mu.RLock()
		cached, ok := r.entries[key]
		r.mu.RUnlock()
		if ok && time.Now().Before(cached.ExpiresAt) {
			return capabilityFromModalities(model, cached.InputModalities, cached.Source)
		}
	}
	return capabilityFromModalities(model, nil, CapabilitySourceUnknown)
}

// presetModelInfo turns the repository's curated provider templates into a
// local model catalog. It only applies to an untouched preset identity; an
// explicitly edited vision list remains a user-owned legacy override.
func presetModelInfo(entry *ProviderEntry, model string) (provider.ModelInfo, bool) {
	if entry == nil || strings.TrimSpace(entry.PresetID) == "" {
		return provider.ModelInfo{}, false
	}
	preset, ok := CuratedProviderPreset(entry.PresetID)
	if !ok {
		return provider.ModelInfo{}, false
	}
	for _, candidate := range preset.Entries {
		if candidate.Name != entry.Name || candidate.Kind != entry.Kind || candidate.BaseURL != entry.BaseURL || !candidate.HasModel(model) {
			continue
		}
		if !stringSlicesEqual(candidate.VisionModels, entry.VisionModels) {
			return provider.ModelInfo{}, false
		}
		modalities := []provider.ModelModality{provider.ModalityText}
		if candidate.HasVisionModel(model) {
			modalities = append(modalities, provider.ModalityImage)
		}
		return provider.ModelInfo{ID: model, Name: model, InputModalities: modalities}, true
	}
	return provider.ModelInfo{}, false
}

func capabilityFromBool(model string, vision bool, source CapabilitySource) ResolvedModelCapability {
	if vision {
		return capabilityFromModalities(model, []provider.ModelModality{provider.ModalityText, provider.ModalityImage}, source)
	}
	return capabilityFromModalities(model, []provider.ModelModality{provider.ModalityText}, source)
}

func capabilityFromModalities(model string, modalities []provider.ModelModality, source CapabilitySource) ResolvedModelCapability {
	copyModalities := append([]provider.ModelModality(nil), modalities...)
	state := CapabilityUnsupported
	if slices.Contains(copyModalities, provider.ModalityImage) {
		state = CapabilitySupported
	}
	if modalities == nil {
		state = CapabilityUnknown
	}
	return ResolvedModelCapability{Model: model, InputModalities: copyModalities, State: state, Source: source}
}

// PutCatalog stores adapter results for one provider identity and persists a
// disposable cache. Invalid entries are ignored rather than enabling images.
func (r *ModelCapabilityResolver) PutCatalog(entry ProviderEntry, catalog []provider.ModelInfo) {
	r.PutCatalogAt(entry, catalog, time.Now())
}

// PutCatalogAt orders successful discoveries by request start, not completion.
// Callers must validate their frozen provider/credential identity before commit.
func (r *ModelCapabilityResolver) PutCatalogAt(entry ProviderEntry, catalog []provider.ModelInfo, started time.Time) {
	if r == nil {
		return
	}
	now := time.Now()
	credentialsRevision := r.credentialRevision()
	providerFingerprint := r.providerFingerprintForCredentialRevision(entry, credentialsRevision)
	r.mu.Lock()
	for _, model := range catalog {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		modalities := normalizeInputModalities(model.InputModalities)
		key := providerFingerprint + "\x00" + id
		if previous, ok := r.entries[key]; ok && !started.After(previous.FetchedAt) {
			continue
		}
		r.entries[key] = ModelCapabilityCacheEntry{
			ProviderFingerprint: providerFingerprint,
			ModelID:             id,
			InputModalities:     modalities,
			Source:              CapabilitySourceAdapter,
			FetchedAt:           started,
			ExpiresAt:           now.Add(modelCapabilityCacheTTL),
		}
	}
	r.mu.Unlock()
	r.persist()
}

func normalizeInputModalities(values []provider.ModelModality) []provider.ModelModality {
	if values == nil {
		return nil
	}
	seen := map[provider.ModelModality]bool{}
	out := make([]provider.ModelModality, 0, len(values))
	for _, value := range values {
		value = provider.ModelModality(strings.ToLower(strings.TrimSpace(string(value))))
		if value != provider.ModalityText && value != provider.ModalityImage {
			return nil
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *ModelCapabilityResolver) entryKey(entry *ProviderEntry, model string) string {
	return r.entryKeyWithCredentialRevision(entry, model, r.credentialRevision())
}

func (r *ModelCapabilityResolver) entryKeyWithCredentialRevision(entry *ProviderEntry, model, credentialsRevision string) string {
	return r.providerFingerprintForCredentialRevision(*entry, credentialsRevision) + "\x00" + strings.TrimSpace(model)
}

func (r *ModelCapabilityResolver) providerFingerprint(entry ProviderEntry) string {
	return r.providerFingerprintForCredentialRevision(entry, r.credentialRevision())
}

func (r *ModelCapabilityResolver) providerFingerprintForCredentialRevision(entry ProviderEntry, credentialsRevision string) string {
	h := hmac.New(sha256.New, []byte("reasonix-model-capabilities-cache-v2"))
	for _, value := range []string{
		"reasonix-model-capabilities-v2", entry.Name, entry.Kind, entry.BaseURL, entry.ChatURL, entry.RequestURL,
		entry.ModelsURL, entry.APIKeyEnv, fmt.Sprintf("%t", entry.AuthHeader), fmt.Sprintf("%t", entry.NoProxy),
		credentialsRevision,
	} {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = h.Write([]byte(value))
	}
	type headerPair struct{ key, value string }
	headers := make([]headerPair, 0, len(entry.Headers))
	for key, value := range entry.Headers {
		headers = append(headers, headerPair{key: strings.ToLower(strings.TrimSpace(key)), value: value})
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].key < headers[j].key })
	for _, header := range headers {
		key, value := header.key, header.value
		_, _ = fmt.Fprintf(h, "%d:", len(key))
		_, _ = h.Write([]byte(key))
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r *ModelCapabilityResolver) load() {
	if r.path == "" {
		return
	}
	file, ok := readModelCapabilityCacheFile(r.path)
	if !ok {
		return
	}
	now := time.Now()
	for _, entry := range file.Entries {
		if entry.ProviderFingerprint == "" || entry.ModelID == "" || !now.Before(entry.ExpiresAt) {
			continue
		}
		entry.InputModalities = normalizeInputModalities(entry.InputModalities)
		entry.Source = CapabilitySourceCache
		r.entries[entry.ProviderFingerprint+"\x00"+entry.ModelID] = entry
	}
}

func (r *ModelCapabilityResolver) persist() {
	if r == nil || r.path == "" {
		return
	}
	modelCapabilityCacheWriteMu.Lock()
	defer modelCapabilityCacheWriteMu.Unlock()
	r.mu.RLock()
	entries := make([]ModelCapabilityCacheEntry, 0, len(r.entries))
	now := time.Now()
	for _, entry := range r.entries {
		if now.Before(entry.ExpiresAt) {
			entry.InputModalities = append([]provider.ModelModality(nil), entry.InputModalities...)
			entries = append(entries, entry)
		}
	}
	r.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].FetchedAt.After(entries[j].FetchedAt) })
	if len(entries) > modelCapabilityCacheMaxItems {
		entries = entries[:modelCapabilityCacheMaxItems]
	}
	file := ModelCapabilityCacheFile{Version: modelCapabilityCacheVersion, Entries: entries}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(r.path)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	release, err := acquireCapabilityFileLock(r.path+".lock", 2*time.Second)
	if err != nil {
		return
	}
	defer release()
	// Merge with the latest on-disk snapshot after taking the cross-process
	// lock. Separate settings refreshes must not erase one another's entries.
	if existing, ok := readModelCapabilityCacheFile(r.path); ok {
		seen := make(map[string]int, len(entries))
		for i, entry := range entries {
			seen[entry.ProviderFingerprint+"\x00"+entry.ModelID] = i
		}
		for _, entry := range existing.Entries {
			key := entry.ProviderFingerprint + "\x00" + entry.ModelID
			if time.Now().Before(entry.ExpiresAt) {
				if i, ok := seen[key]; ok {
					if entry.FetchedAt.After(entries[i].FetchedAt) {
						entries[i] = entry
					}
				} else {
					seen[key] = len(entries)
					entries = append(entries, entry)
				}
			}
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].FetchedAt.After(entries[j].FetchedAt) })
		if len(entries) > modelCapabilityCacheMaxItems {
			entries = entries[:modelCapabilityCacheMaxItems]
		}
		file = ModelCapabilityCacheFile{Version: modelCapabilityCacheVersion, Entries: entries}
		data, err = json.MarshalIndent(file, "", "  ")
		if err != nil {
			return
		}
	}
	// Keep this resolver consistent with the winning on-disk observations.
	r.mu.Lock()
	for _, entry := range entries {
		key := entry.ProviderFingerprint + "\x00" + entry.ModelID
		if current, ok := r.entries[key]; !ok || entry.FetchedAt.After(current.FetchedAt) {
			r.entries[key] = entry
		}
	}
	r.mu.Unlock()
	if len(data) <= modelCapabilityCacheMaxSize {
		// Use the repository's strict cross-platform replacement helper so an
		// existing cache is replaced atomically on Windows as well as Unix.
		_ = fileutil.AtomicWriteFileStrict(r.path, data, 0o600)
	}
}

func readModelCapabilityCacheFile(path string) (ModelCapabilityCacheFile, bool) {
	fileHandle, err := os.Open(path)
	if err != nil {
		return ModelCapabilityCacheFile{}, false
	}
	defer fileHandle.Close()
	data, err := io.ReadAll(io.LimitReader(fileHandle, modelCapabilityCacheMaxSize+1))
	if err != nil || len(data) > modelCapabilityCacheMaxSize {
		return ModelCapabilityCacheFile{}, false
	}
	var file ModelCapabilityCacheFile
	if json.Unmarshal(data, &file) != nil || file.Version != modelCapabilityCacheVersion {
		return ModelCapabilityCacheFile{}, false
	}
	if len(file.Entries) > modelCapabilityCacheMaxItems {
		return ModelCapabilityCacheFile{}, false
	}
	// All read paths, including cross-process persistence merges, must apply
	// the same validation before an entry becomes visible to a resolver.
	for i := range file.Entries {
		entry := &file.Entries[i]
		entry.ModelID = strings.TrimSpace(entry.ModelID)
		if entry.ProviderFingerprint == "" || entry.ModelID == "" {
			return ModelCapabilityCacheFile{}, false
		}
		entry.InputModalities = normalizeInputModalities(entry.InputModalities)
	}
	return file, true
}

func acquireCapabilityFileLock(path string, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
