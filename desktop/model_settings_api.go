package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/secrets"
)

// ModelSettingsChange is a closed set of model operations, never a replacement
// Config. Pointer fields distinguish omission from an explicit clear/false.
type ModelSettingsChange struct {
	Kind                string                       `json:"kind"`
	RequestID           string                       `json:"requestId"`
	ExpectedFingerprint string                       `json:"expectedFingerprint"`
	Field               string                       `json:"field,omitempty"`
	Ref                 string                       `json:"ref,omitempty"`
	Name                string                       `json:"name,omitempty"`
	PresetID            string                       `json:"presetId,omitempty"`
	BaseURL             string                       `json:"baseURL,omitempty"`
	Protocol            string                       `json:"protocol,omitempty"`
	Names               []string                     `json:"names,omitempty"`
	Provider            *ProviderView                `json:"provider,omitempty"`
	Key                 *string                      `json:"key,omitempty"`
	Enabled             *bool                        `json:"enabled,omitempty"`
	Number              int                          `json:"number,omitempty"`
	Catalogs            []ProviderModelCatalogUpdate `json:"catalogs,omitempty"`
}

type ModelSettingsIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ModelSettingsTarget struct {
	TabID           string `json:"tabId"`
	Title           string `json:"title,omitempty"`
	Application     string `json:"application"`
	AppliedRevision string `json:"appliedRevision"`
	DesiredRevision string `json:"desiredRevision"`
}

type ModelSettingsResult struct {
	RequestID       string                `json:"requestId"`
	Persisted       bool                  `json:"persisted"`
	Revision        string                `json:"revision"`
	Application     string                `json:"application"`
	Targets         []ModelSettingsTarget `json:"targets"`
	Issues          []ModelSettingsIssue  `json:"issues"`
	AppliedCatalogs []string              `json:"appliedCatalogs"`
}

type modelSettingsReceipt struct {
	digest string
	result ModelSettingsResult
}

func emptyModelSettingsResult() ModelSettingsResult {
	return ModelSettingsResult{Application: "not_required", Targets: []ModelSettingsTarget{}, Issues: []ModelSettingsIssue{}, AppliedCatalogs: []string{}}
}

func modelSettingsEditFingerprint(c *config.Config) string {
	// Default participates in editing concurrency, but not runtime freshness.
	return hex.EncodeToString([]byte(providerRemovalStateFingerprint(c, c.ModelRuntimeFingerprint(c.DefaultModel)+providerCredentialsRevision())))
}

func modelSettingsIssue(code string, err error) ModelSettingsIssue {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return ModelSettingsIssue{Code: code, Message: "The configuration file could not be read or written. Check file access and available disk space."}
	}
	return ModelSettingsIssue{Code: code, Message: secrets.RedactCredentials(err.Error())}
}

