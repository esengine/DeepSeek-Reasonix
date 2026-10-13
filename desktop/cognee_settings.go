package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/plugin"
)

const cogneeBearerTokenKey = "REASONIX_COGNEE_MCP_TOKEN"

type cogneeStatusProvider interface {
	CogneeMemoryStatus(context.Context) control.CogneeMemoryStatus
}

// CogneeMemorySettings returns readiness for the active workspace session.
func (a *App) CogneeMemorySettings() control.CogneeMemoryStatus {
	_, ctrl, _ := a.activeMCPRuntime()
	if provider, ok := ctrl.(cogneeStatusProvider); ok {
		ctx := context.Background()
		if a.ctx != nil {
			ctx = a.ctx
		}
		return provider.CogneeMemoryStatus(ctx)
	}
	return control.CogneeMemoryStatus{State: "connection_failed", Detail: "No active session is available."}
}

// ConfigureCogneeMemory validates the endpoint, stores an optional bearer
// token in Reasonix credentials, then installs or updates the fixed Cognee MCP
// identity through the existing MCP lifecycle.
func (a *App) ConfigureCogneeMemory(rawURL, token string) (control.CogneeMemoryStatus, error) {
	endpoint := strings.TrimSpace(rawURL)
	if strings.ContainsAny(token, "\r\n") {
		return a.CogneeMemorySettings(), fmt.Errorf("Cognee token must be a single line")
	}
	if err := validateCogneeSettingsURL(endpoint); err != nil {
		return a.CogneeMemorySettings(), err
	}
	tab, ctrl, root := a.activeMCPRuntime()
	if tab == nil || ctrl == nil {
		return control.CogneeMemoryStatus{}, fmt.Errorf("no active session")
	}
	original, found, err := desktopEffectiveMCPServer(root, "cognee")
	if err != nil {
		return a.CogneeMemorySettings(), err
	}
	if found {
		if err := ensureMCPServerDirectlyWritable(root, "cognee"); err != nil {
			return a.CogneeMemorySettings(), err
		}
	}
	token = strings.TrimSpace(token)
	credentialWritten := false
	var credentialWriteRevision string
	var previousCredential string
	previousCredentialStored := false
	var previousCredentialRevision string
	previousEnvironmentValue, previousEnvironmentSet := os.LookupEnv(cogneeBearerTokenKey)
	if token != "" {
		previousCredential, previousCredentialStored, previousCredentialRevision, err = config.CredentialSnapshot(cogneeBearerTokenKey)
		if err != nil {
			return a.CogneeMemorySettings(), fmt.Errorf("read Cognee credential state: %w", err)
		}
		_, credentialWriteRevision, credentialWritten, err = config.SetCredentialIfRevisionWithRevision(
			cogneeBearerTokenKey, token, previousCredentialRevision,
		)
		if err != nil {
			return a.CogneeMemorySettings(), fmt.Errorf("save Cognee token: %w", err)
		}
		if !credentialWritten {
			return a.CogneeMemorySettings(), fmt.Errorf("credential store changed during Cognee setup; retry the save")
		}
	}
	restoreCredential := func() error {
		if !credentialWritten {
			return nil
		}
		restored, err := config.RestoreCredentialIfRevision(
			cogneeBearerTokenKey, previousCredential, previousCredentialStored, credentialWriteRevision,
		)
		if err != nil {
			return fmt.Errorf("restore previous Cognee credential: %w", err)
		}
		if !restored {
			return fmt.Errorf("credential store changed during Cognee setup; the new token was not rolled back")
		}
		if previousEnvironmentSet {
			return os.Setenv(cogneeBearerTokenKey, previousEnvironmentValue)
		}
		return os.Unsetenv(cogneeBearerTokenKey)
	}
	restoreAndReconnect := func() error {
		if err := restoreCredential(); err != nil {
			return err
		}
		if credentialWritten && found {
			if err := a.ReconnectMCPServer("cognee"); err != nil {
				return fmt.Errorf("restore previous Cognee connection failed")
			}
		}
		return nil
	}
	headers := make(map[string]string, len(original.Headers)+1)
	for key, value := range original.Headers {
		headers[key] = value
	}
	if token != "" || config.CredentialStored(cogneeBearerTokenKey) {
		for key := range headers {
			if strings.EqualFold(key, "Authorization") {
				delete(headers, key)
			}
		}
		headers["Authorization"] = "Bearer ${" + cogneeBearerTokenKey + "}"
	}
	in := MCPServerInput{Name: "cognee", Transport: "http", URL: endpoint, Headers: headers}
	var installResult plugin.MCPInstallResult
	if found {
		err = a.UpdateMCPServer("cognee", in)
	} else {
		installResult, err = a.InstallMCPServer(in)
	}
	if err != nil {
		restoreErr := restoreAndReconnect()
		if restoreErr != nil {
			return a.CogneeMemorySettings(), fmt.Errorf("Cognee configuration failed and the previous credential or connection could not be fully restored")
		}
		return a.CogneeMemorySettings(), fmt.Errorf("Cognee MCP configuration could not be connected")
	}
	if !found && installResult.State == "issue" {
		if restoreErr := restoreAndReconnect(); restoreErr != nil {
			return a.CogneeMemorySettings(), fmt.Errorf("Cognee connection failed and the new credential could not be fully rolled back")
		}
		status := a.CogneeMemorySettings()
		status.State = "connection_failed"
		status.Detail = "Cognee could not connect. Check the endpoint, token, and network, then retry."
		return status, nil
	}
	status := a.CogneeMemorySettings()
	if !found && installResult.State == "action_required" && status.State == "not_configured" {
		status.State = "authorization_required"
		status.Configured = true
		status.Detail = "Authorize the Cognee MCP server to continue."
	}
	return status, nil
}

func validateCogneeSettingsURL(raw string) error {
	spec := plugin.Spec{Name: "cognee", Type: "http", URL: raw}
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("Cognee endpoint URL is required")
	}
	return control.ValidateCogneeEndpointForSettings(spec)
}
