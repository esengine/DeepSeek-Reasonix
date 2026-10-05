package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"reasonix/internal/contract/provider"
)

// openCodeGoGateway stands in for opencode.ai: it refuses a request without a
// session exactly as the gateway does (#10921) and records the sessions it saw.
func openCodeGoGateway(t *testing.T, sessions *[]string) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("x-opencode-session")
		if id == "" || r.Header.Get("User-Agent") != "Reasonix" {
			http.Error(w, `{"error":{"message":"Request is missing x-opencode-session and cannot be routed efficiently."}}`, http.StatusBadRequest)
			return
		}
		*sessions = append(*sessions, id)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	return &http.Client{Transport: redirectTransport{target: target}}
}

type redirectTransport struct{ target *url.URL }

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host = rt.target.Scheme, rt.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

func streamOnce(t *testing.T, p provider.Provider) {
	t.Helper()
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
}

func TestOpenCodeGoChatCarriesStableSession(t *testing.T) {
	var sessions []string
	httpClient := openCodeGoGateway(t, &sessions)
	newClient := func() provider.Provider {
		p, err := New(provider.Config{Name: "opencode-go", BaseURL: "https://opencode.ai/zen/go/v1/", Model: "glm-5.2", APIKey: "k"})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		p.(*client).http = httpClient
		return p
	}
	first, second := newClient(), newClient()
	streamOnce(t, first)
	streamOnce(t, first)
	streamOnce(t, second)
	if len(sessions) != 3 || sessions[0] != sessions[1] || sessions[1] == sessions[2] {
		t.Fatalf("sessions = %q, want one stable id per client", sessions)
	}
}

// OpenCode Go's chat route refuses a role=tool message carrying "name", so the
// first tool round trip failed even with a session.
func TestOpenCodeGoChatToolResultOmitsName(t *testing.T) {
	p, err := New(provider.Config{Name: "opencode-go", BaseURL: "https://opencode.ai/zen/go/v1", Model: "glm-5.2", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	history := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "read it"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "data"},
	}}
	for _, m := range p.(*client).buildRequest(history).Messages {
		if m.Role == string(provider.RoleTool) && m.Name != nil {
			t.Fatalf("tool message carries name %q; OpenCode Go answers 400", *m.Name)
		}
	}

	other, err := New(provider.Config{Name: "mimo", BaseURL: "https://relay.example.com/v1", Model: "m", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, m := range other.(*client).buildRequest(history).Messages {
		if m.Role == string(provider.RoleTool) && (m.Name == nil || *m.Name != "read_file") {
			t.Fatalf("tool message name = %v, want read_file on every other endpoint", m.Name)
		}
	}
}
