package channel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestGetBotIdentityUsesCacheWithinTTL(t *testing.T) {
	var botInfoCalls int
	mockHTTP := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			switch req.URL.Path {
			case "/open-apis/auth/v3/tenant_access_token/internal":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","tenant_access_token":"t-token","expire":7200}`)),
					Header:     header,
				}, nil
			case "/open-apis/bot/v3/info":
				botInfoCalls++
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","bot":{"open_id":"ou_bot_1","app_name":"Bot One","activate_status":2}}`)),
					Header:     header,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success"}`)),
					Header:     header,
				}, nil
			}
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHTTP))
	impl := NewChannel(client, nil).(*channelImpl)

	first := impl.GetBotIdentity(context.Background())
	second := impl.GetBotIdentity(context.Background())

	if first == nil || second == nil {
		t.Fatalf("expected bot identity to be cached")
	}
	if botInfoCalls != 1 {
		t.Fatalf("expected bot info to be fetched once, got %d", botInfoCalls)
	}
	if second.Name != "Bot One" {
		t.Fatalf("expected cached bot name Bot One, got %s", second.Name)
	}
	if second.ActivateStatus != 2 {
		t.Fatalf("expected cached activate status 2, got %d", second.ActivateStatus)
	}
}

func TestGetBotIdentityRefreshesAfterTTL(t *testing.T) {
	var botInfoCalls int
	mockHTTP := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			switch req.URL.Path {
			case "/open-apis/auth/v3/tenant_access_token/internal":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","tenant_access_token":"t-token","expire":7200}`)),
					Header:     header,
				}, nil
			case "/open-apis/bot/v3/info":
				botInfoCalls++
				name := "Bot One"
				if botInfoCalls > 1 {
					name = "Bot Two"
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","bot":{"open_id":"ou_bot_1","app_name":"` + name + `"}}`)),
					Header:     header,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success"}`)),
					Header:     header,
				}, nil
			}
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHTTP))
	impl := NewChannel(client, nil, types.WithBotIdentityCacheConfig(types.BotIdentityCacheConfig{
		TTL: 30 * time.Minute,
	})).(*channelImpl)

	first := impl.GetBotIdentity(context.Background())
	if first == nil {
		t.Fatal("expected initial bot identity")
	}

	impl.botIdentityFetchedAt = time.Now().Add(-31 * time.Minute)

	second := impl.GetBotIdentity(context.Background())
	if second == nil {
		t.Fatal("expected refreshed bot identity")
	}
	if botInfoCalls != 2 {
		t.Fatalf("expected bot info to be fetched twice, got %d", botInfoCalls)
	}
	if second.Name != "Bot Two" {
		t.Fatalf("expected refreshed bot name Bot Two, got %s", second.Name)
	}
}

func TestGetBotIdentityReturnsStaleCacheWhenRefreshFails(t *testing.T) {
	var botInfoCalls int
	mockHTTP := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			switch req.URL.Path {
			case "/open-apis/auth/v3/tenant_access_token/internal":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","tenant_access_token":"t-token","expire":7200}`)),
					Header:     header,
				}, nil
			case "/open-apis/bot/v3/info":
				botInfoCalls++
				if botInfoCalls == 1 {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","bot":{"open_id":"ou_bot_1","app_name":"Bot One"}}`)),
						Header:     header,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader(`{"code":500,"msg":"error"}`)),
					Header:     header,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success"}`)),
					Header:     header,
				}, nil
			}
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHTTP))
	impl := NewChannel(client, nil, types.WithBotIdentityCacheConfig(types.BotIdentityCacheConfig{
		TTL: 30 * time.Minute,
	})).(*channelImpl)

	first := impl.GetBotIdentity(context.Background())
	if first == nil {
		t.Fatal("expected initial bot identity")
	}

	impl.botIdentityFetchedAt = time.Now().Add(-31 * time.Minute)

	second := impl.GetBotIdentity(context.Background())
	if second == nil {
		t.Fatal("expected stale bot identity on refresh failure")
	}
	if second.Name != "Bot One" {
		t.Fatalf("expected stale bot name Bot One, got %s", second.Name)
	}
	if botInfoCalls != 2 {
		t.Fatalf("expected refresh attempt after TTL expiry, got %d bot info calls", botInfoCalls)
	}
}

