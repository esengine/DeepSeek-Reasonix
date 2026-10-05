package openai

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/provider"
)

// prefaceAfterBodyServer behaves like an HTTP/2 edge that withholds its SETTINGS
// until it has read the request: a body that arrives first is acknowledged with
// WINDOW_UPDATE ahead of the server preface, which RFC 9113 §3.4 forbids.
func prefaceAfterBodyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.EnableHTTP2 = true
	srv.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){
		"h2": func(_ *http.Server, conn *tls.Conn, _ http.Handler) { servePrefaceAfterBody(conn) },
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func servePrefaceAfterBody(conn *tls.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(conn, make([]byte, len(http2.ClientPreface))); err != nil {
		return
	}
	fr := http2.NewFramer(conn, conn)
	fr.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	var hbuf bytes.Buffer
	enc := hpack.NewEncoder(&hbuf)
	writeHeaders := func(streamID uint32, endStream bool, fields ...hpack.HeaderField) {
		hbuf.Reset()
		for _, f := range fields {
			_ = enc.WriteField(f)
		}
		_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: streamID, BlockFragment: hbuf.Bytes(), EndHeaders: true, EndStream: endStream})
	}
	prefaced := false
	for {
		f, err := fr.ReadFrame()
		if err != nil {
			return
		}
		switch f := f.(type) {
		case *http2.SettingsFrame:
			if prefaced && !f.IsAck() {
				_ = fr.WriteSettingsAck()
			}
		case *http2.MetaHeadersFrame:
			if f.PseudoValue("method") == http.MethodPost && headerValue(f, "expect") == "100-continue" {
				_ = fr.WriteSettings()
				_ = fr.WriteSettingsAck()
				prefaced = true
				writeHeaders(f.StreamID, false, hpack.HeaderField{Name: ":status", Value: "100"})
			}
		case *http2.DataFrame:
			if !prefaced {
				_ = fr.WriteWindowUpdate(f.StreamID, uint32(len(f.Data())))
				_ = fr.WriteSettings()
				prefaced = true
				continue
			}
			if f.StreamEnded() {
				writeHeaders(f.StreamID, false,
					hpack.HeaderField{Name: ":status", Value: "200"},
					hpack.HeaderField{Name: "content-type", Value: "text/event-stream"})
				_ = fr.WriteData(f.StreamID, true, []byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
			}
		}
	}
}

func headerValue(f *http2.MetaHeadersFrame, name string) string {
	for _, hf := range f.RegularFields() {
		if hf.Name == name {
			return hf.Value
		}
	}
	return ""
}

// productionClientTo is the transport newHTTPClient builds, with api.deepseek.com
// dialled to srv so the official host reaches the fixture.
func productionClientTo(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	hc, err := netclient.NewHTTPClient(netclient.ProxySpec{Mode: netclient.ModeOff}, netclient.TransportOptions{})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	tr := hc.Transport.(*http.Transport)
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only self-signed cert
	addr := srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	return hc
}

func TestOfficialDeepSeekBodyWaitsForServerPreface(t *testing.T) {
	srv := prefaceAfterBodyServer(t)
	hc := productionClientTo(t, srv)

	// The fixture must reproduce the failure for an eager body, or it is not this bug.
	raw, _ := http.NewRequest(http.MethodPost, "https://api.deepseek.com/chat/completions", strings.NewReader(`{"model":"m"}`))
	if resp, err := hc.Do(raw); err == nil {
		_ = resp.Body.Close()
		t.Fatal("an eagerly sent body must hit the WINDOW_UPDATE-before-SETTINGS violation")
	} else if !strings.Contains(err.Error(), "PROTOCOL_ERROR") {
		t.Fatalf("eager body error = %v, want the connection-level PROTOCOL_ERROR", err)
	}

	p, err := New(provider.Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c, ok := p.(*client)
	if !ok {
		t.Fatalf("provider is %T, want *client", p)
	}
	c.http = hc

	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if text.String() != "ok" {
		t.Fatalf("streamed text = %q, want %q", text.String(), "ok")
	}
}

func TestOnlyOfficialDeepSeekRequestsAskToContinue(t *testing.T) {
	var expect string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expect = r.Header.Get("Expect")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "gateway", BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}
	if expect != "" {
		t.Fatalf("custom gateway received Expect: %q, want none", expect)
	}
}
