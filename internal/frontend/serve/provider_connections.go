package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

type connectionEntry struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Models      int    `json:"models"`
	Active      bool   `json:"active"`
	KeyRequired bool   `json:"keyRequired"`
}

type connectionList struct {
	Revision    string            `json:"revision"`
	Connections []connectionEntry `json:"connections"`
}

type connectionRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
	Revision string `json:"revision"`
}

var (
	errConnectionUnknown   = refusal(http.StatusNotFound, "provider.unknown", errors.New("no such model connection"), nil)
	errConnectionNoKeySlot = refusal(http.StatusConflict, "provider.no_key_slot", errors.New("this connection has no credential variable to store a key in"), nil)
	errConnectionStale     = refusal(http.StatusConflict, "provider.credentials_changed", errors.New("stored credentials changed; reload the connections"), nil)
	errConnectionActivate  = refusal(http.StatusServiceUnavailable, "provider.activation_failed", errors.New("the key was saved but the connection could not be activated; retry"), nil)
)

// EnableProviderSetupInProcess opens the setup surface for a host that serves
// its own window and has no listener to vet.
func (h *Hub) EnableProviderSetupInProcess() {
	for _, rt := range h.localRuntimes() {
		rt.Server.EnableProviderSetup()
	}
}

// connectionSurface admits only the host's own in-process window: the routes
// write or probe any provider's key, which a listener must never allow.
func (s *Server) connectionSurface(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	state, ok := s.providerSetupSnapshot()
	if !ok || !state.InProcess || !hostReach(r) {
		http.NotFound(w, r)
		return false
	}
	return true
}

func (s *Server) connectionsConfig() (*config.Config, error) {
	return config.LoadForRootReadOnly(s.ctl().WorkspaceRoot())
}

func (s *Server) connectionsList(w http.ResponseWriter, r *http.Request) {
	if !s.connectionSurface(w, r) {
		return
	}
	cfg, err := s.connectionsConfig()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	current, _, _ := strings.Cut(currentModelRef(s.ctl()), "/")
	out := connectionList{Revision: config.CredentialStoreRevision(), Connections: []connectionEntry{}}
	for i := range cfg.Providers {
		entry := &cfg.Providers[i]
		if strings.TrimSpace(entry.Name) == "" || len(entry.ModelList()) == 0 {
			continue
		}
		out.Connections = append(out.Connections, connectionEntry{
			Name: entry.Name, Kind: string(entry.Kind), Models: len(entry.ModelList()),
			Active: entry.Name == current, KeyRequired: entry.RequiresAPIKey() && entry.APIKey() == "",
		})
	}
	writeJSON(w, out)
}

func decodeConnectionRequest(w http.ResponseWriter, r *http.Request) (connectionRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, providerSetupMaxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body connectionRequest
	if err := dec.Decode(&body); err != nil || ensureProviderSetupJSONEOF(dec) != nil {
		badBody(w)
		return body, false
	}
	body.Provider = strings.TrimSpace(body.Provider)
	body.APIKey = strings.TrimSpace(body.APIKey)
	if body.APIKey == "" {
		writeErr(w, http.StatusBadRequest, errProviderSetupAPIKeyRequired)
		return body, false
	}
	if len(body.APIKey) > 16<<10 || strings.ContainsAny(body.APIKey, "\r\n") {
		refuse(w, http.StatusBadRequest, "provider.key_invalid", "API key is too large or spans lines", nil)
		return body, false
	}
	return body, true
}

// probeRefusal names why a probe failed from the error's type; the provider's
// own text can echo the key or the endpoint, so it goes to the log only.
func probeRefusal(err error) (int, string, string) {
	var auth *provider.AuthError
	var api *provider.APIError
	var netErr net.Error
	switch {
	case errors.As(err, &auth):
		return http.StatusUnauthorized, "provider.test_auth", "the provider rejected the key"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "provider.test_timeout", "the provider did not answer in time"
	case errors.As(err, &netErr):
		return http.StatusBadGateway, "provider.test_unreachable", "the provider could not be reached"
	case errors.As(err, &api):
		return http.StatusBadGateway, "provider.test_upstream", "the provider returned an error"
	}
	return http.StatusBadGateway, "provider.test_failed", "the connection test failed"
}

func (s *Server) connectionTest(w http.ResponseWriter, r *http.Request) {
	if !s.connectionSurface(w, r) {
		return
	}
	body, ok := decodeConnectionRequest(w, r)
	if !ok {
		return
	}
	cfg, err := s.connectionsConfig()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	entry, found := cfg.Provider(body.Provider)
	if !found {
		writeErr(w, http.StatusNotFound, errConnectionUnknown)
		return
	}
	probe := *entry
	if probe.Model == "" {
		probe.Model = probe.Default
		if models := probe.ModelList(); probe.Model == "" && len(models) > 0 {
			probe.Model = models[0]
		}
	}
	if err := boot.ProbeProviderConnection(r.Context(), probe, body.APIKey, cfg.NetworkProxySpec()); err != nil {
		status, code, msg := probeRefusal(err)
		slog.Warn("serve: connection test failed", "provider", entry.Name, "code", code, "status", status)
		refuse(w, status, code, msg, nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) connectionSave(w http.ResponseWriter, r *http.Request) {
	if !s.connectionSurface(w, r) {
		return
	}
	body, ok := decodeConnectionRequest(w, r)
	if !ok {
		return
	}
	if err := s.saveConnectionCredential(r.Context(), body); err != nil {
		var named *coded
		if errors.As(err, &named) {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		slog.Warn("serve: connection setup failed", "type", fmt.Sprintf("%T", err))
		refuse(w, http.StatusInternalServerError, "provider.setup_failed", "unable to complete provider setup", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// saveConnectionCredential stores the key under the provider's own credential
// variable only if the store is still at the revision the caller listed, and
// never rewrites config.toml. Only the active provider needs the controller
// rebuilt. A retry after a failed activation finds its own key already stored
// and goes straight to activating.
func (s *Server) saveConnectionCredential(ctx context.Context, req connectionRequest) error {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()

	cfg, err := s.connectionsConfig()
	if err != nil {
		return err
	}
	entry, ok := cfg.Provider(req.Provider)
	if !ok {
		return errConnectionUnknown
	}
	keyEnv := strings.TrimSpace(entry.APIKeyEnv)
	if !config.IsValidCredentialKey(keyEnv) {
		return errConnectionNoKeySlot
	}
	_, applied, err := config.SetCredentialIfRevision(keyEnv, req.APIKey, req.Revision)
	if err != nil {
		return fmt.Errorf("save provider credential: %w", err)
	}
	if !applied {
		if stored := config.ResolveCredentialForRootGlobalFirst(s.ctl().WorkspaceRoot(), keyEnv); !stored.Set || stored.Value != req.APIKey {
			return errConnectionStale
		}
	}
	ref := currentModelRef(s.ctl())
	if active, _, _ := strings.Cut(ref, "/"); active == req.Provider {
		if err := s.switchModelLocked(ctx, ref); err != nil {
			slog.Warn("serve: activate provider after key save", "type", fmt.Sprintf("%T", err))
			return errConnectionActivate
		}
	}
	s.refreshProviderSetup(currentModelRef(s.ctl()))
	return nil
}
