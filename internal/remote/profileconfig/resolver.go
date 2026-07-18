// Package profileconfig resolves Remote Session profiles from the authoritative
// Host configuration. It deliberately depends on config/boot semantics rather
// than Desktop state: no Windows model choice, credential, path, or tab identity
// crosses the Remote protocol boundary.
package profileconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/protocol"
)

// Resolver is stateless. Every resolution reloads the user and workspace
// configuration so an explicit Host-side config edit affects new Sessions but
// never mutates a profile already persisted in Session metadata.
type Resolver struct{}

func New() *Resolver { return &Resolver{} }

var _ catalog.ProfileResolver = (*Resolver)(nil)
var _ catalog.WorkspaceCatalogProvider = (*Resolver)(nil)

// WorkspaceCatalog returns only credential-free, currently selectable Host
// models. Model refs are the same canonical provider/model values accepted by
// ResolveProfile, so Desktop never has to interpret provider configuration.
func (r *Resolver) WorkspaceCatalog(ctx context.Context, workspaceRoot string) (protocol.WorkspaceCatalogResult, error) {
	if r == nil {
		return protocol.WorkspaceCatalogResult{}, profileError(protocol.ErrQueryFailed, errors.New("Remote profile resolver is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return protocol.WorkspaceCatalogResult{}, err
	}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" || !filepath.IsAbs(workspaceRoot) || filepath.Clean(workspaceRoot) != workspaceRoot {
		return protocol.WorkspaceCatalogResult{}, profileError(protocol.ErrQueryFailed, errors.New("workspace root must be a canonical absolute Host path"))
	}
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return protocol.WorkspaceCatalogResult{}, profileError(protocol.ErrQueryFailed, fmt.Errorf("load Host configuration: %w", err))
	}
	models := make([]protocol.ModelCatalogItem, 0)
	for index := range cfg.Providers {
		provider := &cfg.Providers[index]
		if !providerAllowed(cfg.Desktop.ProviderAccess, provider.Name) || !provider.Configured() {
			continue
		}
		for _, model := range provider.ChatModelList() {
			ref := provider.Name + "/" + model
			entry, ok := cfg.ResolveModel(ref)
			if !ok || entry == nil {
				continue
			}
			capability := config.EffortCapabilityForEntry(entry)
			levels := make([]string, 0, len(capability.Levels))
			levels = append(levels, capability.Levels...)
			models = append(models, protocol.ModelCatalogItem{
				Ref: protocol.ModelRef(ref), Provider: provider.Name, Model: model,
				Effort: protocol.EffortCatalog{
					Supported: capability.Supported,
					Default:   capability.Default,
					Levels:    levels,
				},
			})
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Ref < models[j].Ref })
	defaults, err := r.ResolveProfile(ctx, workspaceRoot, protocol.ProfileSelection{})
	if err != nil {
		return protocol.WorkspaceCatalogResult{}, err
	}
	result := protocol.WorkspaceCatalogResult{
		Models: models,
		CollaborationModes: []protocol.CollaborationMode{
			protocol.CollaborationNormal, protocol.CollaborationPlan, protocol.CollaborationGoal,
		},
		TokenModes:        []protocol.TokenMode{protocol.TokenFull, protocol.TokenEconomy, protocol.TokenDelivery},
		ToolApprovalModes: []protocol.ToolApprovalMode{protocol.ToolApprovalAsk, protocol.ToolApprovalAuto, protocol.ToolApprovalYOLO},
		DefaultProfile:    defaults,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return protocol.WorkspaceCatalogResult{}, profileError(protocol.ErrQueryFailed, err)
	}
	digest := sha256.Sum256(encoded)
	result.Revision = protocol.CatalogRevision("workspace_config_" + hex.EncodeToString(digest[:]))
	return result, nil
}

