package dingtalk

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// dingtalkWebhookHosts is the sessionWebhook hostname allowlist. DingTalk
// supplies this callback URL, and replies must never be redirected elsewhere.
var dingtalkWebhookHosts = []string{"api.dingtalk.com", "oapi.dingtalk.com"}

func (a *adapter) validDingtalkWebhook(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "https" && !(a.allowHTTPWebhook && u.Scheme == "http") {
		return false
	}
	for _, allowed := range a.webhookHosts {
		if strings.EqualFold(u.Hostname(), allowed) {
			return true
		}
	}
	return false
}

// webhookClient clones the shared client so redirect policy is scoped to
// webhook sends. Every hop is revalidated and retains net/http's 10-hop limit.
func (a *adapter) webhookClient() *http.Client {
	client := *a.httpClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !a.validDingtalkWebhook(req.URL.String()) {
			return fmt.Errorf("dingtalk send rejected redirect to non-dingtalk endpoint")
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	return &client
}