// ApplyModelSettings checks the edit precondition under the SAME file lock as
// the mutation. Replaying an old request cannot silently overwrite newer state.
func (a *App) ApplyModelSettings(change ModelSettingsChange) (result ModelSettingsResult) {
	a.modelSettingsSubmitMu.Lock()
	defer a.modelSettingsSubmitMu.Unlock()
	result = emptyModelSettingsResult()
	result.RequestID = change.RequestID
	raw, marshalErr := json.Marshal(change)
	if marshalErr != nil {
		result.Issues = append(result.Issues, modelSettingsIssue("validation", marshalErr))
		return result
	}
	mac := hmac.New(sha256.New, providerStateFingerprintKey)
	_, _ = mac.Write(raw)
	digest := hex.EncodeToString(mac.Sum(nil))
	if receipt, ok := a.modelSettingsReceipts[change.RequestID]; ok {
		if receipt.digest != digest {
			result.Issues = append(result.Issues, ModelSettingsIssue{Code: "request_conflict", Message: "requestId was already used for a different edit"})
			return result
		}
		return receipt.result
	}
	defer func() {
		if change.RequestID == "" {
			return
		}
		if a.modelSettingsReceipts == nil {
			a.modelSettingsReceipts = map[string]modelSettingsReceipt{}
		}
		a.modelSettingsReceipts[change.RequestID] = modelSettingsReceipt{digest, result}
		a.modelSettingsReceiptOrder = append(a.modelSettingsReceiptOrder, change.RequestID)
		if len(a.modelSettingsReceiptOrder) > 128 {
			delete(a.modelSettingsReceipts, a.modelSettingsReceiptOrder[0])
			a.modelSettingsReceiptOrder = a.modelSettingsReceiptOrder[1:]
		}
	}()
	if err := validateModelSettingsFields(change); err != nil {
		result.Issues = append(result.Issues, modelSettingsIssue("validation", err))
		return result
	}
	if strings.TrimSpace(change.RequestID) == "" || strings.TrimSpace(change.ExpectedFingerprint) == "" {
		result.Issues = append(result.Issues, modelSettingsIssue("validation", fmt.Errorf("requestId and expectedFingerprint are required; reload settings before saving")))
		return result
	}
	err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		// Every edit precondition includes credential state. Keep external
		// credential-only writers outside compare, stage and config commit.
		unlockCredentials, err := config.LockUserCredentialEdits()
		if err != nil {
			return err
		}
		defer unlockCredentials()
		c, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		result.Revision = modelSettingsEditFingerprint(c)
		if result.Revision != change.ExpectedFingerprint {
			return fmt.Errorf("model settings changed; reload and review the current values before saving")
		}
		baseline := c.ModelSettingsBaseline()
		defer c.CleanupStagedModelCredentialsLocked(path)
		if err := applyModelSettingsChange(c, change, &result); err != nil {
			return err
		}
		if change.Kind == "protocol_upgrade" {
			var changed bool
			changed, err = config.UpgradeDeepSeekProviderProtocolLocked(path, change.Name)
			if err == nil && !changed {
				err = fmt.Errorf("provider is not eligible for protocol upgrade")
			}
		} else if change.Kind == "preference" && change.Field == "search" {
			err = c.SaveWebSearchModelTo(path)
		} else {
			err = c.SaveModelSettingsTo(path, baseline)
		}
		if err != nil {
			return err
		}
		result.Persisted = true
		if saved, _, readErr := a.loadDesktopUserConfigForView(); readErr == nil {
			result.Revision = modelSettingsEditFingerprint(saved)
		}
		return nil
	}()
	if err != nil {
		result.Issues = append(result.Issues, modelSettingsIssue("save_failed", err))
		return result
	}
	a.modelSettingsSaved(change.Kind)
	status := a.GetModelSettingsApplication()
	result.Application, result.Targets, result.Issues = status.Application, status.Targets, status.Issues
	return result
}

// GetModelSettingsRequest recovers a known result after a bridge interruption.
// No receipt after restart/eviction means unknown; callers must not infer that
// a write failed or automatically repeat it.
func (a *App) GetModelSettingsRequest(requestID string) ModelSettingsResult {
	a.modelSettingsSubmitMu.Lock()
	receipt, ok := a.modelSettingsReceipts[requestID]
	a.modelSettingsSubmitMu.Unlock()
	if !ok {
		result := emptyModelSettingsResult()
		result.RequestID = requestID
		result.Issues = append(result.Issues, ModelSettingsIssue{Code: "unknown_result", Message: "The save result could not be confirmed. Review the current settings before saving again."})
		return result
	}
	result := receipt.result
	if result.Persisted {
		status := a.GetModelSettingsApplication()
		result.Application, result.Targets, result.Issues = status.Application, status.Targets, status.Issues
	}
	return result
}

