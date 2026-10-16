package neterr

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"testing"
)

func TestIsConnReset(t *testing.T) {
	if IsConnReset(nil) {
		t.Error("nil is not a conn reset")
	}
	if IsConnReset(context.Canceled) || IsConnReset(context.DeadlineExceeded) {
		t.Error("ctx cancel/deadline must not look like a recoverable reset")
	}
	if IsConnReset(errors.New("decode stream: invalid character")) {
		t.Error("a plain protocol error must not be treated as a conn reset")
	}
	for _, err := range []error{
		io.ErrUnexpectedEOF,
		&net.OpError{Op: "read", Err: resetErrors[0]},
		fmt.Errorf("read stream: %w", &net.OpError{Op: "read", Err: errors.New("wsarecv: forcibly closed")}),
	} {
		if !IsConnReset(err) {
			t.Errorf("want conn reset for %v", err)
		}
	}
}

func TestIsConnResetExcludesDNS(t *testing.T) {
	for _, dns := range []*net.DNSError{
		{Err: "no such host", Name: "api.deepseek.com", IsNotFound: true},
		{Err: "i/o timeout", Name: "api.deepseek.com", IsTimeout: true, IsTemporary: true},
	} {
		err := fmt.Errorf("dial: %w", &net.OpError{Op: "dial", Err: dns})
		if IsConnReset(err) {
			t.Errorf("DNS failure %v is not a connection reset", dns)
		}
	}
}

// A certificate that is refused is not a connection that dropped.
//
// The shape matters: net/http wraps a transport failure in a `*url.Error`, and
// `*url.Error` implements net.Error, so the last line of IsConnReset claimed
// every one of them — including this. A reader behind a poisoned resolver then
// saw "check the provider/proxy connection" for a network that was working,
// while the diagnose page showed the same handshake failing on the certificate.
func TestIsConnResetExcludesCertificateFailures(t *testing.T) {
	hostname := &url.Error{Op: "Post", URL: "https://opencode.ai/zen/go/v1", Err: x509.HostnameError{
		Host: "opencode.ai",
		Certificate: &x509.Certificate{
			Subject:  pkix.Name{CommonName: "localhost"},
			DNSNames: []string{"localhost"},
		},
	}}
	unknown := &url.Error{Op: "Post", URL: "https://a.example", Err: x509.UnknownAuthorityError{
		Cert: &x509.Certificate{Subject: pkix.Name{CommonName: "redirect-cnzz"}},
	}}
	verification := &url.Error{Op: "Post", URL: "https://a.example", Err: &tls.CertificateVerificationError{
		Err: x509.CertificateInvalidError{Reason: x509.Expired},
	}}

	for _, err := range []error{hostname, unknown, verification} {
		if !IsCertificateError(err) {
			t.Errorf("want a certificate failure for %v", err)
		}
		if IsConnReset(err) {
			t.Errorf("a refused certificate is not a conn reset: %v", err)
		}
	}

	// And the thing it must not swallow: a genuinely dropped connection keeps
	// answering yes, wrapped exactly as net/http wraps it.
	cut := &url.Error{Op: "Post", URL: "https://a.example", Err: &net.OpError{
		Op: "read", Err: errors.New("wsarecv: an existing connection was forcibly closed"),
	}}
	if IsCertificateError(cut) {
		t.Error("a dropped connection is not a certificate failure")
	}
	if !IsConnReset(cut) {
		t.Error("a dropped connection must still read as a conn reset")
	}
}
