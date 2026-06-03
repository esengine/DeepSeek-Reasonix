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
