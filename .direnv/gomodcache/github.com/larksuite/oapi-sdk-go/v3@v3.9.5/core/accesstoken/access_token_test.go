/*
 * MIT License
 *
 * Copyright (c) 2022 Lark Technologies Pte. Ltd.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice, shall be included in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package accesstoken

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/core/accesstoken/authorizationcode"
	"github.com/larksuite/oapi-sdk-go/v3/core/accesstoken/refreshtoken"
)

type mockClientAssertionProvider struct {
	token *larkcore.Token
	err   error
	auds  []string
}

func (p *mockClientAssertionProvider) RetrieveToken(ctx context.Context, aud string) (*larkcore.Token, error) {
	p.auds = append(p.auds, aud)
	return p.token, p.err
}

func mustURLHost(t *testing.T, rawURL string) string {
	t.Helper()
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url failed: %v", err)
	}
	return parsedURL.Host
}

func newTestConfig(server *httptest.Server, provider larkcore.ClientAssertionProvider) *larkcore.Config {
	return &larkcore.Config{
		BaseUrl:                 "https://open.feishu.cn",
		OAuthBaseUrl:            server.URL,
		AppId:                   "cli_a",
		AppSecret:               "",
		ClientAssertionProvider: provider,
		EnableTokenCache:        true,
		HttpClient:              server.Client(),
		Logger:                  larkcore.NewDefaultLogger(larkcore.LogLevelInfo),
		Serializable:            &larkcore.DefaultSerialization{},
	}
}

func newAppSecretTestConfig(server *httptest.Server) *larkcore.Config {
	config := newTestConfig(server, nil)
	config.AppSecret = "app-secret"
	return config
}

func TestAccessTokenAuthorizationCode(t *testing.T) {
	provider := &mockClientAssertionProvider{token: &larkcore.Token{Value: "client-assertion"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != larkcore.OAuthTokenUrlPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req accessTokenRequestBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		if req.GrantType != larkcore.GrantTypeAuthorizationCode {
			t.Fatalf("unexpected grant type: %s", req.GrantType)
		}
		if req.ClientAssertion != "client-assertion" || req.ClientID != "cli_a" {
			t.Fatalf("unexpected client assertion request: %#v", req)
		}
		if req.Code != "code" || req.RedirectUri != "https://example.com/cb" || req.CodeVerifier != "verifier" {
			t.Fatalf("unexpected request body: %#v", req)
		}
		_ = json.NewEncoder(w).Encode(&accessTokenResponseBody{
			AccessToken:  "user-token",
			TokenType:    "Bearer",
			ExpiresIn:    7200,
			RefreshToken: "refresh-token",
			Scope:        "contact:user.base:readonly",
		})
	}))
	defer server.Close()

	accessToken := NewAccessToken(newTestConfig(server, provider))
	resp, err := accessToken.RetrieveByAuthorizationCode(context.Background(), authorizationcode.NewTokenRequestBuilder().
		Code("code").
		RedirectUri("https://example.com/cb").
		CodeVerifier("verifier").
		Build())
	if err != nil {
		t.Fatalf("authorization code access token failed: %v", err)
	}
	if !resp.Success() || larkcore.StringValue(resp.Data.AccessToken) != "user-token" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if len(provider.auds) != 1 || provider.auds[0] != mustURLHost(t, server.URL) {
		t.Fatalf("unexpected auds: %#v", provider.auds)
	}
}

func TestAccessTokenRefreshToken(t *testing.T) {
	provider := &mockClientAssertionProvider{token: &larkcore.Token{Value: "client-assertion"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req accessTokenRequestBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		if req.GrantType != larkcore.GrantTypeRefreshToken {
			t.Fatalf("unexpected grant type: %s", req.GrantType)
		}
		if req.RefreshToken != "refresh-token" {
			t.Fatalf("unexpected refresh token: %s", req.RefreshToken)
		}
		_ = json.NewEncoder(w).Encode(&accessTokenResponseBody{AccessToken: "user-token", ExpiresIn: 7200})
	}))
	defer server.Close()

	accessToken := NewAccessToken(newTestConfig(server, provider))
	resp, err := accessToken.Refresh(context.Background(), refreshtoken.NewTokenRequestBuilder().RefreshToken("refresh-token").Build())
	if err != nil {
		t.Fatalf("refresh token access token failed: %v", err)
	}
	if larkcore.StringValue(resp.Data.AccessToken) != "user-token" {
		t.Fatalf("unexpected access token: %#v", resp.Data.AccessToken)
	}
	if len(provider.auds) != 1 || provider.auds[0] != mustURLHost(t, server.URL) {
		t.Fatalf("unexpected auds: %#v", provider.auds)
	}
}

func TestAccessTokenAuthorizationCodeWithAppSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != larkcore.OAuthTokenUrlPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req accessTokenRequestBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		if req.ClientID != "cli_a" || req.ClientSecret != "app-secret" {
			t.Fatalf("unexpected app secret request: %#v", req)
		}
		if req.ClientAssertion != "" || req.ClientAssertionType != "" {
			t.Fatalf("unexpected client assertion fields: %#v", req)
		}
		if req.GrantType != larkcore.GrantTypeAuthorizationCode || req.Code != "code" {
			t.Fatalf("unexpected request body: %#v", req)
		}
		_ = json.NewEncoder(w).Encode(&accessTokenResponseBody{AccessToken: "user-token", ExpiresIn: 7200})
	}))
	defer server.Close()

	accessToken := NewAccessToken(newAppSecretTestConfig(server))
	resp, err := accessToken.RetrieveByAuthorizationCode(context.Background(), authorizationcode.NewTokenRequestBuilder().Code("code").Build())
	if err != nil {
		t.Fatalf("authorization code access token with app secret failed: %v", err)
	}
	if larkcore.StringValue(resp.Data.AccessToken) != "user-token" {
		t.Fatalf("unexpected access token: %#v", resp.Data.AccessToken)
	}
}

func TestAccessTokenRefreshTokenWithAppSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req accessTokenRequestBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		if req.ClientID != "cli_a" || req.ClientSecret != "app-secret" {
			t.Fatalf("unexpected app secret request: %#v", req)
		}
		if req.GrantType != larkcore.GrantTypeRefreshToken || req.RefreshToken != "refresh-token" {
			t.Fatalf("unexpected request body: %#v", req)
		}
		_ = json.NewEncoder(w).Encode(&accessTokenResponseBody{AccessToken: "user-token", ExpiresIn: 7200})
	}))
	defer server.Close()

	accessToken := NewAccessToken(newAppSecretTestConfig(server))
	resp, err := accessToken.Refresh(context.Background(), refreshtoken.NewTokenRequestBuilder().RefreshToken("refresh-token").Build())
	if err != nil {
		t.Fatalf("refresh token access token with app secret failed: %v", err)
	}
	if larkcore.StringValue(resp.Data.AccessToken) != "user-token" {
		t.Fatalf("unexpected access token: %#v", resp.Data.AccessToken)
	}
}

func TestAccessTokenRejectsMissingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	}))
	defer server.Close()

	config := newTestConfig(server, nil)
	accessToken := NewAccessToken(config)
	_, err := accessToken.RetrieveByAuthorizationCode(context.Background(), authorizationcode.NewTokenRequestBuilder().Code("code").Build())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	codeErr, ok := err.(*larkcore.CodeError)
	if !ok || codeErr.Code != larkcore.ErrCodeAppSecretAndClientAssertionEmpty {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestAccessTokenReturnsAccessTokenErrorForNonOK(t *testing.T) {
	provider := &mockClientAssertionProvider{token: &larkcore.Token{Value: "client-assertion"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(&accessTokenResponseBody{
			Code:             20001,
			Error:            "invalid_client",
			ErrorDescription: "client assertion invalid",
		})
	}))
	defer server.Close()

	accessToken := NewAccessToken(newTestConfig(server, provider))
	_, err := accessToken.RetrieveByAuthorizationCode(context.Background(), authorizationcode.NewTokenRequestBuilder().Code("code").Build())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	accessTokenErr, ok := err.(*AccessTokenError)
	if !ok {
		t.Fatalf("unexpected error type: %#v", err)
	}
	if accessTokenErr.Code != 20001 || accessTokenErr.ErrorDescription != "client assertion invalid" {
		t.Fatalf("unexpected access token error: %#v", accessTokenErr)
	}
}

func TestAccessTokenReturnsAccessTokenErrorForBusinessErrorOK(t *testing.T) {
	provider := &mockClientAssertionProvider{token: &larkcore.Token{Value: "client-assertion"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":              20138,
			"error":             "invalid_client",
			"error_description": "client assertion expired",
		})
	}))
	defer server.Close()

	accessToken := NewAccessToken(newTestConfig(server, provider))
	_, err := accessToken.RetrieveByAuthorizationCode(context.Background(), authorizationcode.NewTokenRequestBuilder().Code("code").Build())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	accessTokenErr, ok := err.(*AccessTokenError)
	if !ok {
		t.Fatalf("unexpected error type: %#v", err)
	}
	if accessTokenErr.Code != 20138 || accessTokenErr.ErrorDescription != "client assertion expired" {
		t.Fatalf("unexpected access token error: %#v", accessTokenErr)
	}
}

func TestAccessTokenReturnsAccessTokenErrorForEmptyAccessToken(t *testing.T) {
	provider := &mockClientAssertionProvider{token: &larkcore.Token{Value: "client-assertion"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&accessTokenResponseBody{Code: 0})
	}))
	defer server.Close()

	accessToken := NewAccessToken(newTestConfig(server, provider))
	_, err := accessToken.RetrieveByAuthorizationCode(context.Background(), authorizationcode.NewTokenRequestBuilder().Code("code").Build())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	accessTokenErr, ok := err.(*AccessTokenError)
	if !ok {
		t.Fatalf("unexpected error type: %#v", err)
	}
	if accessTokenErr.ErrorDescription != "access_token is empty" {
		t.Fatalf("unexpected access token error: %#v", accessTokenErr)
	}
}

func TestAccessTokenProxyKeepsCustomHeaders(t *testing.T) {
	provider := &mockClientAssertionProvider{token: &larkcore.Token{Value: "client-assertion"}}
	proxyServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy"+larkcore.OAuthTokenUrlPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get(larkcore.HeaderXTargetService) != r.Host {
			t.Fatalf("unexpected target service header: %s", r.Header.Get(larkcore.HeaderXTargetService))
		}
		if r.Header.Get("X-Custom") != "custom-value" {
			t.Fatalf("missing custom header")
		}
		_ = json.NewEncoder(w).Encode(&accessTokenResponseBody{AccessToken: "user-token", ExpiresIn: 7200})
	}))
	defer proxyServer.Close()
	provider.token.TargetInfo = &larkcore.TargetInfo{TargetService: proxyServer.Listener.Addr().String(), TargetPrefix: "/proxy"}

	config := newTestConfig(proxyServer, provider)
	accessToken := NewAccessToken(config)
	headers := make(http.Header)
	headers.Set("X-Custom", "custom-value")
	_, err := accessToken.RetrieveByAuthorizationCode(context.Background(), authorizationcode.NewTokenRequestBuilder().Code("code").Build(), larkcore.WithHeaders(headers))
	if err != nil {
		t.Fatalf("authorization code access token with proxy failed: %v", err)
	}
}