// ResolveProfile fills every frozen profile axis. workspaceRoot must already be
// a canonical Host path (Catalog owns path canonicalization); requiring an
// absolute clean path here prevents config lookup from depending on process cwd.
func (r *Resolver) ResolveProfile(ctx context.Context, workspaceRoot string, selection protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
	if r == nil {
		return protocol.ResolvedProfile{}, profileError(protocol.ErrInvalidProfile, errors.New("Remote profile resolver is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return protocol.ResolvedProfile{}, err
	}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" || !filepath.IsAbs(workspaceRoot) || filepath.Clean(workspaceRoot) != workspaceRoot {
		return protocol.ResolvedProfile{}, profileError(protocol.ErrInvalidProfile, errors.New("workspace root must be a canonical absolute Host path"))
	}

	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return protocol.ResolvedProfile{}, profileError(protocol.ErrInvalidProfile, fmt.Errorf("load Host configuration: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return protocol.ResolvedProfile{}, err
	}

	entry, model, err := resolveModel(cfg, selection.Model)
	if err != nil {
		return protocol.ResolvedProfile{}, err
	}
	effort, err := resolveEffort(entry, selection.Effort)
	if err != nil {
		return protocol.ResolvedProfile{}, err
	}
	collaboration, legacyYOLO, err := resolveCollaboration(selection.CollaborationMode)
	if err != nil {
		return protocol.ResolvedProfile{}, err
	}
	tokenMode, err := resolveTokenMode(selection.TokenMode)
	if err != nil {
		return protocol.ResolvedProfile{}, err
	}
	approvalMode, err := resolveToolApproval(selection.ToolApprovalMode, legacyYOLO)
	if err != nil {
		return protocol.ResolvedProfile{}, err
	}
	if selection.ToolApprovalMode == nil && !legacyYOLO {
		// The Desktop default is intentionally user-global. Loading it from the
		// merged project config would let a cloned reasonix.toml silently select
		// YOLO for a new Remote Session, unlike the existing Desktop path.
		userConfig := config.LoadForEdit(config.UserConfigPath())
		approvalMode = protocol.ToolApprovalMode(userConfig.DesktopDefaultToolApprovalMode())
	}

	return protocol.ResolvedProfile{
		Model:             model,
		Effort:            effort,
		CollaborationMode: collaboration,
		TokenMode:         tokenMode,
		ToolApprovalMode:  approvalMode,
	}, nil
}

func resolveModel(cfg *config.Config, requested *string) (*config.ProviderEntry, string, error) {
	if cfg == nil {
		return nil, "", profileError(protocol.ErrInvalidProfile, errors.New("Host configuration is nil"))
	}
	model := strings.TrimSpace(cfg.DefaultModel)
	if requested != nil {
		model = strings.TrimSpace(*requested)
		if model == "" {
			return nil, "", profileError(protocol.ErrInvalidProfile, errors.New("model selection is empty"))
		}
	}
	entry, ok := cfg.ResolveModel(model)
	if !ok || entry == nil || strings.TrimSpace(entry.Name) == "" || strings.TrimSpace(entry.Model) == "" {
		return nil, "", profileError(protocol.ErrModelNotAvailable, fmt.Errorf("model %q is not configured", model))
	}
	if !providerAllowed(cfg.Desktop.ProviderAccess, entry.Name) || !entry.Configured() || !config.IsLikelyChatModel(entry.Model) {
		return nil, "", profileError(protocol.ErrModelNotAvailable, fmt.Errorf("model %q is not available for Remote chat", model))
	}
	return entry, entry.Name + "/" + entry.Model, nil
}

func providerAllowed(names []string, provider string) bool {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			allowed[name] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[strings.TrimSpace(provider)]
	return ok
}

func resolveEffort(entry *config.ProviderEntry, requested *string) (string, error) {
	if entry == nil {
		return "", profileError(protocol.ErrInvalidProfile, errors.New("resolved model entry is nil"))
	}
	raw := config.EffortDisplay(entry)
	if requested != nil {
		raw = strings.TrimSpace(*requested)
		if raw == "" {
			return "", profileError(protocol.ErrInvalidProfile, errors.New("effort selection is empty"))
		}
	}
	normalized, err := config.NormalizeEffort(entry, raw)
	if err != nil {
		return "", profileError(protocol.ErrEffortNotSupported, fmt.Errorf("model %s/%s: %w", entry.Name, entry.Model, err))
	}
	if normalized != "" {
		return config.EffortDisplay(&config.ProviderEntry{Effort: normalized}), nil
	}

	// "auto" is not persisted as an unresolved empty value. Freeze the current
	// capability default when one exists so a later Host config edit cannot
	// silently change a cold Session's reasoning depth. Providers whose default
	// is intentionally provider-defined retain the explicit wire value "auto".
	copy := *entry
	copy.Effort = ""
	capability := config.EffortCapabilityForEntry(&copy)
	if capability.Supported && strings.TrimSpace(capability.Default) != "" && capability.Default != "auto" {
		resolvedDefault, normalizeErr := config.NormalizeEffort(&copy, capability.Default)
		if normalizeErr != nil || strings.TrimSpace(resolvedDefault) == "" {
			if normalizeErr == nil {
				normalizeErr = errors.New("configured effort default resolves to an empty value")
			}
			return "", profileError(protocol.ErrEffortNotSupported, fmt.Errorf("model %s/%s: %w", entry.Name, entry.Model, normalizeErr))
		}
		return config.EffortDisplay(&config.ProviderEntry{Effort: resolvedDefault}), nil
	}
	return "auto", nil
}

func resolveCollaboration(requested *protocol.CollaborationMode) (protocol.CollaborationMode, bool, error) {
	if requested == nil {
		return protocol.CollaborationNormal, false, nil
	}
	raw := strings.ToLower(strings.TrimSpace(string(*requested)))
	switch raw {
	case string(protocol.CollaborationNormal):
		return protocol.CollaborationNormal, false, nil
	case string(protocol.CollaborationPlan):
		return protocol.CollaborationPlan, false, nil
	case string(protocol.CollaborationGoal):
		return protocol.CollaborationGoal, false, nil
	case "yolo": // pre-Remote Desktop BranchMeta encoded both axes in Mode
		return protocol.CollaborationNormal, true, nil
	case "plan-yolo", "yolo-plan":
		return protocol.CollaborationPlan, true, nil
	default:
		return "", false, profileError(protocol.ErrInvalidProfile, fmt.Errorf("invalid collaboration mode %q", raw))
	}
}

func resolveTokenMode(requested *protocol.TokenMode) (protocol.TokenMode, error) {
	if requested == nil {
		return protocol.TokenFull, nil
	}
	raw := strings.ToLower(strings.TrimSpace(string(*requested)))
	switch raw {
	case string(protocol.TokenFull), "balanced":
		return protocol.TokenMode(boot.NormalizeTokenMode(raw)), nil
	case string(protocol.TokenEconomy), "eco", "save", "saving", "low", "lite", "minimal":
		return protocol.TokenMode(boot.NormalizeTokenMode(raw)), nil
	case string(protocol.TokenDelivery), "deliver", "quality", "performance":
		return protocol.TokenMode(boot.NormalizeTokenMode(raw)), nil
	default:
		return "", profileError(protocol.ErrInvalidProfile, fmt.Errorf("invalid token mode %q", raw))
	}
}

func resolveToolApproval(requested *protocol.ToolApprovalMode, legacyYOLO bool) (protocol.ToolApprovalMode, error) {
	if requested == nil {
		if legacyYOLO {
			return protocol.ToolApprovalYOLO, nil
		}
		return protocol.ToolApprovalAsk, nil
	}
	raw := strings.ToLower(strings.TrimSpace(string(*requested)))
	switch raw {
	case string(protocol.ToolApprovalAsk), string(protocol.ToolApprovalAuto), string(protocol.ToolApprovalYOLO), "full", "full-access", "bypass":
		return protocol.ToolApprovalMode(config.NormalizeToolApprovalMode(raw)), nil
	default:
		return "", profileError(protocol.ErrInvalidProfile, fmt.Errorf("invalid tool approval mode %q", raw))
	}
}

func profileError(code protocol.ReasonixErrorCode, detail error) error {
	return &catalog.Error{Code: code, Detail: detail}
}
