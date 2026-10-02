package anthropic

import (
	"net/http"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/provider"
)

func newHTTPClient(cfg provider.Config) (*http.Client, error) {
	spec, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	client, err := netclient.NewHTTPClient(spec, netclient.TransportOptions{ResponseHeaderTimeout: provider.IdleTimeoutFromExtra(cfg.Extra)})
	if err == nil {
		if reject, _ := cfg.Extra["reject_redirects"].(bool); reject {
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		}
	}
	return client, err
}
