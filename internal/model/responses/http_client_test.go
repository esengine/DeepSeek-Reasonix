package responses

import (
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/provider"
)

func TestInvalidProxyNeverFallsBackToAnotherTransport(t *testing.T) {
	proxy := netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "invalid://proxy"}
	for _, only := range []bool{false, true} {
		for _, kind := range []string{"responses", "dashscope-responses"} {
			_, err := provider.New(kind, provider.Config{HTTP1Only: only, BaseURL: "https://fixture.invalid", Model: "m", Extra: map[string]any{"proxy_spec": proxy}})
			if err == nil {
				t.Fatal("factory accepted invalid proxy")
			}
		}
		p := New(Config{HTTP1Only: only, Proxy: proxy, BaseURL: "https://fixture.invalid", Model: "m"})
		if _, err := p.Stream(t.Context(), provider.Request{}); err == nil {
			t.Fatal("direct constructor discarded transport error")
		}
	}
}
