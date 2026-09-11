package plugin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

type mcpOAuthSDKRuntime struct {
	mu              sync.Mutex
	fatalErr        error
	fatalErrReturns int
}

func (c *mcpOAuthClient) oauthToken(ctx context.Context, forceRefresh bool) (*oauth2.Token, error) {
	return c.oauthTokenAfterRejection(ctx, forceRefresh, "")
}

func (c *mcpOAuthClient) oauthTokenAfterRejection(ctx context.Context, forceRefresh bool, rejectedAccessToken string) (*oauth2.Token, error) {
	if c == nil {
		return nil, nil
	}
	c.runtime.mu.Lock()
	defer c.runtime.mu.Unlock()
	if c.runtime.fatalErr != nil {
		err := c.runtime.fatalErr
		c.runtime.fatalErrReturns--
		if c.runtime.fatalErrReturns <= 0 {
			c.runtime.fatalErr = nil
		}
		return nil, err
	}
	if forceRefresh && rejectedAccessToken == "" {
		rejectedAccessToken = c.state.AccessToken
	}
	if forceRefresh && rejectedAccessToken != "" && c.state.AccessToken != rejectedAccessToken && oauthAccessTokenUsable(c.state, time.Now()) {
		forceRefresh = false
	}
	needsRefresh := forceRefresh || (strings.TrimSpace(c.state.RefreshToken) != "" && !c.state.Expiry.IsZero() && time.Now().Add(30*time.Second).After(c.state.Expiry))
	if needsRefresh {
		if err := c.refresh(ctx, forceRefresh, rejectedAccessToken); err != nil {
			c.runtime.fatalErr = err
			c.runtime.fatalErrReturns = 1
			return nil, err
		}
	}
	if strings.TrimSpace(c.state.AccessToken) == "" {
		return nil, nil
	}
	tokenType := strings.TrimSpace(c.state.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	if !strings.EqualFold(tokenType, "Bearer") {
		return nil, fmt.Errorf("MCP OAuth: unsupported token type %q", tokenType)
	}
	return &oauth2.Token{
		AccessToken:  c.state.AccessToken,
		TokenType:    tokenType,
		RefreshToken: c.state.RefreshToken,
		Expiry:       c.state.Expiry,
	}, nil
}

func (c *mcpOAuthClient) canRefresh() bool {
	return c != nil && strings.TrimSpace(c.state.RefreshToken) != "" && strings.TrimSpace(c.state.TokenEndpoint) != ""
}

type mcpOAuthTokenSource struct {
	ctx    context.Context
	client *mcpOAuthClient
}

func (s *mcpOAuthTokenSource) Token() (*oauth2.Token, error) {
	return s.client.oauthToken(s.ctx, false)
}

// TokenSource implements auth.OAuthHandler for the official MCP Go SDK.
func (c *mcpOAuthClient) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if c == nil {
		return nil, nil
	}
	return &mcpOAuthTokenSource{ctx: ctx, client: c}, nil
}

// Authorize handles the SDK's single retry after a 401/403 without starting an
// interactive browser flow from a background tool call.
func (c *mcpOAuthClient) Authorize(ctx context.Context, request *http.Request, response *http.Response) error {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if c == nil {
		return fmt.Errorf("MCP OAuth authorization is required")
	}
	c.runtime.mu.Lock()
	canRefresh := c.canRefresh()
	c.runtime.mu.Unlock()
	if !canRefresh {
		return fmt.Errorf("MCP OAuth authorization is required; authorize this MCP server")
	}
	rejectedAccessToken := ""
	if request != nil {
		scheme, token, ok := strings.Cut(strings.TrimSpace(request.Header.Get("Authorization")), " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			rejectedAccessToken = strings.TrimSpace(token)
		}
	}
	_, err := c.oauthTokenAfterRejection(ctx, true, rejectedAccessToken)
	return err
}
