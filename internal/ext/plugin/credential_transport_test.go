package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type credentialFailureTransport struct{ cause error }

func (t credentialFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.cause
}

func TestHTTPTransportErrorsMaskURLAndPreserveIdentity(t *testing.T) {
	cause := errors.New("fixture transport unavailable")
	transport := &httpTransport{name: "neutral", url: "https://host/mcp?%74oken=fixture-secret", client: &http.Client{Transport: credentialFailureTransport{cause: cause}}}
	_, err := transport.call(context.Background(), "initialize", nil)
	if !errors.Is(err, cause) {
		t.Fatalf("identity lost: %v", err)
	}
	if strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("transport leaked: %v", err)
	}
}

func TestSSETransportErrorsMaskURLAndPreserveIdentity(t *testing.T) {
	cause := errors.New("fixture transport unavailable")
	endpoint, err := url.Parse("https://host/mcp?%74oken=fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	transport := &sseTransport{endpoint: endpoint, client: &http.Client{Transport: credentialFailureTransport{cause: cause}}}
	err = transport.post(context.Background(), nil)
	if !errors.Is(err, cause) {
		t.Fatalf("identity lost: %v", err)
	}
	if strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("SSE leaked: %v", err)
	}
	_, err = newSSETransport(context.Background(), Spec{Name: "neutral", URL: "https://user:fixture-secret@host/mcp%zz"})
	if err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("invalid SSE endpoint leaked: %v", err)
	}
}

func TestSSEEndpointParseFailureKeepsEveryDiagnosticRedacted(t *testing.T) {
	base, err := url.Parse("https://host/mcp")
	if err != nil {
		t.Fatal(err)
	}
	transport := &sseTransport{pending: map[int]chan rpcResponse{}, endpointReady: make(chan struct{})}
	transport.handleEvent("endpoint", "https://user:fixture-secret@host/mcp%zz", base)
	for _, err := range []error{transport.waitEndpoint(context.Background()), transport.readErr} {
		if err == nil || strings.Contains(err.Error(), "fixture-secret") {
			t.Fatalf("SSE endpoint diagnostic leaked: %v", err)
		}
	}
}
