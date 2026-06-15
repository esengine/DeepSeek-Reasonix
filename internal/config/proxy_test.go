package config

import "testing"

func TestDirectProxyHostsFromNoProxyProviders(t *testing.T) {
	spec := Default().NetworkProxySpec()
	hasMimo := false
	for _, h := range spec.DirectHosts {
		if h == "token-plan-cn.xiaomimimo.com" {
			hasMimo = true
		}
		if h == "api.deepseek.com" {
			t.Errorf("DeepSeek works through the proxy and must not be forced direct: %v", spec.DirectHosts)
		}
	}
	if !hasMimo {
		t.Errorf("a no_proxy provider's host should land in DirectHosts, got %v", spec.DirectHosts)
	}
}

func TestExplicitProxyOverridesProviderNoProxy(t *testing.T) {
	// An explicit custom proxy (e.g. a mandatory corporate proxy) must apply to
	// every provider, including no_proxy ones like mimo, so it isn't unreachable
	// behind the proxy (#3635).
	c := Default()
	c.Network.ProxyMode = "custom"
	spec := c.NetworkProxySpec()
	for _, h := range spec.DirectHosts {
		if h == "token-plan-cn.xiaomimimo.com" {
			t.Fatalf("custom proxy must not force mimo direct; DirectHosts = %v", spec.DirectHosts)
		}
	}
}
