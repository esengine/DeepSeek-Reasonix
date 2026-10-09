package secrets

import (
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestEndpointConfigProjectionDoesNotMutateOperationalValues(t *testing.T) {
	fields := map[string]string{"PASSWORD": "fixture-secret", "author": "neutral", "ENDPOINT": "https://host/mcp?%74oken=fixture-secret"}
	want := map[string]string{"PASSWORD": EndpointRedacted, "author": "neutral", "ENDPOINT": "https://host/mcp?token=%3Credacted%3E"}
	if got := RedactConfigMap(fields); !reflect.DeepEqual(got, want) {
		t.Fatalf("display = %#v", got)
	}
	if fields["PASSWORD"] != "fixture-secret" {
		t.Fatal("operational map mutated")
	}
	if RedactConfigMap(nil) != nil || RedactArgs(nil) != nil {
		t.Fatal("nil projection changed")
	}
}

func TestEndpointArgumentsMaskCredentialCarrierFlags(t *testing.T) {
	for _, args := range [][]string{
		{"https://user:fixture-secret'private@host/mcp"},
		{"--endpoint=https://user:fixture-secret'private@host/mcp"},
		{"-H", "Authorization: Basic fixture-secret"},
		{"--header", "X-Custom: fixture-secret"},
		{"--env", "CUSTOM=fixture-secret"},
		{"--headers={\"X-Custom\":\"fixture-secret\"}"},
	} {
		if got := strings.Join(RedactArgs(args), " "); strings.Contains(got, "fixture-secret") || strings.Contains(got, "private") {
			t.Fatalf("flag leaked: %s", got)
		}
	}
}

func TestEndpointDiagnosticErrorKeepsIdentityAndMasksEntireURL(t *testing.T) {
	cause := errors.New("fixture unavailable")
	wrapped := &url.Error{Op: "Post", URL: "https://user:fixture secret@host/mcp%zz", Err: cause}
	err := DiagnosticError(wrapped)
	if !errors.Is(err, cause) {
		t.Fatal("error identity lost")
	}
	var endpoint *url.Error
	if !errors.As(err, &endpoint) || endpoint != wrapped {
		t.Fatal("typed cause lost")
	}
	if strings.Contains(err.Error(), "fixture secret") || strings.Contains(err.Error(), "secret@") {
		t.Fatalf("diagnostic leaked: %s", err)
	}
	if DiagnosticError(nil) != nil {
		t.Fatal("nil error changed")
	}
}

func TestRedactEndpointSemicolonQueryKeepsOrigin(t *testing.T) {
	got := RedactEndpoint("https://mcp.example.com/sse?a=1;token=abc")
	if got != "https://mcp.example.com/"+EndpointRedacted {
		t.Fatalf("got %q", got)
	}
}
