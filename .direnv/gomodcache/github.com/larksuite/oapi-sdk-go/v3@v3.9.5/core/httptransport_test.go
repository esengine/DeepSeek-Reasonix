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

package larkcore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSendPost(t *testing.T) {
	config := mockConfig()
	_, err := Request(context.Background(), &ApiReq{
		HttpMethod: http.MethodPost,
		ApiPath:    "/",
		Body: map[string]interface{}{
			"approval_code": "ou_c245b0a7dff2725cfa2fb104f8b48b9d",
		},
		SupportedAccessTokenTypes: []AccessTokenType{AccessTokenTypeUser},
	}, config, WithUserAccessToken("key"))

	if err != nil {
		t.Errorf("TestSendPost failed ,%v", err)
		return
	}
	fmt.Println("ok")

}

type closeTrackingBody struct {
	closed int32
}

func (b *closeTrackingBody) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (b *closeTrackingBody) Close() error {
	atomic.StoreInt32(&b.closed, 1)
	return nil
}

type httpClientStub struct {
	resp *http.Response
	err  error
}

func (c httpClientStub) Do(req *http.Request) (*http.Response, error) {
	return c.resp, c.err
}

type noopLogger struct{}

func (noopLogger) Debug(context.Context, ...interface{}) {}
func (noopLogger) Info(context.Context, ...interface{})  {}
func (noopLogger) Warn(context.Context, ...interface{})  {}
func (noopLogger) Error(context.Context, ...interface{}) {}

type recordingLogger struct {
	entries []string
}

func (l *recordingLogger) Debug(ctx context.Context, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprint(args...))
}

func (l *recordingLogger) Info(ctx context.Context, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprint(args...))
}

func (l *recordingLogger) Warn(ctx context.Context, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprint(args...))
}

func (l *recordingLogger) Error(ctx context.Context, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprint(args...))
}

func (l *recordingLogger) String() string {
	return strings.Join(l.entries, "\n")
}

func TestDoSend_CloseBodyOnGatewayTimeout(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	body := &closeTrackingBody{}
	client := httpClientStub{resp: &http.Response{
		StatusCode: http.StatusGatewayTimeout,
		Header:     make(http.Header),
		Body:       body,
		Request:    req,
	}}

	resp, err := doSend(context.Background(), req, client, noopLogger{})
	if resp != nil {
		t.Fatalf("expect nil resp, got: %#v", resp)
	}
	if err == nil {
		t.Fatalf("expect error, got nil")
	}
	if _, ok := err.(*ServerTimeoutError); !ok {
		t.Fatalf("expect *ServerTimeoutError, got: %T (%v)", err, err)
	}
	if atomic.LoadInt32(&body.closed) != 1 {
		t.Fatalf("expect resp body closed on gateway timeout")
	}
}

