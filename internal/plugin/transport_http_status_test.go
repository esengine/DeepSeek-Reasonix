package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The status is a fact the host had at the moment it answered. Formatting it
// into the sentence was where it stopped being one: a failure record keeps only
// prose, and the settings row above it was left reading the digits back out of
// text an external server had a hand in writing.
func TestATerminalHTTPFailureKeepsItsStatusAsANumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "go away", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, _, err := StartAll(context.Background(), []Spec{{Name: "e", Type: "http", URL: srv.URL}})
	if err == nil {
		t.Fatal("a server answering 401 to everything did not fail the handshake")
	}
	var status *httpStatusError
	if !errors.As(err, &status) {
		t.Fatalf("no status survived to the terminal error: %v", err)
	}
	if status.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status.Status)
	}
	// The sentence is unchanged. Anything downstream that only displays it —
	// diagnostics, a redaction pass, a settings row — sees what it saw before.
	if !strings.Contains(err.Error(), "http 401: go away") {
		t.Errorf("message = %q, want it to still read http 401 with the detail", err.Error())
	}
}

// The one non-2xx that already had an identity keeps it. Giving every status a
// typed error is exactly the change that could have swallowed this one, and a
// session the host can silently re-establish is not a failure to report.
func TestAnExpiredSessionIsStillItsOwnIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"Session not found"}}`))
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "remote", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()

	_, err = transport.call(context.Background(), "ping", nil)
	var expired *httpSessionExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("an expired session became an ordinary status: %v", err)
	}
	var status *httpStatusError
	if errors.As(err, &status) {
		t.Errorf("it also reads as a plain http %d, which is the arm that would swallow it", status.Status)
	}
}
