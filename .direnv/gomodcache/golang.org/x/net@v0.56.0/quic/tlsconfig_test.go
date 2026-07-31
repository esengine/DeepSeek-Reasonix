// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"crypto/tls"

	"golang.org/x/net/internal/testcert"
)

func newTestTLSConfig(side connSide) *tls.Config {
	config := &tls.Config{
		InsecureSkipVerify: true,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
		MinVersion: tls.VersionTLS13,
		// Default key exchange mechanisms as of Go 1.23 minus X25519Kyber768Draft00,
		// which bloats the client hello enough to spill into a second datagram.
		// Tests were written with the assumption each flight in the handshake
		// fits in one datagram, and it's simpler to keep that property.
		CurvePreferences: []tls.CurveID{
			tls.X25519, tls.CurveP256, tls.CurveP384, tls.CurveP521,
		},
	}
	if side == serverSide {
		config.Certificates = []tls.Certificate{testCert}
	}
	return config
}

// newTestTLSConfigWithMoreDefaults returns a *tls.Config for testing
// which behaves more like a default, empty config.
//
// In particular, it uses the default curve preferences, which can increase
// the size of the handshake.
func newTestTLSConfigWithMoreDefaults(side connSide) *tls.Config {
	config := newTestTLSConfig(side)
	config.CipherSuites = nil
	config.CurvePreferences = nil
	return config
}

var testCert = func() tls.Certificate {
	cert, err := tls.X509KeyPair(testcert.LocalhostCert, testcert.LocalhostKey)
	if err != nil {
		panic(err)
	}
	return cert
}()
