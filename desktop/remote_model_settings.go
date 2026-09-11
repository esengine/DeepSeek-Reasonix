package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/config"
)

type remoteModelSettingsStatus struct {
	config.ModelSettingsOwnership
	Version           int      `json:"version"`
	Revision          string   `json:"revision"`
	Model             string   `json:"model"`
	SessionPath       string   `json:"sessionPath"`
	OwnedRevisions    []string `json:"ownedRevisions"`
	UnversionedOwners bool     `json:"unversionedOwners"`
}

func credentialProxyScope(host, workspace string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d:%s%s", len(host), host, workspace))
	return hex.EncodeToString(sum[:])
}

// Status refreshes already run when foreground/background work settles. Read
// ownership under the manager gate, then reject receipts overtaken by source
// refreshes using the Serve-wide ownership sequence.
func (a *App) refreshRemoteModelOwnership(ctx context.Context, tabID string, client *http.Client, generation uint64) {
	a.remoteMu.Lock()
	manager, ok := a.remoteRuntime.(*desktopRemoteManager)
	a.remoteMu.Unlock()
	if !ok {
		return
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.client != client || tab.gen != generation || tab.settings.revision == "" {
		a.remoteTabMu.Unlock()
		return
	}
	host, workspace, path := tab.ref.HostID, tab.ref.Workspace, tab.routing.currentPath
	a.remoteTabMu.Unlock()
	managed := manager.managed(host)
	if managed == nil {
		return
	}
	managed.serveMu.Lock()
	defer managed.serveMu.Unlock()
	if !manager.isCurrent(host, managed) {
		return
	}
	manager.mu.Lock()
	server := managed.serves[workspace]
	base := ""
	if server != nil {
		base = server.view.LocalURL
	}
	manager.mu.Unlock()
	if base == "" {
		return
	}
	status, err := remoteModelSettingsRequest(ctx, client, base, path, nil)
	if err == nil && manager.isCurrent(host, managed) {
		if !a.reconcileCredentialProxyGenerations(host, workspace, status) {
			return
		}
		// Serve may have applied a new snapshot itself for a browser or queued
		// turn. Reflect that acknowledgement on the same bound Desktop target.
		if cfg, loadErr := config.LoadModelRuntimeSnapshot("."); loadErr == nil {
			for _, provider := range cfg.Providers {
				for _, modelID := range provider.ModelList() {
					ref := provider.Name + "/" + modelID
					if remoteSnapshotProviderName(provider.Name, modelID)+"/"+modelID != status.Model || cfg.ModelRuntimeFingerprint(ref) != status.Revision {
						continue
					}
					a.remoteTabMu.Lock()
					if current := a.remoteTabs[tabID]; current == tab && current.client == client && current.gen == generation && current.routing.currentPath == path && status.SessionPath == path {
						current.model, current.settings.revision = ref, status.Revision
						current.settings.generation, current.settings.sessionPath = generation, path
						current.settings.failure, current.settings.failureRevision = "", ""
					}
					a.remoteTabMu.Unlock()
				}
			}
		}
	}
}

// Each model has its own immutable tunnel token. Provider names are stable
// across revisions; only the virtual credentials and resolver content change.
func remoteSnapshotProviderName(provider, model string) string {
	sum := sha256.Sum256([]byte(model))
	return provider + "-" + hex.EncodeToString(sum[:6])
}

func (a *App) buildRemoteModelSettings(host, workspace, model string, remotePort int, cfg *config.Config) (*config.ModelRuntimeSettings, string, error) {
	offerID, err := config.NewModelSettingsOfferID()
	if err != nil {
		return nil, "", err
	}
	return a.buildRemoteModelSettingsOffer(host, workspace, model, remotePort, cfg, offerID)
}

func (a *App) buildRemoteModelSettingsOffer(host, workspace, model string, remotePort int, cfg *config.Config, offerID string) (_ *config.ModelRuntimeSettings, _ string, buildErr error) {
	defer func() {
		if buildErr != nil {
			a.finishCredentialProxyOffer(host, workspace, offerID)
		}
	}()
	revision := cfg.ModelRuntimeFingerprint(model)
	bundle := &config.ModelRuntimeSettings{OfferID: offerID, Revision: revision, ProxyURL: fmt.Sprintf("http://127.0.0.1:%d", remotePort), Credentials: map[string]string{}, Preferences: cfg.RuntimeModelPreferences()}
	refs := map[string]string{}
	for _, base := range cfg.Providers {
		if !base.Configured() || !modelProviderAccessAllowed(cfg.Desktop.ProviderAccess, base.Name) {
			continue
		}
		for _, modelID := range base.ModelList() {
			ref := base.Name + "/" + modelID
			entry, ok := cfg.ResolveModel(ref)
			if !ok {
				continue
			}
			route, err := a.applyCredentialProxySnapshot(host, workspace, ref, cfg, revision, offerID)
			if err != nil {
				return nil, "", err
			}
			p := *entry
			p.Name = remoteSnapshotProviderName(base.Name, modelID)
			p.ModelsURL, p.BalanceURL, p.APIKeyEnv = "", "", ""
			p.Headers, p.ExtraBody = nil, nil
			p.NoProxy = true
			p.Model, p.Default, p.Models = modelID, "", nil
			bundle.Providers = append(bundle.Providers, p)
			bundle.Credentials[p.Name] = route.token
			if ref == model {
				bundle.SourceToken = route.token
			}
			refs[ref] = p.Name + "/" + modelID
			if modelID == base.DefaultModel() {
				refs[base.Name] = refs[ref]
			}
		}
	}
	mapRef := func(ref string) string {
		if mapped := refs[ref]; mapped != "" {
			return mapped
		}
		return ref
	}
	p := &bundle.Preferences
	p.PlannerModel, p.VisionModel, p.WebSearchModel = mapRef(p.PlannerModel), mapRef(p.VisionModel), mapRef(p.WebSearchModel)
	p.GuardianModel, p.RecoveryModel, p.SubagentModel = mapRef(p.GuardianModel), mapRef(p.RecoveryModel), mapRef(p.SubagentModel)
	p.SubagentModels = map[string]string{}
	for name, ref := range cfg.Agent.SubagentModels {
		p.SubagentModels[name] = mapRef(ref)
	}
	if refs[model] == "" {
		return nil, "", fmt.Errorf("no available remote model in saved settings")
	}
	bundle.References = refs
	return bundle, refs[model], nil
}

func (a *App) finishCredentialProxyOffer(host, workspace, offerID string) {
	if offerID == "" {
		return
	}
	a.credProxyMu.Lock()
	p := a.credProxy
	a.credProxyMu.Unlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	scope := credentialProxyScope(host, workspace)
	for _, route := range p.routes {
		if route.scope == scope {
			delete(route.holds, offerID)
		}
	}
}

// updateMu serializes reservations; releasing one only needs the route lock.
// Refuse excess candidates without evicting a runtime or an accepted request.
func (p *credentialProxy) validateModelSettingsOfferCapacity(up proxyUpstream) error {
	if up.offerID == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	offers := map[string]bool{}
	for _, route := range p.routes {
		if route.scope != up.scope {
			continue
		}
		if route.holds[up.offerID] {
			return nil
		}
		for id := range route.holds {
			offers[id] = true
		}
	}
	if len(offers) >= 64 {
		return fmt.Errorf("too many unacknowledged model settings offers for this session")
	}
	return nil
}

// A managed Serve can refresh at its own run boundary, including durable
// inbox dispatch. Authentication is the currently owned immutable route token;
// the request cannot choose a local workspace or disclose a real credential.
func (a *App) serveModelSettingsSource(w http.ResponseWriter, r *http.Request, route *credProxyRoute) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request config.ModelSettingsSourceRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || len(request.OfferID) != 32 || (request.Mode != "prepare" && request.Mode != "finish") {
		http.Error(w, "invalid model settings request", http.StatusBadRequest)
		return
	}
	if _, err := hex.DecodeString(request.OfferID); err != nil {
		http.Error(w, "invalid model settings offer", http.StatusBadRequest)
		return
	}
	offers := []string{request.PreviousOfferID}
	if request.Mode == "finish" {
		offers = append(offers, request.OfferID)
	}
	if !a.reconcileCredentialProxyGenerations(route.host, route.workspace, remoteModelSettingsStatus{ModelSettingsOwnership: request.ModelSettingsOwnership, Version: 1, OwnedRevisions: request.OwnedRevisions, UnversionedOwners: request.UnversionedOwners}, offers...) {
		http.Error(w, "stale model settings ownership", http.StatusConflict)
		return
	}
	cfg, err := config.LoadModelRuntimeSnapshot(".")
	if err != nil {
		http.Error(w, "cannot read saved model settings", http.StatusServiceUnavailable)
		return
	}
	model := request.Model
	if model == "" {
		model = route.ref
	}
	model, err = resolveModelSettingsRuntime(cfg, model)
	if err != nil {
		http.Error(w, "choose an available model in Desktop Settings before starting another run", http.StatusConflict)
		return
	}
	response := config.ModelSettingsSourceResponse{Version: 1, Revision: cfg.ModelRuntimeFingerprint(model)}
	if request.Mode == "prepare" && request.AppliedRevision != response.Revision {
		if request.RemotePort < 1 || request.RemotePort > 65535 {
			http.Error(w, "invalid credential tunnel port", http.StatusBadRequest)
			return
		}
		response.Settings, response.Ref, err = a.buildRemoteModelSettingsOffer(route.host, route.workspace, model, request.RemotePort, cfg, request.OfferID)
		if err != nil {
			http.Error(w, "cannot prepare saved model settings", http.StatusConflict)
			return
		}
		if !a.reserveCredentialProxyInstall(route.host, route.workspace, request.OfferID, response.Revision, request.OwnershipIncarnation) {
			a.finishCredentialProxyOffer(route.host, route.workspace, request.OfferID)
			http.Error(w, "model settings owner was replaced", http.StatusConflict)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

const remoteModelSettingsUpgradeHint = "saved model settings require a newer remote Serve; upgrade or safely reconnect after its current work finishes"

type remoteModelSettingsRejection struct {
	message string
	// unsupported marks rejections that mean the remote Serve predates the
	// model-settings protocol entirely (no /model-settings route at all).
	unsupported bool
}

func (e *remoteModelSettingsRejection) Error() string { return e.message }

// isRemoteModelSettingsUnsupported reports whether err means the remote Serve
// cannot speak the model-settings protocol, as opposed to refusing a specific
// snapshot (ownership conflicts, busy turns, and other transient rejections).
func isRemoteModelSettingsUnsupported(err error) bool {
	var rejected *remoteModelSettingsRejection
	return errors.As(err, &rejected) && rejected.unsupported
}

func remoteModelSettingsRequest(ctx context.Context, client *http.Client, base, expectedPath string, body any) (remoteModelSettingsStatus, error) {
	var result remoteModelSettingsStatus
	method := http.MethodGet
	var data []byte
	if body != nil {
		method = http.MethodPost
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return result, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, serveURL(base, "/model-settings"), bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	if expectedPath != "" {
		req.Header.Set(expectedSessionPathHeader, expectedPath)
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return result, &remoteModelSettingsRejection{message: remoteModelSettingsUpgradeHint, unsupported: true}
	}
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return result, &remoteModelSettingsRejection{message: fmt.Sprintf("remote model settings status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))}
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		// Older Serves answer unknown routes with the HTML index; classify that
		// document response as an unsupported model-settings protocol.
		if trimmed := bytes.TrimSpace(payload); len(trimmed) > 0 && trimmed[0] == '<' {
			return result, &remoteModelSettingsRejection{message: remoteModelSettingsUpgradeHint, unsupported: true}
		}
		return result, fmt.Errorf("decode remote model settings status: %w", err)
	}
	if result.Version != 1 {
		return result, fmt.Errorf("remote Serve does not support immutable model settings")
	}
	return result, nil
}

// ensureRemoteModelSettings is the Desktop remote run-admission boundary.
// Approval/ask/steer replies bypass it because they belong to the accepted run.
// The returned generation scopes the admission (revision or legacy skip) to one
// tab generation; callers must re-admit before a target that no longer runs it.
func (a *App) ensureRemoteModelSettings(tabID string) (string, uint64, error) {
	if !a.remoteTabLocalProxy(tabID) {
		return "", 0, nil
	}
	for {
		cfg, err := config.LoadModelRuntimeSnapshot(".")
		if err != nil {
			return "", 0, err
		}
		a.remoteTabMu.Lock()
		tab := a.remoteTabs[tabID]
		if tab == nil {
			a.remoteTabMu.Unlock()
			return "", 0, fmt.Errorf("remote session is no longer available")
		}
		model, applied, generation, path := tab.model, tab.settings.revision, tab.gen, tab.routing.currentPath
		valid := tab.settings.generation == generation && tab.settings.sessionPath == path
		// Generation 0 is an unrecorded verdict, not a legacy one: fresh and
		// restored tabs run generation 0 before their first attachment.
		unsupported := tab.settings.unsupportedGen != 0 && tab.settings.unsupportedGen == generation
		a.remoteTabMu.Unlock()
		if unsupported {
			return "", generation, nil
		}
		if model == "" {
			model = resolveNewSessionModel(cfg)
		}
		desired := cfg.ModelRuntimeFingerprint(model)
		if valid && applied == desired {
			return applied, generation, nil
		}
		next, err := resolveModelSettingsRuntime(cfg, model)
		if err == nil {
			err = a.SetRemoteTabModel(tabID, next)
		}
		if err != nil {
			if isRemoteModelSettingsUnsupported(err) {
				// A legacy Serve cannot apply snapshots; admit without a revision
				// only when the verdict belongs to the current connection.
				a.remoteTabMu.Lock()
				current := a.remoteTabs[tabID]
				recorded := current == tab && current.gen == generation && current.routing.currentPath == path
				if recorded {
					current.settings.unsupportedGen = generation
				}
				a.remoteTabMu.Unlock()
				if !recorded {
					// A reconnect replaced the probe's target mid-flight; the
					// replacement Serve may speak the protocol, so re-probe it
					// instead of admitting against the retired fence.
					continue
				}
				return "", generation, nil
			}
			a.remoteTabMu.Lock()
			if current := a.remoteTabs[tabID]; current == tab && current.gen == generation && current.routing.currentPath == path {
				current.settings.failure = modelSettingsIssue("apply_failed", err).Message
				current.settings.failureRevision = desired
			}
			a.remoteTabMu.Unlock()
			return "", 0, fmt.Errorf("model settings were saved but the remote session could not apply them: %w", err)
		}
		a.remoteTabMu.Lock()
		acknowledged := tab.settings.revision != "" && tab.settings.generation == tab.gen && tab.settings.sessionPath == tab.routing.currentPath
		a.remoteTabMu.Unlock()
		if !acknowledged {
			return "", 0, fmt.Errorf("remote session did not acknowledge the saved model settings")
		}
		// Read the current file again: another save may have won during the
		// remote build. Never admit a new request with that stale completion.
	}
}

func (a *App) appendRemoteModelSettingsStatus(result *ModelSettingsResult) {
	cfg, err := config.LoadModelRuntimeSnapshot(".")
	if err != nil {
		return
	} // the global read failure is already in the result
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	for _, tab := range a.remoteTabs {
		if tab == nil {
			continue
		}
		host, ok := cfg.RemoteHost(tab.ref.HostID)
		if !ok || !host.CredentialProxyEnabled() {
			continue
		}
		model := tab.model
		if model == "" {
			model = resolveNewSessionModel(cfg)
		}
		desired := cfg.ModelRuntimeFingerprint(model)
		// Generation 0 is an unrecorded verdict: a not-yet-attached tab has not
		// probed any Serve and must stay pending, not claim not_required.
		if tab.settings.unsupportedGen != 0 && tab.settings.unsupportedGen == tab.gen {
			// Legacy targets never apply snapshots, so report them as not required
			// instead of leaving the settings receipt pending indefinitely.
			result.Targets = append(result.Targets, ModelSettingsTarget{TabID: tab.id, Title: tab.topicTitle, Application: "not_required", AppliedRevision: tab.settings.revision, DesiredRevision: desired})
			continue
		}
		state := "applied"
		if tab.settings.revision != desired || tab.settings.generation != tab.gen || tab.settings.sessionPath != tab.routing.currentPath {
			state = "pending"
			if tab.settings.failure != "" && tab.settings.failureRevision == desired {
				state = "failed"
				result.Application = "failed"
				result.Issues = append(result.Issues, ModelSettingsIssue{Code: "remote_apply_failed", Message: tab.settings.failure})
			} else if result.Application != "failed" {
				result.Application = "pending"
			}
		}
		result.Targets = append(result.Targets, ModelSettingsTarget{TabID: tab.id, Title: tab.topicTitle, Application: state, AppliedRevision: tab.settings.revision, DesiredRevision: desired})
	}
}

// remoteModelApplicationState is scoped to one acknowledged session binding.
type remoteModelApplicationState struct {
	revision        string
	failure         string
	failureRevision string
	generation      uint64
	sessionPath     string
	// unsupportedGen records a generation whose Serve predates model-settings;
	// a reconnect or replacement generation probes the protocol again.
	unsupportedGen uint64
}

func applyRemoteModelSettingsSnapshot(ctx context.Context, client *http.Client, base, expectedPath, remoteRef string, bundle *config.ModelRuntimeSettings, status remoteModelSettingsStatus) (remoteModelSettingsStatus, error) {
	var err error
	if status.Revision != bundle.Revision || status.Model != remoteRef {
		body := map[string]any{"version": 1, "ref": remoteRef, "settings": bundle}
		status, err = remoteModelSettingsRequest(ctx, client, base, expectedPath, body)
		if err != nil {
			readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			observed, readErr := remoteModelSettingsRequest(readCtx, client, base, expectedPath, nil)
			if readErr != nil {
				return status, err
			}
			if observed.Revision != bundle.Revision || observed.Model != remoteRef {
				return observed, err
			}
			status = observed
		}
	}
	if status.Revision != bundle.Revision || status.Model != remoteRef {
		return status, fmt.Errorf("remote model settings acknowledgement did not match the submitted snapshot")
	}
	return status, nil
}