func TestDetermineTokenTypeRejectsAppOnlyInClientAssertionMode(t *testing.T) {
	config := mockConfig()
	config.ClientAssertionProvider = &mockClientAssertionProvider{token: &Token{Value: "assertion"}}

	_, err := determineTokenType([]AccessTokenType{AccessTokenTypeApp}, &RequestOption{}, config)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	codeErr, ok := err.(*CodeError)
	if !ok || codeErr.Code != ErrCodeClientAssertionModeNotSupported {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestValidateSkipsAppSecretWithClientAssertionProvider(t *testing.T) {
	config := mockConfig()
	config.AppSecret = ""
	config.EnableTokenCache = true
	config.ClientAssertionProvider = &mockClientAssertionProvider{token: &Token{Value: "assertion"}}

	err := validate(config, &RequestOption{}, AccessTokenTypeTenant)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
}

func TestDetermineTokenTypePrefersTenantInClientAssertionMode(t *testing.T) {
	config := mockConfig()
	config.ClientAssertionProvider = &mockClientAssertionProvider{token: &Token{Value: "assertion"}}

	tokenType, err := determineTokenType([]AccessTokenType{AccessTokenTypeApp, AccessTokenTypeTenant}, &RequestOption{}, config)
	if err != nil {
		t.Fatalf("determine token type failed: %v", err)
	}
	if tokenType != AccessTokenTypeTenant {
		t.Fatalf("unexpected token type: %s", tokenType)
	}
}

func TestRequestDoesNotRetryWhenRetrieveTokenFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == OAuthTokenUrlPath {
			_, _ = w.Write([]byte(`{"access_token":"tenant-token","expires_in":7200}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	provider := &retryProvider{token: &Token{Value: "assertion"}}
	config := mockConfig()
	config.BaseUrl = "https://open.feishu.cn"
	config.OAuthBaseUrl = server.URL
	config.EnableTokenCache = true
	config.HttpClient = server.Client()
	config.ClientAssertionProvider = provider

	resp, err := Request(context.Background(), &ApiReq{
		HttpMethod:                http.MethodGet,
		ApiPath:                   "/resource",
		SupportedAccessTokenTypes: []AccessTokenType{AccessTokenTypeTenant},
	}, config)
	if err == nil {
		t.Fatal("expected retrieve token error, got nil")
	}
	if resp != nil || provider.calls != 1 {
		t.Fatalf("unexpected response/provider calls: resp=%v calls=%d", resp, provider.calls)
	}
}

func TestRequestRedactsOAuthTokenDebugLogs(t *testing.T) {
	logger := &recordingLogger{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"oauth-access-token-secret","refresh_token":"oauth-refresh-token-secret","expires_in":7200}`))
	}))
	defer server.Close()

	config := mockConfig()
	config.AppId = "cli_a"
	config.BaseUrl = "https://open.feishu.cn"
	config.LogReqAtDebug = true
	config.Logger = logger
	config.HttpClient = server.Client()

	_, err := Request(context.Background(), &ApiReq{
		HttpMethod: http.MethodPost,
		ApiPath:    server.URL + OAuthTokenUrlPath,
		Body: map[string]string{
			"client_assertion": "client-assertion-secret",
			"client_secret":    "app-secret-value",
			"refresh_token":    "refresh-token-secret",
		},
		SupportedAccessTokenTypes: []AccessTokenType{AccessTokenTypeNone},
	}, config)
	if err != nil {
		t.Fatalf("oauth token request failed: %v", err)
	}

	logs := logger.String()
	for _, secret := range []string{
		"client-assertion-secret",
		"app-secret-value",
		"refresh-token-secret",
		"oauth-access-token-secret",
		"oauth-refresh-token-secret",
	} {
		if strings.Contains(logs, secret) {
			t.Fatalf("debug logs leaked %q: %s", secret, logs)
		}
	}
	if !strings.Contains(logs, "oauth token request body omitted") {
		t.Fatalf("expected oauth request body omission marker, got logs: %s", logs)
	}
	if !strings.Contains(logs, "oauth token response body omitted") {
		t.Fatalf("expected oauth response body omission marker, got logs: %s", logs)
	}
}

func TestValidateAllowsManualUserAccessTokenWithoutAppSecret(t *testing.T) {
	config := mockConfig()
	config.AppSecret = ""
	config.ClientAssertionProvider = nil

	err := validate(config, &RequestOption{UserAccessToken: "user-token"}, AccessTokenTypeUser)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
}

func TestValidateAllowsManualTenantAccessTokenWithoutAppSecret(t *testing.T) {
	config := mockConfig()
	config.AppSecret = ""
	config.ClientAssertionProvider = nil

	err := validate(config, &RequestOption{TenantAccessToken: "tenant-token"}, AccessTokenTypeTenant)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
}

func TestValidateRejectsMarketplaceClientAssertionMode(t *testing.T) {
	config := mockConfig()
	config.AppType = AppTypeMarketplace
	config.ClientAssertionProvider = &mockClientAssertionProvider{token: &Token{Value: "assertion"}}

	err := validate(config, &RequestOption{}, AccessTokenTypeTenant)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	codeErr, ok := err.(*CodeError)
	if !ok || codeErr.Code != ErrCodeClientAssertionProviderNotConfigured {
		t.Fatalf("unexpected error: %#v", err)
	}
}

type retryProvider struct {
	calls int
	token *Token
}

func (p *retryProvider) RetrieveToken(ctx context.Context, aud string) (*Token, error) {
	p.calls++
	if p.calls == 1 {
		return nil, ErrAppTicketIsEmpty
	}
	return p.token, nil
}
