// Package endpointclient builds the HTTP client for requests that carry a
// credential to an address the user configured. A redirect that leaves that
// origin is refused before the request, and the credential with it, is resent.
package endpointclient

import (
	"net/http"

	"reasonix/internal/base/netclient"
	"reasonix/internal/safety/redirectguard"
)

// New is netclient.NewHTTPClient with the redirect policy of redirectguard.StayOnOrigin.
func New(spec netclient.ProxySpec, opts netclient.TransportOptions) (*http.Client, error) {
	c, err := netclient.NewHTTPClient(spec, opts)
	if err != nil {
		return nil, err
	}
	c.CheckRedirect = redirectguard.StayOnOrigin()
	return c, nil
}
