package boot

import (
	"context"
	"errors"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/account"
	"reasonix/internal/platform/feedback"
)

// feedbackRetired is the end of service: the build makes no report client, so
// every frontend is told feedback is unavailable. Tests of the live service
// lift it.
var feedbackRetired = true

// feedbackService is the install's report client. A build that cannot make one
// leaves the controller without it, which refuses feedback rather than failing
// the session.
func feedbackService(home string, proxy netclient.ProxySpec, accountsURL string) *feedback.Service {
	if feedbackRetired {
		return nil
	}
	svc, err := feedback.New(feedback.Config{Home: home, Proxy: proxy, Assert: accountAssertion(proxy, accountsURL)})
	if err != nil {
		return nil
	}
	return svc
}

// accountAssertion mints the signed-in account's proof for one link call. It
// reads the stored session at the moment of the call, so signing out takes
// effect at once and a signed-out machine answers ErrAccountRequired.
func accountAssertion(proxy netclient.ProxySpec, accountsURL string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		token := account.Token()
		if token == "" {
			return "", feedback.ErrAccountRequired
		}
		httpClient, err := netclient.NewHTTPClient(proxy, netclient.TransportOptions{})
		if err != nil {
			return "", err
		}
		client := account.New(accountsURL, "reasonix-feedback", httpClient)
		assertion, err := client.FeedbackAssertion(ctx, token)
		if errors.Is(err, account.ErrUnauthorized) {
			return "", feedback.ErrAccountRequired
		}
		return assertion, err
	}
}

// feedbackProviderKind is the coarse provider family a report names. The
// endpoint host decides DeepSeek; the entry's protocol decides the rest.
func feedbackProviderKind(entry *config.ProviderEntry) string {
	if entry == nil {
		return "other"
	}
	if provider.IsDeepSeekEndpoint(entry.BaseURL) {
		return "deepseek"
	}
	switch strings.ToLower(strings.TrimSpace(entry.Kind)) {
	case "", "openai":
		return "openai"
	case "anthropic":
		return "anthropic"
	}
	return "other"
}