func applyModelSettingsChange(c *config.Config, change ModelSettingsChange, result *ModelSettingsResult) error {
	switch change.Kind {
	case "preference":
		return applyModelPreference(c, change)
	case "provider_save":
		if change.Provider == nil {
			return fmt.Errorf("provider is required")
		}
		if err := saveProviderConfig(c, *change.Provider); err != nil {
			return err
		}
		if change.Key != nil {
			return setConnectionCredentialConfig(c, change.Provider.Name, *change.Key)
		}
		return nil
	case "credential":
		if change.Key == nil {
			return fmt.Errorf("key is required")
		}
		if change.Name != "" && len(change.Names) != 0 {
			return fmt.Errorf("use either name or names for a credential edit")
		}
		names := change.Names
		if change.Name != "" {
			names = []string{change.Name}
		}
		return setConnectionsCredentialConfig(c, names, *change.Key)
	case "web_search_capability":
		if change.Enabled == nil {
			return fmt.Errorf("enabled is required")
		}
		return setProviderWebSearchConfig(c, change.Names, *change.Enabled)
	case "connection_add":
		return addProviderConnectionConfig(c, change.PresetID, change.Name, modelSettingsKey(change), change.BaseURL, change.Protocol)
	case "official_add":
		return addOfficialProviderAccessConfig(c, change.Name, modelSettingsKey(change))
	case "preset_add":
		return addProviderPresetConfig(c, change.PresetID, modelSettingsKey(change))
	case "preset_reset":
		return resetProviderPresetConfig(c, change.PresetID)
	case "protocol_upgrade":
		if change.Name == "" {
			return fmt.Errorf("provider name is required")
		}
		return nil
	case "catalogs":
		revision := providerCredentialsRevision()
		for _, update := range change.Catalogs {
			changed, err := applyProviderModelCatalogUpdate(c, update, revision)
			if err != nil {
				return err
			}
			if changed {
				result.AppliedCatalogs = append(result.AppliedCatalogs, update.Name)
			}
		}
		return nil
	case "provider_remove", "access_remove":
		return applyModelProviderRemoval(c, change)
	case "rename":
		if strings.TrimSpace(change.Ref) == "" {
			return fmt.Errorf("display name is required")
		}
		for _, name := range change.Names {
			p, ok := c.Provider(name)
			if !ok {
				return fmt.Errorf("unknown provider %q", name)
			}
			entry := *p
			entry.DisplayName = strings.TrimSpace(change.Ref)
			if err := c.UpsertProvider(entry); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown model setting operation %q", change.Kind)
	}
}

func applyModelPreference(c *config.Config, change ModelSettingsChange) error {
	if change.Provider != nil || change.Key != nil || len(change.Catalogs) > 0 || change.Enabled != nil {
		return fmt.Errorf("unexpected preference fields")
	}
	switch change.Field {
	case "default":
		return setDefaultModelConfig(c, change.Ref)
	case "planner":
		return setPlannerModelConfig(c, change.Ref)
	case "vision":
		return setVisionModelConfig(c, change.Ref)
	case "subagent":
		return setSubagentModelConfig(c, change.Ref)
	case "subagent_effort":
		return setSubagentEffortConfig(c, change.Ref)
	case "profile_model":
		return setSubagentProfileModelConfig(c, change.Name, change.Ref)
	case "profile_effort":
		return setSubagentProfileEffortConfig(c, change.Name, change.Ref)
	case "depth":
		return setMaxSubagentDepthConfig(c, change.Number)
	case "concurrency":
		return setMaxSubagentConcurrencyConfig(c, change.Number)
	case "writers":
		return setMaxParallelWritersConfig(c, change.Number)
	case "search":
		return setWebSearchModelConfig(c, change.Ref)
	default:
		return fmt.Errorf("unknown model preference %q", change.Field)
	}
}

func applyModelProviderRemoval(c *config.Config, change ModelSettingsChange) error {
	names := uniqueNonEmptyStrings(change.Names)
	if len(names) == 0 {
		return fmt.Errorf("provider names are required")
	}
	for _, name := range names {
		if _, ok := c.Provider(name); !ok {
			return fmt.Errorf("unknown provider %q", name)
		}
	}
	removeEntries := change.Kind == "provider_remove"
	if change.Kind == "access_remove" {
		p, _ := c.Provider(names[0])
		removeEntries = !isOfficialBuiltInProvider(*p)
		if removeEntries && len(names) > 1 && !isAtomicCustomProviderGroup(c, names) {
			return fmt.Errorf("custom providers do not belong to one removable group")
		}
		if !removeEntries {
			if err := validateOfficialProviderRemoval(c, names); err != nil {
				return err
			}
			names = officialProviderRemovalTargets(names)
		}
	}
	fallback := providerAccessFallbackRef(c, names)
	retargetProviderReferences(c, names, fallback)
	if removeEntries {
		for _, name := range names {
			p, _ := c.Provider(name)
			if isOfficialBuiltInProvider(*p) {
				return fmt.Errorf("remove access for an official provider instead")
			}
			if err := c.RemoveProvider(name); err != nil {
				return err
			}
		}
	}
	removeProviderAccess(c, names...)
	return nil
}

func validateModelSettingsFields(change ModelSettingsChange) error {
	allowed := ""
	switch change.Kind {
	case "preference":
		switch change.Field {
		case "depth", "concurrency", "writers":
			allowed = "Field Number"
		case "profile_model", "profile_effort":
			allowed = "Field Name Ref"
		default:
			allowed = "Field Ref"
		}
	case "provider_save":
		allowed = "Provider Key"
	case "credential":
		allowed = "Name Names Key"
	case "web_search_capability":
		allowed = "Names Enabled"
	case "connection_add":
		allowed = "PresetID Name Key BaseURL Protocol"
	case "official_add":
		allowed = "Name Key"
	case "preset_add":
		allowed = "PresetID Key"
	case "preset_reset":
		allowed = "PresetID"
	case "protocol_upgrade":
		allowed = "Name"
	case "catalogs":
		allowed = "Catalogs"
	case "provider_remove", "access_remove":
		allowed = "Names"
	case "rename":
		allowed = "Names Ref"
	default:
		return fmt.Errorf("unknown model setting operation %q", change.Kind)
	}
	allowed = " Kind RequestID ExpectedFingerprint " + allowed + " "
	value, typ := reflect.ValueOf(change), reflect.TypeOf(change)
	for i := range value.NumField() {
		if !value.Field(i).IsZero() && !strings.Contains(allowed, " "+typ.Field(i).Name+" ") {
			return fmt.Errorf("unexpected field %s for %s", typ.Field(i).Tag.Get("json"), change.Kind)
		}
	}
	return nil
}

func modelSettingsKey(change ModelSettingsChange) string {
	if change.Key == nil {
		return ""
	}
	return *change.Key
}

func setConnectionCredentialConfig(c *config.Config, name, key string) error {
	return setConnectionsCredentialConfig(c, []string{name}, key)
}

func setConnectionsCredentialConfig(c *config.Config, names []string, key string) error {
	if len(names) == 0 {
		return fmt.Errorf("at least one provider is required")
	}
	entries := make([]config.ProviderEntry, 0, len(names))
	for _, name := range names {
		p, ok := c.Provider(name)
		if !ok {
			return fmt.Errorf("unknown provider %q", name)
		}
		entries = append(entries, *p)
	}
	env, err := c.StageModelCredentialLocked(key)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		entry.APIKeyEnv = env
		if err := c.UpsertProvider(entry); err != nil {
			return err
		}
	}
	return nil
}

