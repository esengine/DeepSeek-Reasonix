// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package autocert

import (
	"crypto/tls"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"
)

// NewListener returns a net.Listener that listens on the standard TLS
// port (443) on all interfaces and returns *tls.Conn connections with
// LetsEncrypt certificates for the provided domain or domains.
//
// It enables one-line HTTPS servers:
//
//	log.Fatal(http.Serve(autocert.NewListener("example.com"), handler))
//
// NewListener is a convenience function for a common configuration.
// More complex or custom configurations can use the autocert.Manager
// type instead.
//
// Use of this function implies acceptance of the LetsEncrypt Terms of
// Service. If domains is not empty, the provided domains are passed
// to HostWhitelist. If domains is empty, the listener will do
// LetsEncrypt challenges for any requested domain, which is not
// recommended.
//
// Certificates are cached in a "golang-autocert" directory under an
// operating system-specific cache or temp directory. This may not
// be suitable for servers spanning multiple machines.
//
// The returned listener uses a *tls.Config that enables HTTP/2, and
// should only be used with servers that support HTTP/2.
//
// The returned Listener also enables TCP keep-alives on the accepted
// connections. The returned *tls.Conn are returned before their TLS
// handshake has completed.
func NewListener(domains ...string) net.Listener {
	m := &Manager{
		Prompt: AcceptTOS,
	}
	if len(domains) > 0 {
		m.HostPolicy = HostWhitelist(domains...)
	}
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("warning: autocert.NewListener not using a cache: %v", err)
	} else {
		m.Cache = DirCache(dir)
	}
	return m.Listener()
}

// Listener listens on the standard TLS port (443) on all interfaces
// and returns a net.Listener returning *tls.Conn connections.
//
// The returned listener uses a *tls.Config that enables HTTP/2, and
// should only be used with servers that support HTTP/2.
//
// The returned Listener also enables TCP keep-alives on the accepted
// connections. The returned *tls.Conn are returned before their TLS
// handshake has completed.
//
// Unlike NewListener, it is the caller's responsibility to initialize
// the Manager m's Prompt, Cache, HostPolicy, and other desired options.
func (m *Manager) Listener() net.Listener {
	ln := &listener{
		conf: m.TLSConfig(),
	}
	ln.tcpListener, ln.tcpListenErr = net.Listen("tcp", ":443")
	return ln
}

type listener struct {
	conf *tls.Config

	tcpListener  net.Listener
	tcpListenErr error
}

func (ln *listener) Accept() (net.Conn, error) {
	if ln.tcpListenErr != nil {
		return nil, ln.tcpListenErr
	}
	conn, err := ln.tcpListener.Accept()
	if err != nil {
		return nil, err
	}
	tcpConn := conn.(*net.TCPConn)

	// Because Listener is a convenience function, help out with
	// this too.  This is not possible for the caller to set once
	// we return a *tcp.Conn wrapping an inaccessible net.Conn.
	// If callers don't want this, they can do things the manual
	// way and tweak as needed. But this is what net/http does
	// itself, so copy that. If net/http changes, we can change
	// here too.
	tcpConn.SetKeepAlive(true)
	tcpConn.SetKeepAlivePeriod(3 * time.Minute)

	return tls.Server(tcpConn, ln.conf), nil
}

func (ln *listener) Addr() net.Addr {
	if ln.tcpListener != nil {
		return ln.tcpListener.Addr()
	}
	// net.Listen failed. Return something non-nil in case callers
	// call Addr before Accept:
	return &net.TCPAddr{IP: net.IP{0, 0, 0, 0}, Port: 443}
}

func (ln *listener) Close() error {
	if ln.tcpListenErr != nil {
		return ln.tcpListenErr
	}
	return ln.tcpListener.Close()
}

func cacheDir() string {
	const base = "golang-autocert"
	cache, err := os.UserCacheDir()
	if err != nil {
		// Fall back to the root directory.
		cache = "/.cache"
	}

	return filepath.Join(cache, base)
}
