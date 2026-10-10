package serve

import (
	"context"
	"errors"
	"net"
	"net/http"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/safety/typesafe"
)

// checkDecisionProvider verifies an entry whose protocol answers decisions. Such
// a wire has no model listing, so the protocol's own probe replaces the listing
// probe and the finding carries the same typed codes.
func checkDecisionProvider(ctx context.Context, cfg *config.Config, entry *config.ProviderEntry) providerCheck {
	model := entry.DefaultModel()
	if model == "" {
		return providerCheck{Code: codeNoModelsPicked}
	}
	probed := *entry
	if probed.APIKey() == "" {
		probed.ResolveAPIKeyFromProcessEnvForProbe()
	}
	if err := probeDecision(ctx, cfg, &probed, model); err != nil {
		return decisionFinding(err, probed.APIKey)
	}
	return providerCheck{OK: true, Kind: entry.Kind, Matches: true, Models: []string{model}}
}

// probeDecision runs the protocol's own probe the way a saved entry would
// reach the service: through its proxy choice, with the key it carries.
func probeDecision(ctx context.Context, cfg *config.Config, entry *config.ProviderEntry, model string) error {
	proxy := cfg.NetworkProxySpec()
	if entry.NoProxy && proxy.Mode != netclient.ModeCustom {
		proxy = netclient.ProxySpec{Mode: netclient.ModeOff}
	}
	client, err := netclient.NewHTTPClient(proxy, netclient.TransportOptions{})
	if err != nil {
		return errors.New("decision probe client: " + err.Error())
	}
	defer client.CloseIdleConnections()
	return typesafe.Client{HTTP: client, BaseURL: entry.BaseURL, APIKey: entry.APIKey}.Probe(ctx, model)
}

// decisionFinding reads a failed decision probe as a typed finding. Identity
// comes from the HTTP status the service sent, the sentinels the client wraps
// and the transport error types, never from any text.
func decisionFinding(err error, apiKey func() string) providerCheck {
	var httpErr *typesafe.HTTPError
	switch {
	case errors.Is(err, typesafe.ErrKeyMissing):
		return providerCheck{Code: codeProbeUnauthorized}
	case errors.As(err, &httpErr):
		return providerCheck{
			Code:       decisionStatusCode(httpErr.Status),
			HTTPStatus: httpErr.Status,
			Detail:     endpointDetail(httpErr.Body, apiKey),
		}
	case errors.Is(err, typesafe.ErrMalformedResponse):
		return providerCheck{Code: codeProbeDecisionNotCompatible}
	case errors.Is(err, context.DeadlineExceeded):
		return providerCheck{Code: codeProbeTimeout}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return providerCheck{Code: codeProbeUnreachable}
	}
	return providerCheck{Code: codeProbeFailed}
}

func decisionStatusCode(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return codeProbeUnauthorized
	case status == http.StatusPaymentRequired:
		return codeProbePaymentRequired
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		return codeProbeDecisionPathNotFound
	case status == http.StatusTooManyRequests:
		return codeProbeRateLimited
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return codeProbeTimeout
	case status >= http.StatusInternalServerError:
		return codeProbeUpstreamError
	default:
		return codeProbeDecisionRejected
	}
}
