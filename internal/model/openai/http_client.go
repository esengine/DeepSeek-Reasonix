package openai

import (
	"net/http"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/provider"
)

func newHTTPClient(cfg provider.Config) (*http.Client, error) {
	spec, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	return netclient.NewHTTPClient(spec, netclient.TransportOptions{
		HTTP1Only:             cfg.HTTP1Only,
		DialTimeout:           30 * time.Second,
		KeepAlive:             30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: provider.StreamIdleTimeout,
	})
}
