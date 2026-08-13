package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// RequestFailureKind is the stable transport category exposed to frontends.
// The underlying cause remains available through RequestError.Unwrap, but is
// deliberately absent from RequestError.Error so proxy credentials, headers,
// and provider URLs cannot leak through a user-facing error string.
type RequestFailureKind string

const (
	RequestFailureDNS     RequestFailureKind = "dns"
	RequestFailureTLS     RequestFailureKind = "tls"
	RequestFailureProxy   RequestFailureKind = "proxy"
	RequestFailureTimeout RequestFailureKind = "timeout"
	RequestFailureURL     RequestFailureKind = "url"
	RequestFailureNetwork RequestFailureKind = "network"
)

// RequestError classifies a failure before an HTTP response exists.
type RequestError struct {
	Provider string
	Kind     RequestFailureKind
	Cause    error
}

func NewRequestError(provider string, kind RequestFailureKind, cause error) *RequestError {
	if kind == "" {
		kind = RequestFailureNetwork
	}
	return &RequestError{Provider: strings.TrimSpace(provider), Kind: kind, Cause: cause}
}

func (e *RequestError) Error() string {
	if e == nil {
		return "provider request failed"
	}
	label := "provider request failed"
	switch e.Kind {
	case RequestFailureDNS:
		label = "provider DNS resolution failed"
	case RequestFailureTLS:
		label = "provider TLS handshake failed"
	case RequestFailureProxy:
		label = "provider proxy connection failed"
	case RequestFailureTimeout:
		label = "provider request timed out"
	case RequestFailureURL:
		label = "provider request URL is invalid"
	case RequestFailureNetwork:
		label = "provider network request failed"
	}
	if e.Provider == "" {
		return label
	}
	return fmt.Sprintf("%s: %s", e.Provider, label)
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ClassifyRequestFailure maps errors raised before an HTTP response into a
// small stable set. It intentionally uses the error chain and conservative
// transport phrases rather than provider-specific string parsing.
func ClassifyRequestFailure(err error) RequestFailureKind {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return RequestFailureTimeout
	}
	text := strings.ToLower(err.Error())
	if isProxyFailure(text) {
		return RequestFailureProxy
	}
	if isTLSFailure(text) {
		return RequestFailureTLS
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) || isDNSFailure(text) {
		return RequestFailureDNS
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && strings.EqualFold(urlErr.Op, "parse") {
		return RequestFailureURL
	}
	if isURLFailure(text) {
		return RequestFailureURL
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return RequestFailureTimeout
	}
	return RequestFailureNetwork
}

func isProxyFailure(text string) bool {
	for _, marker := range []string{"proxyconnect", "proxy connect", "proxy authorization", "netclient: proxy", "network proxy"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isTLSFailure(text string) bool {
	for _, marker := range []string{"tls:", "tls handshake", "x509:", "certificate", "does not look like a tls handshake", "remote error: tls"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isDNSFailure(text string) bool {
	for _, marker := range []string{"no such host", "name or service not known", "nodename nor servname", "temporary failure in name resolution", "server misbehaving"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isURLFailure(text string) bool {
	for _, marker := range []string{"missing protocol scheme", "unsupported protocol scheme", "invalid url", "invalid control character in url"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func retryableRequestFailure(kind RequestFailureKind) bool {
	switch kind {
	case RequestFailureDNS, RequestFailureTLS, RequestFailureURL:
		return false
	default:
		return true
	}
}
