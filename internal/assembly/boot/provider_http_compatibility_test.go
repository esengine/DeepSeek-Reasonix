package boot

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

func TestProviderHTTPCompatibilityPreservesWireBodyAndStreaming(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			var bodies [][]byte
			var protocols []int
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				bodies = append(bodies, body)
				protocols = append(protocols, r.ProtoMajor)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				switch kind {
				case "openai":
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				case "anthropic":
					_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				case "responses":
					_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\"}}\n\n")
				}
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			// The factory still creates the real production transport; only its
			// default trust roots point to this disposable TLS fixture.
			original := http.DefaultTransport
			base := original.(*http.Transport).Clone()
			base.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			http.DefaultTransport = base
			defer func() { http.DefaultTransport = original; base.CloseIdleConnections() }()
			for _, only := range []bool{false, true} {
				entry := config.ProviderEntry{Name: "fixture", Kind: kind, BaseURL: server.URL, Model: "fixture", HTTP1Only: only}
				entry = entry.WithAPIKeyForProbe("fixture-secret")
				resolver := NewLocalProviderResolver(&config.Config{Providers: []config.ProviderEntry{entry}}, netclient.ProxySpec{Mode: netclient.ModeOff})
				p, err := resolver.Resolve(provider.Selection{Ref: "fixture/fixture"})
				if err != nil {
					t.Fatal(err)
				}
				stream, err := p.Stream(t.Context(), provider.Request{Messages: []provider.Message{{Role: "user", Content: "hello"}}})
				if err != nil {
					t.Fatal(err)
				}
				var text strings.Builder
				for chunk := range stream {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
					text.WriteString(chunk.Text)
				}
				if text.String() != "OK" {
					t.Fatalf("stream = %q", text.String())
				}

			}
			mu.Lock()
			defer mu.Unlock()
			if len(bodies) != 2 || protocols[0] != 2 || protocols[1] != 1 {
				t.Fatalf("requests = %v", protocols)
			}
			if !bytes.Equal(bodies[0], bodies[1]) {
				t.Fatal("compatibility changed provider-visible request body")
			}
		})
	}
}
