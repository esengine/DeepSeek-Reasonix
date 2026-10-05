package netclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Probe is one step of a connectivity check. A misconfigured proxy otherwise
// surfaces as a timeout during a chat turn, which says nothing about where the
// path broke — the point of the steps is to name the one that failed.
type Probe struct {
	Step       string `json:"step"` // proxy | dns | connect | tls
	OK         bool   `json:"ok"`
	Detail     string `json:"detail"`
	DurationMs int64  `json:"durationMs"`
	// Advice is the next thing to try, for failures whose cause is knowable.
	Advice string `json:"advice,omitempty"`
}

// Diagnose walks the path to endpoint under spec and stops at the first failure,
// because every later step would fail for the same reason and reporting three
// red rows hides which one is the cause.
func Diagnose(ctx context.Context, spec ProxySpec, endpoint string) []Probe {
	var out []Probe
	target, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || target.Host == "" {
		return append(out, Probe{Step: "dns", Detail: fmt.Sprintf("读不懂这个地址：%s", endpoint)})
	}

	resolved, perr := ProxyFunc(spec)
	if perr != nil {
		return append(out, Probe{Step: "proxy", Detail: perr.Error(), Advice: "代理配置本身不合法，先改这里"})
	}
	// A nil resolver is the transport's own way of saying "never proxy", not a
	// missing one.
	var proxyURL *url.URL
	if resolved != nil {
		proxyURL, _ = resolved(&http.Request{URL: target})
	}
	out = append(out, Probe{Step: "proxy", OK: true, Detail: proxyDetail(spec, proxyURL)})

	// What must resolve and connect is the proxy when there is one: the endpoint's
	// own name is the proxy's problem to resolve, not ours.
	host := target.Hostname()
	port := target.Port()
	if port == "" {
		port = schemePort(target.Scheme)
	}
	if proxyURL != nil {
		host = proxyURL.Hostname()
		if port = proxyURL.Port(); port == "" {
			port = schemePort(proxyURL.Scheme)
		}
	}

	started := time.Now()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	dns := Probe{Step: "dns", OK: err == nil, DurationMs: since(started)}
	if err != nil {
		dns.Detail = fmt.Sprintf("解析不了 %s：%v", host, err)
		dns.Advice = "域名解析就失败了，通常是 DNS 或者根本没有网"
		return append(out, dns)
	}
	dns.Detail = fmt.Sprintf("%s → %s", host, strings.Join(clip(addrs, 2), ", "))
	out = append(out, dns)

	started = time.Now()
	conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	tcp := Probe{Step: "connect", OK: err == nil, DurationMs: since(started)}
	if err != nil {
		tcp.Detail = fmt.Sprintf("连不上 %s:%s：%v", host, port, err)
		if proxyURL != nil {
			tcp.Advice = "解析得到地址但连不上代理，检查代理是不是没开、端口对不对"
		} else {
			tcp.Advice = "解析得到地址但连不上，可能被防火墙挡了，或者需要走代理"
		}
		return append(out, tcp)
	}
	_ = conn.Close()
	tcp.Detail = fmt.Sprintf("%s:%s 通了", host, port)
	out = append(out, tcp)

	if target.Scheme != "https" {
		return out
	}
	started = time.Now()
	client, err := NewHTTPClient(spec, TransportOptions{DialTimeout: 8 * time.Second, TLSHandshakeTimeout: 8 * time.Second})
	if err != nil {
		return append(out, Probe{Step: "tls", Detail: err.Error(), DurationMs: since(started)})
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, target.Scheme+"://"+target.Host, nil)
	resp, err := client.Do(req)
	tlsProbe := Probe{Step: "tls", OK: err == nil, DurationMs: since(started)}
	if err != nil {
		tlsProbe.Detail = trimTransportError(err)
		// A corporate MITM proxy is the common cause and reads as "the network is
		// broken" unless it is named. It is a certificate problem, not a block.
		var unknownAuthority x509.UnknownAuthorityError
		var certInvalid x509.CertificateInvalidError
		var hostnameErr x509.HostnameError
		switch {
		case errors.As(err, &unknownAuthority), errors.As(err, &certInvalid), errors.As(err, &hostnameErr):
			tlsProbe.Advice = "证书不被信任 —— 公司网络里的解密代理常有这种情况，需要把它的根证书装进系统信任"
		case isTLSRecordError(err):
			tlsProbe.Advice = "对面不像是一个 HTTPS 端点，检查地址或者代理协议是不是填错了"
		}
		return append(out, tlsProbe)
	}
	_ = resp.Body.Close()
	tlsProbe.Detail = fmt.Sprintf("握手成功 · HTTP %d", resp.StatusCode)
	out = append(out, tlsProbe)
	return out
}

func proxyDetail(spec ProxySpec, proxyURL *url.URL) string {
	if proxyURL == nil {
		if NormalizeMode(spec.Mode) == ModeOff {
			return "不走代理（已关闭）"
		}
		return "这个地址直连，不走代理"
	}
	return "经由 " + RedactProxyURL(proxyURL.String())
}

// RedactProxyURL masks credentials embedded in a proxy URL. A proxy line goes
// into screenshots and bug reports far more often than it gets typed.
func RedactProxyURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User == nil {
		return raw
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword(u.User.Username(), "••••")
	}
	return u.String()
}

func schemePort(scheme string) string {
	switch strings.ToLower(scheme) {
	case "https":
		return "443"
	case "socks5", "socks5h":
		return "1080"
	default:
		return "80"
	}
}

func isTLSRecordError(err error) bool {
	var recordErr tls.RecordHeaderError
	return errors.As(err, &recordErr)
}

// trimTransportError drops the operation and URL net/http carries on a
// transport failure, which repeat what the row already says and push the
// actual cause off the end of the line.
func trimTransportError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}

func clip(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return append(values[:n:n], fmt.Sprintf("+%d", len(values)-n))
}

func since(t time.Time) int64 { return time.Since(t).Milliseconds() }
