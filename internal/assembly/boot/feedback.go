package boot

import (
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/feedback"
)

// feedbackService is the install's report client. A build that cannot make one
// leaves the controller without it, which refuses feedback rather than failing
// the session.
func feedbackService(home string, proxy netclient.ProxySpec) *feedback.Service {
	svc, err := feedback.New(feedback.Config{Home: home, Proxy: proxy})
	if err != nil {
		return nil
	}
	return svc
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
