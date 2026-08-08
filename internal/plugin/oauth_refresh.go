package plugin

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func (c *mcpOAuthClient) refresh(ctx context.Context) error {
	releaseGate, err := acquireMCPOAuthRefreshGate(ctx, c.stateDir)
	if err != nil {
		return fmt.Errorf("serialize MCP OAuth token refresh: %w", err)
	}
	defer releaseGate()

	// The file lock protects snapshots; network I/O happens after it is released.
	release, err := acquireMCPOAuthStateLock(ctx, c.stateDir)
	if err != nil {
		return fmt.Errorf("lock MCP OAuth token refresh: %w", err)
	}
	latest, err := loadMCPOAuthState(c.stateDir)
	if err != nil {
		release()
		return err
	}
	if strings.TrimSpace(latest.Resource) != "" && !sameCanonicalResource(latest.Resource, c.state.Resource) {
		release()
		return fmt.Errorf("MCP OAuth token refresh: stored token belongs to a different MCP resource")
	}
	c.state = latest
	if oauthAccessTokenUsable(latest, time.Now()) {
		release()
		return nil
	}
	if !c.canRefresh() {
		release()
		return fmt.Errorf("MCP OAuth access token expired and no refresh token is available; authorize again")
	}
	refreshState := latest
	generation, err := loadMCPOAuthGeneration(c.stateDir)
	if err != nil {
		release()
		return err
	}
	release()

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshState.RefreshToken},
		"client_id":     {refreshState.ClientID},
		"resource":      {refreshState.Resource},
	}
	if refreshState.Scope != "" {
		form.Set("scope", refreshState.Scope)
	}
	token, err := requestOAuthToken(ctx, c.client, refreshState, form)
	if err != nil {
		return fmt.Errorf("refresh MCP OAuth token: %w", err)
	}

	release, err = acquireMCPOAuthStateLock(ctx, c.stateDir)
	if err != nil {
		return fmt.Errorf("lock MCP OAuth token refresh result: %w", err)
	}
	defer release()
	currentGeneration, err := loadMCPOAuthGeneration(c.stateDir)
	if err != nil {
		return err
	}
	current, err := loadMCPOAuthState(c.stateDir)
	if err != nil {
		return err
	}
	if currentGeneration != generation || !sameOAuthRefreshState(current, refreshState) {
		if currentGeneration != generation {
			return fmt.Errorf("MCP OAuth token refresh was invalidated while contacting the token endpoint; authorize again")
		}
		if oauthAccessTokenUsable(current, time.Now()) {
			c.state = current
			return nil
		}
		return fmt.Errorf("MCP OAuth token state changed while refreshing; authorize again")
	}
	oldRefresh := refreshState.RefreshToken
	applyTokenResponse(&refreshState, token, time.Now())
	if refreshState.RefreshToken == "" {
		refreshState.RefreshToken = oldRefresh
	}
	if err := saveMCPOAuthState(c.stateDir, refreshState); err != nil {
		return err
	}
	c.state = refreshState
	return nil
}

func sameOAuthRefreshState(a, b mcpOAuthState) bool {
	return a.Version == b.Version &&
		a.Resource == b.Resource &&
		a.Issuer == b.Issuer &&
		a.AuthorizationEndpoint == b.AuthorizationEndpoint &&
		a.TokenEndpoint == b.TokenEndpoint &&
		a.RegistrationEndpoint == b.RegistrationEndpoint &&
		a.ClientID == b.ClientID &&
		a.ClientSecret == b.ClientSecret &&
		a.TokenEndpointAuthMethod == b.TokenEndpointAuthMethod &&
		a.Scope == b.Scope &&
		a.AccessToken == b.AccessToken &&
		a.RefreshToken == b.RefreshToken &&
		a.TokenType == b.TokenType &&
		a.Expiry.Equal(b.Expiry)
}

func oauthAccessTokenUsable(state mcpOAuthState, now time.Time) bool {
	return strings.TrimSpace(state.AccessToken) != "" && (state.Expiry.IsZero() || now.Add(30*time.Second).Before(state.Expiry))
}
