// Package neterr answers what a transport error is from the identity the
// operating system gave it, never from the sentence it rendered. A system
// error's text arrives in the machine's own language and is reworded between
// releases, so a word list that matches on an English host silently stops
// matching elsewhere. The judgement lives here once because three packages
// were each carrying their own copy of it.
package neterr

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"strings"
)

// IsConnReset reports whether err is a connection-level drop (peer reset,
// truncated body, closed socket) as opposed to a protocol or caller error. A
// stream cut this way mid-body can be replayed from scratch, unlike a decode
// or 4xx error. The common trigger is a local proxy (v2rayN/sing-box)
// idle-closing a long-lived SSE connection during a reasoner's first-token gap.
func IsConnReset(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// A certificate failure is the opposite of a dropped connection: the peer
	// answered the handshake and was refused, on purpose, by a rule no retry
	// changes. It arrives wrapped in a `*url.Error`, which satisfies net.Error,
	// so without this the `errors.As` at the bottom claims it and a reader is
	// sent to check a network that is working. The text half covers the same
	// failure after a retry rebuilt the error from its sentence.
	if IsCertificateError(err) || LooksLikeTLSCertificateFailure(err) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) || isAny(err, resetErrors) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// IsCertificateError reports whether err is a TLS certificate failure: the
// name does not match, the chain does not verify, or the certificate is not
// valid for a reason the OS named. The handshake reached an answer and that
// answer was refused, so the difference between this and a dead network is
// worth keeping — one is fixed by a certificate, the other by a cable.
//
// The judgement reads the error's identity rather than its text. On an Android
// phone behind a tampered resolver the same class shows up as `x509:
// certificate is valid for localhost, not opencode.ai`, and telling that reader
// to check their proxy is telling them to look where the fault is not.
func IsCertificateError(err error) bool {
	if err == nil {
		return false
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verification *tls.CertificateVerificationError
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &verification)
}

func isAny(err error, targets []error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// certificateTextMarkers are the phrases Go's own TLS and x509 code puts in an
// error. They are the fallback for `IsCertificateError` above, not a
// replacement for it.
//
// The identity check is the right one and it is tried first. It is not always
// enough: a provider stream that failed its handshake is retried, and an error
// crossing a retry boundary is sometimes rebuilt from its sentence rather than
// kept as a chain — after which there is no `*tls.CertificateVerificationError`
// left for `errors.As` to find, and the reader is told to check a proxy they
// are not behind.
//
// Matching on text is allowed here for the one reason the package's rule
// permits: these strings do not come from the operating system. An OS error's
// wording is the machine's language and changes between releases; `x509:` and
// `tls:` are Go's own, written once in its source and stable across versions and
// locales. The package doc's warning is about `ECONNRESET`, not about this.
var certificateTextMarkers = []string{
	"x509: ",
	"tls: failed to verify certificate",
	"certificate is valid for",
	"certificate is not valid for",
	"certificate signed by unknown authority",
	"certificate has expired or is not yet valid",
}

// LooksLikeTLSCertificateFailure reports whether err reads as a certificate
// failure even when its original type has been lost to a wrapper. See
// certificateTextMarkers for why matching text is sound in exactly this case.
func LooksLikeTLSCertificateFailure(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	for _, marker := range certificateTextMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
