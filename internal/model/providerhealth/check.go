// Package providerhealth checks saved connections without generating model output.
package providerhealth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/model/billing"
	"reasonix/internal/model/catalog"
	"reasonix/internal/model/openai"
	"reasonix/internal/safety/endpointclient"
	"reasonix/internal/safety/redirectguard"
)

// Reading separates a reachable listing from permission to generate with a model.
type Reading struct {
	Connection string `json:"connection"`
	Wallet     string `json:"wallet"`
	CheckedAt  int64  `json:"checkedAt"`
}

func check(ctx context.Context, p *config.ProviderEntry, spec netclient.ProxySpec) Reading {
	got := Reading{Connection: "unknown", Wallet: "unsupported"}
	if p.BalanceURL != "" {
		got.Wallet = "unknown"
	}
	if p.RequiresAPIKey() && p.APIKey() == "" {
		got.Connection = "missing_key"
		return got
	}
	if p.NoProxy && spec.Mode != netclient.ModeCustom {
		spec = netclient.ProxySpec{Mode: netclient.ModeOff}
	}
	client, err := endpointclient.New(spec, netclient.TransportOptions{DialTimeout: 5 * time.Second})
	if err != nil {
		return got
	}
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return redirectguard.ErrRefused }
	if config.CanListModels(p) {
		probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		_, err := catalog.FetchModelsVia(probeCtx, p, client)
		got.Connection = connectionState(err)
		cancel()
	}
	if p.BalanceURL != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		balance, err := billing.FetchFormat(probeCtx, client, p.BalanceURL, billing.Format(p.BalanceFormat), p.APIKey())
		cancel()
		switch {
		case errors.Is(err, billing.ErrUnauthorized):
			got.Wallet = "auth"
		case err == nil && balance != nil && !balance.Available:
			got.Wallet = "insufficient"
		case err == nil && balance != nil:
			got.Wallet = "available"
		}
	}
	return got
}

func connectionState(err error) string {
	switch {
	case err == nil:
		return "reachable"
	case errors.Is(err, context.Canceled):
		return "unknown"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, redirectguard.ErrRefused):
		return "redirect_refused"
	}
	if status, ok := openai.ModelFetchStatus(err); ok {
		if body, _ := openai.ModelFetchBody(err); quotaExhausted(body) {
			return "quota_exhausted"
		}
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			return "auth"
		case status == http.StatusPaymentRequired:
			return "payment_required"
		case status == http.StatusTooManyRequests:
			return "rate_limited"
		case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
			return "timeout"
		case status >= 500:
			return "unreachable"
		default:
			return "unknown"
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "unreachable"
	}
	return "unknown"
}

func quotaExhausted(body string) bool {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	return json.Unmarshal([]byte(body), &envelope) == nil &&
		(envelope.Error.Code == "insufficient_quota" || envelope.Error.Type == "insufficient_quota")
}
