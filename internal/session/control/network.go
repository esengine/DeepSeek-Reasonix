package control

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/safety/endpointclient"
)

// NetworkSettings is the proxy configuration as an editor needs it: the mode,
// the fields behind it, and where the effective value came from. Source matters
// because "auto" reading a proxy out of the environment looks identical to no
// proxy at all until it is named.
type NetworkSettings struct {
	Mode     string `json:"mode"`
	URL      string `json:"url,omitempty"`
	NoProxy  string `json:"noProxy,omitempty"`
	Type     string `json:"type,omitempty"`
	Server   string `json:"server,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	// HasPassword reports that one is stored without handing it back out.
	HasPassword bool `json:"hasPassword,omitempty"`
	// Effective is the human-readable resolution, credentials masked.
	Effective string   `json:"effective"`
	Direct    []string `json:"direct,omitempty"`
	Endpoint  string   `json:"endpoint,omitempty"`
	// DNS and DoH are the resolvers to use instead of the system's. Both are
	// ordinary settings rather than secrets — an endpoint may name a provider
	// the person pays for, but the name of it is not the credential.
	DNS []string `json:"dns,omitempty"`
	DoH string   `json:"doh,omitempty"`
}

// NetworkSettings reads the current proxy configuration.
func (c *Controller) NetworkSettings() NetworkSettings {
	cfg, err := config.Load()
	if err != nil {
		return NetworkSettings{Mode: netclient.ModeAuto, Effective: "读不到配置"}
	}
	spec := cfg.NetworkProxySpec()
	return NetworkSettings{
		Mode:        netclient.NormalizeMode(cfg.Network.ProxyMode),
		URL:         netclient.RedactProxyURL(cfg.Network.ProxyURL),
		NoProxy:     cfg.Network.NoProxy,
		Type:        cfg.Network.Proxy.Type,
		Server:      cfg.Network.Proxy.Server,
		Port:        cfg.Network.Proxy.Port,
		Username:    cfg.Network.Proxy.Username,
		HasPassword: strings.TrimSpace(cfg.Network.Proxy.Password) != "",
		Effective:   netclient.RedactProxyURL(netclient.Summary(spec)),
		Direct:      spec.DirectHosts,
		Endpoint:    c.providerEndpoint(cfg),
		DNS:         cfg.NetworkDNS(),
		DoH:         cfg.NetworkDoH(),
	}
}

// SaveNetworkSettings validates before persisting: a proxy that cannot even be
// composed into a URL would otherwise be written and then fail on every request
// afterwards, far from the screen that set it. An empty password keeps whatever
// is stored, since the editor is never handed the current one.
func (c *Controller) SaveNetworkSettings(in NetworkSettings, password string, clearPassword bool) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Network.ProxyMode = netclient.NormalizeMode(in.Mode)
	cfg.Network.ProxyURL = strings.TrimSpace(in.URL)
	cfg.Network.NoProxy = strings.TrimSpace(in.NoProxy)
	cfg.Network.Proxy.Type = strings.TrimSpace(in.Type)
	cfg.Network.Proxy.Server = strings.TrimSpace(in.Server)
	cfg.Network.Proxy.Port = in.Port
	cfg.Network.Proxy.Username = strings.TrimSpace(in.Username)
	switch {
	case clearPassword:
		cfg.Network.Proxy.Password = ""
	case strings.TrimSpace(password) != "":
		cfg.Network.Proxy.Password = password
	}
	if err := netclient.Validate(cfg.NetworkProxySpec()); err != nil {
		return fmt.Errorf("这个代理设置用不了：%w", err)
	}
	// Kept as written, and validated only for shape: a resolver that does not
	// answer is a network fact, not a typo, and refusing to save one would leave
	// the person unable to fix a broken resolver by pointing at a working one.
	cfg.Network.DNSServers = trimAll(in.DNS)
	cfg.Network.DoH = strings.TrimSpace(in.DoH)
	if err := validateDoH(cfg.Network.DoH); err != nil {
		return err
	}
	return cfg.SaveTo(config.UserConfigPath())
}

// validateDoH rejects an endpoint that is not an HTTPS URL, because a DoH
// client that accepted `http://` would send the query it exists to protect in
// clear text — the exact failure this setting is here to fix.
func validateDoH(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("DNS over HTTPS 地址读不懂：%w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("DNS over HTTPS 必须是 https 地址，当前是 %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("DNS over HTTPS 地址缺少主机名")
	}
	return nil
}

func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if v := strings.TrimSpace(s); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// NetworkProbe is one diagnosed step. The auth step is here rather than in
// netclient because only the controller knows which provider is selected and
// what counts as a usable answer from it.
type NetworkProbe = netclient.Probe

// DiagnoseNetwork walks proxy → dns → connect → tls → auth against the active
// provider. Each step names the layer it tested, so a failure points at what to
// change instead of at "the network".
func (c *Controller) DiagnoseNetwork(ctx context.Context) []NetworkProbe {
	cfg, err := config.Load()
	if err != nil {
		return []NetworkProbe{{Step: "proxy", Detail: "读不到配置：" + err.Error()}}
	}
	spec := cfg.NetworkProxySpec()
	endpoint := c.providerEndpoint(cfg)
	if endpoint == "" {
		return []NetworkProbe{{Step: "proxy", Detail: "没有配置 provider，没有可以测的目标"}}
	}
	probes := netclient.Diagnose(ctx, spec, endpoint)
	if len(probes) == 0 || !probes[len(probes)-1].OK {
		return probes
	}
	return append(probes, c.probeProviderAuth(ctx, cfg, spec, endpoint))
}

// probeProviderAuth asks the endpoint a question only a credential can answer.
// A reachable host that rejects the key is a completely different problem from
// an unreachable one, and the two are indistinguishable from a chat timeout.
func (c *Controller) probeProviderAuth(ctx context.Context, cfg *config.Config, spec netclient.ProxySpec, endpoint string) NetworkProbe {
	started := time.Now()
	probe := NetworkProbe{Step: "auth"}
	entry, ok := cfg.ResolveModel(c.ModelRef())
	if !ok {
		probe.Detail = "没有可用的模型配置"
		return probe
	}
	key := strings.TrimSpace(entry.APIKey())
	if key == "" {
		probe.Detail = "没有找到 API key"
		probe.Advice = "网络这一侧是通的，缺的是 key —— 去「模型」那页填"
		probe.DurationMs = time.Since(started).Milliseconds()
		return probe
	}
	client, err := endpointclient.New(spec, netclient.TransportOptions{DialTimeout: 8 * time.Second})
	if err != nil {
		probe.Detail = err.Error()
		return probe
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/models", nil)
	if err != nil {
		probe.Detail = err.Error()
		return probe
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("x-api-key", key)
	resp, err := client.Do(req)
	probe.DurationMs = time.Since(started).Milliseconds()
	if err != nil {
		probe.Detail = err.Error()
		return probe
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		probe.Detail = fmt.Sprintf("key 被拒了 — HTTP %d", resp.StatusCode)
		probe.Advice = "网络通了，是 key 的问题，去「模型」那页换一个"
	case resp.StatusCode >= 500:
		probe.Detail = fmt.Sprintf("对面出错了 — HTTP %d", resp.StatusCode)
		probe.Advice = "不是你的配置问题，是服务端；过一会儿再试"
	default:
		// Some endpoints answer 404 to /models and still work; the credential was
		// accepted either way, which is what this step is asking.
		probe.OK = true
		probe.Detail = fmt.Sprintf("key 可用 — HTTP %d", resp.StatusCode)
	}
	return probe
}

func (c *Controller) providerEndpoint(cfg *config.Config) string {
	entry, ok := cfg.ResolveModel(c.ModelRef())
	if !ok {
		return ""
	}
	return strings.TrimSpace(entry.BaseURL)
}
