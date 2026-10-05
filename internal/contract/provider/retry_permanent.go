package provider

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
)

// permanentTransportErr reports a transport failure no backoff can fix: the
// endpoint refused the connection, its name does not resolve, or its
// certificate does not verify. These fail on the first attempt.
func permanentTransportErr(err error) bool {
	if connectionRefused(err) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) && dns.IsNotFound {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verification *tls.CertificateVerificationError
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &verification)
}