func TestNewChannelNormalizesBotIdentityCacheConfig(t *testing.T) {
	client := lark.NewClient("appId", "appSecret")
	impl := NewChannel(client, nil, types.WithBotIdentityCacheConfig(types.BotIdentityCacheConfig{
		TTL:                0,
		MinRefreshInterval: 5 * time.Second,
	})).(*channelImpl)

	if impl.config.BotIdentityCache.TTL != 30*time.Minute {
		t.Fatalf("expected default TTL 30m, got %v", impl.config.BotIdentityCache.TTL)
	}
	if impl.config.BotIdentityCache.MinRefreshInterval != 30*time.Second {
		t.Fatalf("expected min refresh interval to be normalized to 30s, got %v", impl.config.BotIdentityCache.MinRefreshInterval)
	}
}

func TestGetBotIdentityDoesNotRetryTooSoonAfterFailure(t *testing.T) {
	var botInfoCalls int
	mockHTTP := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			switch req.URL.Path {
			case "/open-apis/auth/v3/tenant_access_token/internal":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","tenant_access_token":"t-token","expire":7200}`)),
					Header:     header,
				}, nil
			case "/open-apis/bot/v3/info":
				botInfoCalls++
				if botInfoCalls == 1 {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","bot":{"open_id":"ou_bot_1","app_name":"Bot One"}}`)),
						Header:     header,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader(`{"code":500,"msg":"error"}`)),
					Header:     header,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success"}`)),
					Header:     header,
				}, nil
			}
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHTTP))
	impl := NewChannel(client, nil, types.WithBotIdentityCacheConfig(types.BotIdentityCacheConfig{
		TTL:                30 * time.Minute,
		MinRefreshInterval: 1 * time.Minute,
	})).(*channelImpl)

	first := impl.GetBotIdentity(context.Background())
	if first == nil {
		t.Fatal("expected initial bot identity")
	}
	impl.botIdentityFetchedAt = time.Now().Add(-31 * time.Minute)

	second := impl.GetBotIdentity(context.Background())
	if second == nil || second.Name != "Bot One" {
		t.Fatalf("expected stale bot identity after failed refresh, got %#v", second)
	}
	if botInfoCalls != 2 {
		t.Fatalf("expected two bot info calls after one failed refresh, got %d", botInfoCalls)
	}

	third := impl.GetBotIdentity(context.Background())
	if third == nil || third.Name != "Bot One" {
		t.Fatalf("expected stale bot identity without immediate retry, got %#v", third)
	}
	if botInfoCalls != 2 {
		t.Fatalf("expected no extra retry within min refresh interval, got %d calls", botInfoCalls)
	}
}

func TestGetBotIdentityRetriesAgainAfterMinRefreshInterval(t *testing.T) {
	var botInfoCalls int
	mockHTTP := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			switch req.URL.Path {
			case "/open-apis/auth/v3/tenant_access_token/internal":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","tenant_access_token":"t-token","expire":7200}`)),
					Header:     header,
				}, nil
			case "/open-apis/bot/v3/info":
				botInfoCalls++
				if botInfoCalls == 1 {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","bot":{"open_id":"ou_bot_1","app_name":"Bot One"}}`)),
						Header:     header,
					}, nil
				}
				if botInfoCalls == 2 {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       io.NopCloser(strings.NewReader(`{"code":500,"msg":"error"}`)),
						Header:     header,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","bot":{"open_id":"ou_bot_1","app_name":"Bot Two"}}`)),
					Header:     header,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success"}`)),
					Header:     header,
				}, nil
			}
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHTTP))
	impl := NewChannel(client, nil, types.WithBotIdentityCacheConfig(types.BotIdentityCacheConfig{
		TTL:                30 * time.Minute,
		MinRefreshInterval: 1 * time.Minute,
	})).(*channelImpl)

	first := impl.GetBotIdentity(context.Background())
	if first == nil {
		t.Fatal("expected initial bot identity")
	}
	impl.botIdentityFetchedAt = time.Now().Add(-31 * time.Minute)

	second := impl.GetBotIdentity(context.Background())
	if second == nil || second.Name != "Bot One" {
		t.Fatalf("expected stale bot identity after failed refresh, got %#v", second)
	}
	if botInfoCalls != 2 {
		t.Fatalf("expected two bot info calls after failed refresh, got %d", botInfoCalls)
	}

	impl.botIdentityLastFailureAt = time.Now().Add(-61 * time.Second)

	third := impl.GetBotIdentity(context.Background())
	if third == nil || third.Name != "Bot Two" {
		t.Fatalf("expected retry after min refresh interval to succeed, got %#v", third)
	}
	if botInfoCalls != 3 {
		t.Fatalf("expected retry after min refresh interval, got %d calls", botInfoCalls)
	}
}
