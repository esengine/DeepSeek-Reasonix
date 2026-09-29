package netclient_test

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"reasonix/internal/base/netclient"
)

// A peer that negotiates h2 but sends an illegal frame reproduces the failure
// mechanism; compatibility mode must avoid h2 before sending, never replay it.
func TestHTTP1CompatibilityAvoidsBrokenHTTP2Peer(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Errorf("protocol = %s", r.Proto)
		}
		_, _ = io.WriteString(w, "OK")
	}))
	server.EnableHTTP2 = true
	server.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){
		"h2": func(_ *http.Server, conn *tls.Conn, _ http.Handler) {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := io.ReadFull(conn, make([]byte, len(http2.ClientPreface))); err != nil {
				return
			}
			_, _ = conn.Write([]byte{0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
			_, _ = io.Copy(io.Discard, conn)
		},
	}
	server.StartTLS()
	defer server.Close()
	for _, only := range []bool{false, true} {
		client, err := netclient.NewHTTPClient(netclient.ProxySpec{Mode: netclient.ModeOff}, netclient.TransportOptions{HTTP1Only: only})
		if err != nil {
			t.Fatal(err)
		}
		tr := client.Transport.(*http.Transport)
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.RootCAs = server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
		defer tr.CloseIdleConnections()
		client.Timeout = 3 * time.Second
		resp, err := client.Get(server.URL)
		if !only {
			if err == nil {
				resp.Body.Close()
				t.Fatal("broken h2 unexpectedly succeeded")
			}
			if !isProtocolError(err) {
				t.Fatalf("expected HTTP/2 protocol failure, got %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(body) != "OK" || resp.ProtoMajor != 1 {
			t.Fatalf("response = %s %q, %v", resp.Proto, body, err)
		}
	}
}

func TestHTTP1CompatibilityKeepsProxyAndTLSValidation(t *testing.T) {
	tr, err := netclient.NewTransport(netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "http://proxy.invalid:8080"}, netclient.TransportOptions{HTTP1Only: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequest(http.MethodGet, "https://provider.invalid/", nil)
	proxy, err := tr.Proxy(req)
	if err != nil || proxy.String() != "http://proxy.invalid:8080" {
		t.Fatalf("proxy = %v, %v", proxy, err)
	}
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("compatibility bypassed certificate validation")
	}
	if !http.DefaultTransport.(*http.Transport).ForceAttemptHTTP2 {
		t.Fatal("changed the global transport")
	}
}

func isProtocolError(err error) bool {
	for err != nil {
		v := reflect.ValueOf(err)
		if v.Type().PkgPath() == "net/http" && v.Type().Name() == "http2ConnectionError" && v.Kind() == reflect.Uint32 {
			return v.Uint() == uint64(http2.ErrCodeProtocol)
		}
		err = errors.Unwrap(err)
	}
	return false
}

func TestHTTP1CompatibilityClearsClonedHTTP2State(t *testing.T) {
	original := http.DefaultTransport
	base := original.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}}
	base.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{
		"h2": func(string, *tls.Conn) http.RoundTripper { t.Fatal("h2 handler invoked"); return nil },
	}
	http.DefaultTransport = base
	t.Cleanup(func() { http.DefaultTransport = original; base.CloseIdleConnections() })
	tr, err := netclient.NewTransport(netclient.ProxySpec{Mode: netclient.ModeOff}, netclient.TransportOptions{HTTP1Only: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.CloseIdleConnections()
	if tr.Protocols == nil || !tr.Protocols.HTTP1() || tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() || tr.ForceAttemptHTTP2 || len(tr.TLSNextProto) != 0 {
		t.Fatal("HTTP/2 remains enabled on the cloned transport")
	}
	if !slices.Equal(tr.TLSClientConfig.NextProtos, []string{"http/1.1"}) || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("ALPN or TLS policy was not preserved correctly")
	}
	if base.TLSClientConfig.NextProtos[0] != "h2" || base.TLSNextProto["h2"] == nil {
		t.Fatal("mutated the source transport")
	}
}
