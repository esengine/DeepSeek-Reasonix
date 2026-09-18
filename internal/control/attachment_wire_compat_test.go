package control

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	"reasonix/internal/provider/responses"
)

func TestLegacyImageResolverPreservesSerializedProtocolBytes(t *testing.T) {
	for _, protocol := range []string{"openai", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			bodies := make(chan []byte, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				bodies <- body
				// A terminal client error avoids synthetic stream differences and
				// retries; the real adapter has already serialized the request.
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"fixture captured","type":"invalid_request_error"}}`))
			}))
			defer server.Close()
			info := &provider.ModelInfo{ID: "vision", InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}
			cfg := provider.Config{Name: "fixture", Model: "vision", BaseURL: server.URL, APIKey: "fixture-only", ModelInfo: info}
			var p provider.Provider
			var err error
			switch protocol {
			case "openai":
				p, err = openai.New(cfg)
			case "anthropic":
				p, err = anthropic.New(cfg)
			case "responses":
				p = responses.New(responses.Config{Name: "fixture", Model: "vision", BaseURL: server.URL, APIKey: "fixture-only", ModelInfo: info})
			}
			if err != nil {
				t.Fatal(err)
			}
			messages := []provider.Message{{Role: provider.RoleSystem, Content: "unchanged system"}, {Role: provider.RoleUser, Content: "legacy image", Images: []string{"data:image/png;base64," + tinyPNG}}, {Role: provider.RoleAssistant, Content: "unchanged history"}, {Role: provider.RoleUser, Content: "next turn"}}
			c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), ImageRouteConfig: config.Default()})
			resolved, err := c.ResolveRequestImagesForModel(t.Context(), messages, "fixture/vision", true)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range [][]provider.Message{messages, resolved} {
				ch, _ := p.Stream(t.Context(), provider.Request{Messages: input})
				if ch != nil {
					for range ch {
					}
				}
			}
			if len(bodies) != 2 {
				t.Fatalf("adapter sent %d requests, want 2", len(bodies))
			}
			before, after := <-bodies, <-bodies
			if !bytes.Equal(before, after) {
				t.Fatalf("legacy protocol bytes changed:\nbefore %s\nafter %s", before, after)
			}
			if !bytes.Contains(after, []byte(tinyPNG)) {
				t.Fatal("fixture did not exercise image serialization")
			}
		})
	}
}
