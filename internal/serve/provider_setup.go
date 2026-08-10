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

	"reasonix/internal/config"
)

//go:embed provider_setup.html
var providerSetupHTML []byte

const providerSetupMaxBody = 20 << 10

type providerSetupState struct {
	Enabled            bool   `json:"-"`
	Required           bool   `json:"required"`
	ActivationPending  bool   `json:"activationPending,omitempty"`
	Provider           string `json:"provider,omitempty"`
	Model              string `json:"model,omitempty"`
	ModelRef           string `json:"modelRef,omitempty"`
	KeyEnv             string `json:"keyEnv,omitempty"`
	CredentialRevision string `json:"-"`
	Error              string `json:"error,omitempty"`
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
	enabled := s.providerSetup.Enabled
	s.providerSetupMu.RUnlock()
	if !enabled {
		return
	}

	next := providerSetupState{Enabled: true}
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

func (s *Server) providerSetupIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'")
	lang := "auto"
	if cfg, err := config.Load(); err == nil {
		if dl := cfg.DesktopLanguage(); dl != "" {
			lang = dl
		}
	}
	html := strings.ReplaceAll(string(providerSetupHTML), "__LANG__", lang)
	_, _ = w.Write([]byte(html))
}

func (s *Server) providerSetupStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	setup, ok := s.providerSetupSnapshot()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, setup)
}

func (s *Server) providerSetupSave(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, providerSetupMaxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body struct {
		APIKey string `json:"apiKey"`
	}
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid provider setup request", http.StatusBadRequest)
		return
	}
	if err := ensureProviderSetupJSONEOF(dec); err != nil {
		http.Error(w, "invalid provider setup request", http.StatusBadRequest)
		return
	}
	key := strings.TrimSpace(body.APIKey)
	if len(key) > 16<<10 {
		http.Error(w, "API key is too large", http.StatusBadRequest)
		return
	}
	if err := s.configureProviderCredential(r.Context(), key); err != nil {
		status := providerSetupHTTPStatus(err)
		if errors.Is(err, errProviderSetupAPIKeyRequired) {
			http.Error(w, errProviderSetupAPIKeyRequired.Error(), status)
			return
		}
		if status == http.StatusConflict {
			http.Error(w, errProviderSetupUnavailable.Error(), status)
			return
		}
		// Setup failures can contain filesystem or provider details. Keep those in
		// the remote process log rather than reflecting them into the browser.
		slog.Warn("serve: remote provider setup failed", "err", err)
		http.Error(w, "unable to complete remote Provider setup", status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errProviderSetupUnavailable = errors.New("provider setup is no longer required")
var errProviderSetupAPIKeyRequired = errors.New("API key is required")

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
