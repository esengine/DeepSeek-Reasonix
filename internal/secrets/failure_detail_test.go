package secrets

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/provider"
)

func TestFailureDetailPreservesCauseWithoutCredentialsOrResponseEnvelope(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("unknown vision model fixture/vision"), "unknown vision model fixture/vision"},
		{&provider.ImageInputError{Err: errors.New("configured image understanding model does not accept images")}, "does not accept images"},
		{&provider.ImageInputError{Err: &provider.APIError{Status: 400, Body: `{"error":{"message":"bad image API_KEY=sk-private-value-12345"},"request":{"prompt":"private prompt","image":"private pixels"}}`}}, "bad image"},
		{&provider.AuthError{Status: 401, Body: `{"error":{"message":"expired token sk-private-value-12345"}}`}, "expired token"},
		{&provider.APIError{Status: 502, Body: "private prompt private pixels"}, "502"},
		{&provider.RequestFailure{Operation: "send", Err: &url.Error{Op: "Post", URL: "https://user:password@provider.test/v1/chat?prompt=private+prompt#private-fragment", Err: errors.New("connection refused")}}, "connection refused"},
	} {
		got := FailureDetail(tc.err)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("lost cause: %q", got)
		}
		for _, private := range []string{"sk-private-value-12345", "private prompt", "private pixels", "password", "private-fragment", "prompt="} {
			if strings.Contains(got, private) {
				t.Fatalf("leaked %q: %q", private, got)
			}
		}
		b, _ := json.Marshal(got)
		if !json.Valid(b) {
			t.Fatal("invalid persisted text")
		}
	}
	if FailureDetail(nil) != "" {
		t.Fatal("nil failure")
	}
}

func TestFailureDetailRedactsBeforeBoundingUnicode(t *testing.T) {
	got := FailureDetail(errors.New(strings.Repeat("图", 4090) + " API_KEY=" + strings.Repeat("aB1", 4000)))
	if !utf8.ValidString(got) || len([]rune(got)) > 4097 || strings.Contains(got, "aB1") {
		t.Fatalf("invalid bound/redaction: %q", got)
	}
}
