package responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"reasonix/internal/contract/provider"
)

type redirectTransport struct{ target *url.URL }

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host = rt.target.Scheme, rt.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

// The OpenCode Go Responses route refuses a request without a session (#10921).
func TestOpenCodeGoResponsesCarrySession(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-opencode-session")
		if got == "" {
			http.Error(w, `{"error":{"message":"Request is missing x-opencode-session"}}`, http.StatusBadRequest)
			return
		}
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	}))
	defer srv.Close()
	target, _ := url.Parse(srv.URL)

	p := New(Config{Name: "opencode-go-responses", BaseURL: "https://opencode.ai/zen/go/v1", Model: "deepseek-v4-flash", APIKey: "k"}).(*client)
	p.http = &http.Client{Transport: redirectTransport{target: target}}
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got == "" {
		t.Fatal("request reached the gateway without x-opencode-session")
	}
}
