package serve

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"reasonix/internal/contract/config"
)

const providerSetupMaxBody = 20 << 10

type providerSetupState struct {
	Enabled            bool   `json:"-"`
	InProcess          bool   `json:"-"`
	Required           bool   `json:"required"`
	ActivationPending  bool   `json:"activationPending,omitempty"`
	Provider           string `json:"provider,omitempty"`
	Model              string `json:"model,omitempty"`
	ModelRef           string `json:"modelRef,omitempty"`
	KeyEnv             string `json:"keyEnv,omitempty"`
	CredentialRevision string `json:"-"`
	Error              string `json:"error,omitempty"`
}

// EnableProviderSetup enables the setup surface for a host that serves this API
// in-process to its own window. There is no listener to vet — no address exists
// for another machine to reach — so the loopback rule below has nothing to
// check. A host that does listen must use EnableProviderSetupForListener.
func (s *Server) EnableProviderSetup() {
	if s == nil {
		return
	}
	s.providerSetupMu.Lock()
	s.providerSetup.Enabled, s.providerSetup.InProcess = true, true
	s.providerSetupMu.Unlock()
	s.refreshProviderSetup(currentModelRef(s.ctl()))
}

// EnableProviderSetupForListener enables the credential-writing setup surface
// only for loopback listeners. Remote Desktop reaches it through an SSH tunnel;
// a directly exposed HTTP listener must never accept provider secrets.
func (s *Server) EnableProviderSetupForListener(addr string) bool {
	if s == nil || !isLoopbackHost(addr) {
		return false
	}
	s.providerSetupMu.Lock()
	s.providerSetup.Enabled = true
	s.providerSetupMu.Unlock()
	s.refreshProviderSetup(currentModelRef(s.ctl()))
	return true
}

func (s *Server) refreshProviderSetup(ref string) {
	s.providerSetupMu.RLock()
	enabled, inProcess := s.providerSetup.Enabled, s.providerSetup.InProcess
	s.providerSetupMu.RUnlock()
	if !enabled {
		return
	}

	next := providerSetupState{Enabled: true, InProcess: inProcess}
	// Resolve the missing-key state and its credential-file revision under the
	// same cross-process lock used by every writer. This prevents capturing a
	// stale "missing" snapshot paired with a newer revision.
	unlockCredentials, lockErr := config.LockUserCredentialEdits()
	if lockErr != nil {
		next.Error = "Unable to inspect the remote Reasonix credentials."
	} else {
		// Keep the credential snapshot atomic without reversing the documented
		// config -> credential lock order. Provider setup only inspects config, so
		// it must not run on-disk migrations or acquire a config edit lock here.
		cfg, err := config.LoadForRootReadOnly(".")
		if err != nil {
			next.Error = "Unable to load the remote Reasonix configuration."
		} else if entry, ok := cfg.ResolveModel(strings.TrimSpace(ref)); ok && entry.RequiresAPIKey() && entry.APIKey() == "" && config.IsValidCredentialKey(entry.APIKeyEnv) {
			next.Required = true
			next.Provider = entry.Name
			next.Model = entry.Model
			next.ModelRef = entry.Name + "/" + entry.Model
			next.KeyEnv = strings.TrimSpace(entry.APIKeyEnv)
			next.CredentialRevision = config.CredentialStoreRevision()
		}
		unlockCredentials()
	}

	s.providerSetupMu.Lock()
	s.providerSetup = next
	s.providerSetupMu.Unlock()
}

func (s *Server) providerSetupSnapshot() (providerSetupState, bool) {
	if s == nil {
		return providerSetupState{}, false
	}
	s.providerSetupMu.RLock()
	defer s.providerSetupMu.RUnlock()
	return s.providerSetup, s.providerSetup.Enabled
}

func (s *Server) providerSetupStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !hostReach(r) {
		http.NotFound(w, r)
		return
	}
	setup, ok := s.providerSetupSnapshot()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, setup)
}

func (s *Server) providerSetupSave(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// The surface opens for the listener the host named, and a device reaches
	// the same handler through another one.
	if !hostReach(r) {
		refuse(w, http.StatusNotFound, codeDeviceHostOnly, "a paired device cannot use this", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, providerSetupMaxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body struct {
		APIKey string `json:"apiKey"`
	}
	if err := dec.Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := ensureProviderSetupJSONEOF(dec); err != nil {
		badBody(w)
		return
	}
	key := strings.TrimSpace(body.APIKey)
	if len(key) > 16<<10 {
		refuse(w, http.StatusBadRequest, "provider.key_too_large", "API key is too large", nil)
		return
	}
	if err := s.configureProviderCredential(r.Context(), key); err != nil {
		status := providerSetupHTTPStatus(err)
		if errors.Is(err, errProviderSetupAPIKeyRequired) {
			writeErr(w, status, errProviderSetupAPIKeyRequired)
			return
		}
		if status == http.StatusConflict {
			writeErr(w, status, errProviderSetupUnavailable)
			return
		}
		// Setup failures can contain filesystem or provider details. Keep those in
		// the remote process log rather than reflecting them into the browser.
		slog.Warn("serve: remote provider setup failed", "err", err)
		refuse(w, status, "provider.setup_failed", "unable to complete remote Provider setup", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errProviderSetupUnavailable = refusal(http.StatusConflict, "provider.setup_done", errors.New("provider setup is no longer required"), nil)
var errProviderSetupAPIKeyRequired = refusal(http.StatusBadRequest, "provider.key_required", errors.New("API key is required"), nil)

func (s *Server) configureProviderCredential(ctx context.Context, key string) error {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()

	setup, ok := s.providerSetupSnapshot()
	if !ok || !setup.Required || setup.KeyEnv == "" || setup.ModelRef == "" || (!setup.ActivationPending && setup.CredentialRevision == "") {
		return errProviderSetupUnavailable
	}
	if currentModelRef(s.ctl()) != setup.ModelRef {
		s.refreshProviderSetup(currentModelRef(s.ctl()))
		return errProviderSetupUnavailable
	}
	if setup.ActivationPending {
		// The first request already committed the secret. Retrying must rebuild the
		// controller without rewriting the credential or comparing against the now
		// stale pre-save revision. If another process removed the credential in the
		// meantime, return to the ordinary missing-key state instead.
		if !config.CredentialStored(setup.KeyEnv) {
			s.refreshProviderSetup(currentModelRef(s.ctl()))
			return errProviderSetupAPIKeyRequired
		}
	} else {
		if key == "" {
			return errProviderSetupAPIKeyRequired
		}
		if _, applied, err := config.SetCredentialIfRevision(setup.KeyEnv, key, setup.CredentialRevision); err != nil {
			return fmt.Errorf("save remote provider credential: %w", err)
		} else if !applied {
			s.refreshProviderSetup(currentModelRef(s.ctl()))
			return errProviderSetupUnavailable
		}
	}
	if err := s.switchModelLocked(ctx, setup.ModelRef); err != nil {
		s.providerSetupMu.Lock()
		s.providerSetup.ActivationPending = true
		s.providerSetup.Error = "The credential was saved, but the Provider could not be activated. Retry or restart Reasonix Serve."
		s.providerSetupMu.Unlock()
		return fmt.Errorf("activate remote provider: %w", err)
	}
	return nil
}

func providerSetupHTTPStatus(err error) int {
	if errors.Is(err, errProviderSetupAPIKeyRequired) {
		return http.StatusBadRequest
	}
	if errors.Is(err, errProviderSetupUnavailable) {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func ensureProviderSetupJSONEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("multiple JSON values")
}
