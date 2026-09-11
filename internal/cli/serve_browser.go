package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/serve"
)

// serveBrowserBrokerFromEnv builds the desktop browser broker client from the
// environment a desktop bootstrap injected (REASONIX_BROWSER_BROKER /
// REASONIX_BROWSER_TOKEN). Both empty means no broker; exactly one set is a
// broken injection and fails startup loudly instead of half-configuring.
func serveBrowserBrokerFromEnv(getenv func(string) string) (*serve.BrowserBroker, error) {
	if getenv == nil {
		return nil, nil
	}
	endpoint := strings.TrimSpace(getenv(bootstrap.BrowserBrokerEnv))
	token := strings.TrimSpace(getenv(bootstrap.BrowserTokenEnv))
	if endpoint == "" && token == "" {
		return nil, nil
	}
	if endpoint == "" || token == "" {
		return nil, fmt.Errorf("%s and %s must be set together", bootstrap.BrowserBrokerEnv, bootstrap.BrowserTokenEnv)
	}
	return serve.NewBrowserBroker(endpoint, token)
}
