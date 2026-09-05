package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/provider"
)

// TestProviderModel sends a bounded, tool-free probe through the configured
// adapter. It does not create a session or change persisted settings.
func (a *App) TestProviderModel(p ProviderView, model, key string) error {
	root := a.activeWorkspaceRoot()
	cfg, err := config.LoadForRootWithoutCredentialsReadOnly(root)
	if err != nil {
		return err
	}
	if err := saveProviderConfig(cfg, p); err != nil {
		return err
	}
	entry, ok := cfg.ResolveModel(p.Name + "/" + strings.TrimSpace(model))
	if !ok {
		return fmt.Errorf("model is not in this provider's configured list")
	}
	entry.ResolveAPIKeyForRoot(root)
	if strings.TrimSpace(key) != "" {
		copied := entry.WithAPIKeyForProbe(key)
		entry = &copied
	}
	client, err := boot.NewProviderWithProxy(entry, withProbeDirectHost(a.networkProxySpecForRoot(root), entry.BaseURL, p.NoProxy))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), 20*time.Second)
	defer cancel()
	chunks, err := client.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: "user", Content: "Reply with OK."}}, MaxTokens: 16})
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, open := <-chunks:
			if !open {
				return fmt.Errorf("provider closed the connection without a response")
			}
			if chunk.Err != nil {
				return chunk.Err
			}
			if chunk.Type == provider.ChunkText && strings.TrimSpace(chunk.Text) != "" {
				return nil
			}
			if chunk.Type == provider.ChunkDone {
				return nil
			}
		}
	}
}

// FetchProviderModelCatalogDraft discovers models using an unsaved credential.
// Draft secrets are request-local and never enter the settings response or cache.
func (a *App) FetchProviderModelCatalogDraft(p ProviderView, key string) ([]ProviderModelCapabilityView, error) {
	root := a.activeWorkspaceRoot()
	// Capture persisted identity separately from the editor draft. Draft routes
	// may differ legitimately; a saved provider changing during discovery may not.
	savedIdentity := func() string {
		cfg, err := config.LoadForRootWithoutCredentialsReadOnly(root)
		if err != nil {
			return ""
		}
		for _, entry := range cfg.Providers {
			if entry.Name == p.Name {
				return providerModelCatalogFingerprint(entry)
			}
		}
		return ""
	}
	before := savedIdentity()
	e := config.ProviderEntry{
		Name:       p.Name,
		Kind:       p.Kind,
		BaseURL:    p.BaseURL,
		ModelsURL:  strings.TrimSpace(p.ModelsURL),
		APIKeyEnv:  p.APIKeyEnv,
		Headers:    p.Headers,
		AuthHeader: p.AuthHeader,
		NoProxy:    p.NoProxy,
		ChatURL:    p.ChatURL,
		RequestURL: p.RequestURL,
	}
	started := time.Now()
	credentialsRevision := config.CredentialStoreRevision()
	e.ResolveAPIKeyForRoot(root)
	if strings.TrimSpace(key) != "" {
		e = e.WithAPIKeyForProbe(key)
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), 15*time.Second)
	defer cancel()
	models, err := e.FetchModelCatalogWithProxy(ctx, withProbeDirectHost(a.networkProxySpecForRoot(root), e.BaseURL, p.NoProxy))
	if err != nil {
		return []ProviderModelCapabilityView{}, err
	}
	// Credential changes invalidate a result even when the endpoint stayed the same.
	unlockConfig := config.LockUserConfigEdits()
	defer unlockConfig()
	unlockCredentials, err := config.LockUserCredentialEdits()
	if err != nil {
		return []ProviderModelCapabilityView{}, err
	}
	defer unlockCredentials()
	if credentialsRevision != config.CredentialStoreRevision() || before != savedIdentity() {
		return []ProviderModelCapabilityView{}, fmt.Errorf("model discovery configuration changed; fetch again")
	}
	capabilities := config.NewModelCapabilityResolver()
	if strings.TrimSpace(key) != "" {
		capabilities = config.NewTransientModelCapabilityResolver()
	}
	capabilities.PutCatalogAt(e, models, started)
	// Only adapter facts enter the cache. User choices apply to the returned view.
	if cfg, err := config.LoadForRootWithoutCredentialsReadOnly(root); err == nil {
		for _, saved := range cfg.Providers {
			if saved.Name == p.Name {
				e.PresetID, e.Vision = saved.PresetID, saved.Vision
				break
			}
		}
	}
	e.VisionModels = p.VisionModels
	e.ModelOverrides = providerModelOverridesForSave(p.ModelOverrides, nil)
	result := make([]ProviderModelCapabilityView, 0, len(models))
	for _, model := range models {
		entry := e
		entry.Model = model.ID
		resolved := capabilities.Resolve(&entry)
		result = append(result, modelCapabilityView(resolved))
	}
	return result, nil
}
