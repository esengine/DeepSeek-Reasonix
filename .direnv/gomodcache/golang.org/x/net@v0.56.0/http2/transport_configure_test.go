// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package http2_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "golang.org/x/net/http2"
)

// TestConfigureTransportEnablesHTTP2 verifies that ConfigureTransport and
// ConfigureTransports enable HTTP/2 on a transport with a custom TLSClientConfig.
// net/http does not auto-enable HTTP/2 for such a transport, so without
// ConfigureTransport doing so the request is sent over HTTP/1 to an HTTP/2
// server. The test is intentionally not build-tagged: the behavior must hold for
// the legacy implementation (transport.go) and the go1.27 net/http wrapper
// (transport_wrap.go) alike.
func TestConfigureTransportEnablesHTTP2(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*http.Transport) error
	}{
		{"ConfigureTransport", ConfigureTransport},
		{"ConfigureTransports", func(t1 *http.Transport) error {
			_, err := ConfigureTransports(t1)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, r.Proto)
			}))
			if err := ConfigureServer(ts.Config, &Server{}); err != nil {
				t.Fatal(err)
			}
			ts.TLS = ts.Config.TLSConfig
			ts.StartTLS()
			defer ts.Close()

			// A custom TLSClientConfig disables net/http's automatic HTTP/2;
			// ConfigureTransport is responsible for re-enabling it.
			tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
			if err := tc.configure(tr); err != nil {
				t.Fatal(err)
			}
			res, err := (&http.Client{Transport: tr}).Get(ts.URL)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.ProtoMajor != 2 {
				t.Errorf("request negotiated %s (server saw %q); want HTTP/2", res.Proto, body)
			}
		})
	}
}
