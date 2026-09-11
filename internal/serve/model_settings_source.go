package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

var modelSettingsSourceClient = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 20 * time.Second}

func requestModelSettingsSource(ctx context.Context, settings *config.ModelRuntimeSettings, request config.ModelSettingsSourceRequest) (config.ModelSettingsSourceResponse, error) {
	var response config.ModelSettingsSourceResponse
	endpoint, err := url.Parse(settings.ProxyURL)
	if err != nil || endpoint.Scheme != "http" || (endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "::1") || endpoint.User != nil {
		return response, fmt.Errorf("model settings source requires the local credential tunnel")
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = "/model-settings-source", "", ""
	body, err := json.Marshal(request)
	if err != nil {
		return response, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return response, err
	}
	req.Header.Set("Authorization", "Bearer "+settings.SourceToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := modelSettingsSourceClient.Do(req)
	if err != nil {
		return response, fmt.Errorf("Desktop model settings source is unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return response, fmt.Errorf("Desktop cannot apply saved model settings (status %d); check its available models and connection", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&response); err != nil || response.Version != 1 || response.Revision == "" {
		return response, fmt.Errorf("invalid Desktop model settings acknowledgement")
	}
	return response, nil
}

func sourceModelRef(settings *config.ModelRuntimeSettings, remoteRef string) string {
	for source, target := range settings.References {
		if target == remoteRef && strings.Contains(source, "/") {
			return source
		}
	}
	return remoteRef
}

// New HTTP turns and autonomous FIFO dispatch share this boundary. A source
// failure leaves the existing controller and queued work intact. Approvals,
// steers, and children remain on their already accepted runtime.
func (s *Server) refreshRunModelSettingsLocked(ctx context.Context) error {
	return s.refreshModelSettingsOwnerLocked(ctx, modelSettingsRuntimeOwner{
		current: s.ctl, settings: &s.managedModels, offerID: &s.modelSettingsOfferID, apply: s.switchModelLocked,
	})
}

type modelSettingsRuntimeOwner struct {
	current  func() control.SessionAPI
	settings **config.ModelRuntimeSettings
	offerID  *string
	apply    func(context.Context, string) error
}

func (s *Server) refreshModelSettingsOwnerLocked(ctx context.Context, owner modelSettingsRuntimeOwner) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		current := owner.current()
		settings := (*owner.settings)
		var offered *config.ModelRuntimeSettings
		ref := current.ModelRef()
		var sourceRequest config.ModelSettingsSourceRequest
		if settings != nil && settings.SourceToken != "" {
			offerID, err := config.NewModelSettingsOfferID()
			if err != nil {
				return err
			}
			endpoint, err := url.Parse(settings.ProxyURL)
			if err != nil {
				return fmt.Errorf("invalid model settings tunnel")
			}
			port, _ := strconv.Atoi(endpoint.Port())
			status := s.modelSettingsStatusLocked()
			sourceRequest = config.ModelSettingsSourceRequest{Mode: "prepare", OfferID: offerID, PreviousOfferID: (*owner.offerID), Model: sourceModelRef(settings, ref), AppliedRevision: settings.Revision, RemotePort: port, OwnedRevisions: status.OwnedRevisions, UnversionedOwners: status.UnversionedOwners}
			(*owner.offerID) = offerID
			sourceRequest.ModelSettingsOwnership = status.ModelSettingsOwnership
			response, err := requestModelSettingsSource(ctx, settings, sourceRequest)
			if err != nil {
				return err
			}
			if response.Settings != nil {
				if err := validateModelSettingsSourceOffer(offerID, response); err != nil {
					return err
				}
				offered, ref = response.Settings, response.Ref
			} else if response.Revision != settings.Revision {
				return fmt.Errorf("changed model settings require a complete resolver")
			}
		}
		needsApply := offered != nil && (offered.Revision != settings.Revision || ref != current.ModelRef())
		if snapshot, ok := current.(interface {
			ModelSettingsState() (string, string, error)
		}); ok {
			applied, desired, err := snapshot.ModelSettingsState()
			if err != nil {
				return err
			}
			needsApply = needsApply || applied != desired
		}
		var applyErr error
		if needsApply {
			if offered != nil {
				(*owner.settings) = offered
			}
			applyErr = owner.apply(ctx, ref)
			if applyErr != nil {
				(*owner.settings) = settings
			}
		}
		if settings != nil && settings.SourceToken != "" {
			ackSettings := settings
			if offered != nil {
				ackSettings = offered
			}
			status := s.modelSettingsStatusLocked()
			sourceRequest.Mode = "finish"
			sourceRequest.ModelSettingsOwnership = status.ModelSettingsOwnership
			sourceRequest.PreviousOfferID = ""
			sourceRequest.OwnedRevisions, sourceRequest.UnversionedOwners = status.OwnedRevisions, status.UnversionedOwners
			ack, ackErr := requestModelSettingsSource(ctx, ackSettings, sourceRequest)
			if ackErr == nil {
				(*owner.offerID) = ""
			}
			if applyErr != nil {
				return fmt.Errorf("saved model settings could not be applied: %w", applyErr)
			}
			if ackErr != nil {
				return ackErr
			}
			if ack.Revision != (*owner.settings).Revision {
				continue // a save overtook the candidate; no new run was admitted
			}
		}
		if applyErr != nil {
			return applyErr
		}
		if changed, err := runtimeModelSettingsChanged(owner.current()); err != nil {
			return err
		} else if changed {
			continue
		}
		return nil
	}
}

func validateModelSettingsSourceOffer(id string, response config.ModelSettingsSourceResponse) error {
	if response.Settings.OfferID != id || response.Settings.Revision != response.Revision || response.Ref == "" || response.Settings.SourceToken == "" {
		return fmt.Errorf("model settings offer does not match its request")
	}
	return nil
}

func runtimeModelSettingsChanged(ctrl control.SessionAPI) (bool, error) {
	snapshot, ok := ctrl.(interface {
		ModelSettingsState() (string, string, error)
	})
	if !ok {
		return false, nil
	}
	applied, desired, err := snapshot.ModelSettingsState()
	return applied != desired, err
}

func (s *Server) beforeInboxDispatch(ctrl *control.Controller) (func(), error) {
	if s.ctl() != ctrl {
		return s.beforeDetachedInboxDispatch(ctrl)
	}
	s.bindMu.Lock()
	if s.ctl() != ctrl {
		s.bindMu.Unlock()
		return nil, control.ErrInboxRuntimeUnpublished
	}
	if ctrl.Running() {
		s.bindMu.Unlock()
		return nil, control.ErrTurnRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := s.refreshRunModelSettingsLocked(ctx)
	cancel()
	if err != nil {
		s.bindMu.Unlock()
		return nil, err
	}
	if current := s.ctl(); current != ctrl {
		s.bindMu.Unlock()
		if replacement, ok := current.(*control.Controller); ok {
			replacement.NotifyInboxRuntimeReady()
		}
		return nil, control.ErrInboxRuntimeUnpublished
	}
	return s.bindMu.Unlock, nil
}
