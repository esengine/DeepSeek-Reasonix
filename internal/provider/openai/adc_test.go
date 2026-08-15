package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"reasonix/internal/provider"
)

type stubTokenSource struct {
	tok string
	err error
}

func (s stubTokenSource) Token() (*oauth2.Token, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &oauth2.Token{AccessToken: s.tok, TokenType: "Bearer"}, nil
}

const adcTestSSE = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n"

func TestADCModeSendsBearerToken(t *testing.T) {
	restore := newADCTokenSource
	newADCTokenSource = func() (oauth2.TokenSource, error) {
		return stubTokenSource{tok: "stub-adc-token"}, nil
	}
	t.Cleanup(func() { newADCTokenSource = restore })

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, adcTestSSE)
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "vertex",
		BaseURL: srv.URL,
		Model:   "google/gemini-3.6-flash",
		Extra:   map[string]any{"auth": "adc"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}
	if gotAuth != "Bearer stub-adc-token" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer stub-adc-token")
	}
}

func TestADCModeConstructionErrorSurfaces(t *testing.T) {
	restore := newADCTokenSource
	newADCTokenSource = func() (oauth2.TokenSource, error) {
		return nil, errors.New("no ambient credentials")
	}
	t.Cleanup(func() { newADCTokenSource = restore })

	_, err := New(provider.Config{
		Name:    "vertex",
		BaseURL: "https://example.invalid",
		Model:   "google/gemini-3.6-flash",
		Extra:   map[string]any{"auth": "adc"},
	})
	if err == nil || !strings.Contains(err.Error(), "adc") {
		t.Fatalf("expected adc construction error, got %v", err)
	}
}

func TestADCModeTokenFetchFailureFailsStream(t *testing.T) {
	restore := newADCTokenSource
	newADCTokenSource = func() (oauth2.TokenSource, error) {
		return stubTokenSource{err: errors.New("metadata unavailable")}, nil
	}
	t.Cleanup(func() { newADCTokenSource = restore })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not be sent when token minting fails")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "vertex",
		BaseURL: srv.URL,
		Model:   "google/gemini-3.6-flash",
		Extra:   map[string]any{"auth": "adc"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "adc: fetch access token") {
		t.Fatalf("expected adc fetch failure, got %v", err)
	}
}