// Status is derived from the owning runtime and current disk config; it cannot
// be lost by an event race, app restart or changing the active tab.
func (a *App) GetModelSettingsApplication() ModelSettingsResult {
	result := emptyModelSettingsResult()
	result.Persisted = true
	if cfg, _, err := a.loadDesktopUserConfigForView(); err == nil {
		result.Revision = modelSettingsEditFingerprint(cfg)
	} else {
		result.Application = "failed"
		result.Issues = append(result.Issues, modelSettingsIssue("read_failed", err))
	}
	a.mu.RLock()
	tabs := append([]*WorkspaceTab(nil), a.runtimeTabsLocked()...)
	controllers := make([]modelSettingsSnapshot, len(tabs))
	failures := make([]*modelSettingsApplyFailure, len(tabs))
	titles := make([]string, len(tabs))
	for i, tab := range tabs {
		if tab != nil {
			controllers[i], _ = tab.Ctrl.(modelSettingsSnapshot)
			failures[i] = tab.modelApplication.failure
			titles[i] = tab.TopicTitle
			if titles[i] == "" {
				titles[i] = tab.Label
			}
		}
	}
	a.mu.RUnlock()
	for i, tab := range tabs {
		if tab == nil {
			continue
		}
		snapshot := controllers[i]
		if snapshot == nil {
			continue
		}
		applied, desired, err := snapshot.ModelSettingsState()
		state := "applied"
		if err != nil {
			state = "failed"
			result.Application = "failed"
			result.Issues = append(result.Issues, modelSettingsIssue("read_failed", err))
		} else if applied != desired {
			state = "pending"
			if failure := failures[i]; failure != nil && failure.revision == desired {
				state = "failed"
				result.Application = "failed"
				result.Issues = append(result.Issues, ModelSettingsIssue{Code: "apply_failed", Message: failure.message})
			}
			if result.Application != "failed" {
				result.Application = "pending"
			}
		}
		result.Targets = append(result.Targets, ModelSettingsTarget{TabID: tab.ID, Title: titles[i], Application: state, AppliedRevision: applied, DesiredRevision: desired})
	}
	a.appendRemoteModelSettingsStatus(&result)
	if result.Application == "not_required" && len(result.Targets) > 0 {
		result.Application = "applied"
	}
	return result
}

func (a *App) RetryModelSettingsApplication(tabID string) ModelSettingsResult {
	a.remoteTabMu.Lock()
	remote := a.remoteTabs[tabID] != nil
	a.remoteTabMu.Unlock()
	if remote {
		_, _, err := a.ensureRemoteModelSettings(tabID)
		result := a.GetModelSettingsApplication()
		if err != nil {
			result.Application = "failed"
			result.Issues = append(result.Issues, modelSettingsIssue("apply_failed", err))
		}
		return result
	}
	a.mu.RLock()
	tab := a.tabByEventSinkIDLocked(tabID)
	a.mu.RUnlock()
	var err error
	if tab == nil || tab.ID != tabID {
		err = fmt.Errorf("session is no longer available")
	} else {
		err = a.refreshTabModelSettings(tab)
	}
	result := a.GetModelSettingsApplication()
	if err != nil {
		result.Application = "failed"
		result.Issues = append(result.Issues, modelSettingsIssue("apply_failed", err))
	}
	return result
}
