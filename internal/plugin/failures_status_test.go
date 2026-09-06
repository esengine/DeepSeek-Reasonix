package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"reasonix/internal/testenv"
)

// The status a failure ended at, carried as the number the host held. Reading
// it back out of Error is what the row above this used to do, over text the
// server itself wrote and this package then sanitised and truncated.
func TestAFailureCarriesTheStatusItEndedAt(t *testing.T) {
	h := &Host{}
	h.RecordFailure(Spec{Name: "remote", Type: "http"},
		fmt.Errorf("plugin %q: initialize: %w", "remote", &httpStatusError{Status: http.StatusUnauthorized, Detail: "go away"}))

	got := h.Failures()
	if len(got) != 1 {
		t.Fatalf("failures = %d, want 1", len(got))
	}
	if got[0].HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want 401", got[0].HTTPStatus)
	}
}

// The number and the sentence are independent. One says what the endpoint
// answered; the other is detail, and an external server has a hand in writing
// it — including, if it likes, other numbers.
func TestTheStatusIsNotReadOutOfTheMessage(t *testing.T) {
	h := &Host{}
	h.RecordFailure(Spec{Name: "a", Type: "http"},
		fmt.Errorf("plugin %q: %w", "a", &httpStatusError{Status: http.StatusForbidden, Detail: "401 unauthorized forbidden auth please login"}))
	h.RecordFailure(Spec{Name: "b", Type: "stdio"}, errors.New("401 unauthorized forbidden auth please login"))

	by := map[string]Failure{}
	for _, f := range h.Failures() {
		by[f.Name] = f
	}
	if by["a"].HTTPStatus != http.StatusForbidden {
		t.Errorf("a: HTTPStatus = %d, want 403 — the typed status, not the 401 in the text", by["a"].HTTPStatus)
	}
	// Nothing typed said a status, so there is none. Prose full of the words the
	// old regex looked for buys no classification at all.
	if by["b"].HTTPStatus != 0 {
		t.Errorf("b: HTTPStatus = %d, want 0 for a failure that was not an HTTP one", by["b"].HTTPStatus)
	}
}

// A tripwire, not a specification: a forced refresh returns early while the
// local clock still likes the token, so a retry goes out with the one the
// server just refused. Until that is settled a 401 does not mean "authorise
// again". When this fails the recovery was fixed — delete it and write the real
// negative control, that such a 401 never becomes a Failure at all.
func TestKnownGapAForcedRefreshDoesNotForce(t *testing.T) {
	dir := testenv.TempDir(t)
	state := mcpOAuthState{
		Version: 1, Resource: "https://example.invalid/mcp", Issuer: "https://example.invalid",
		ClientID: "c", ClientSecret: "s",
		// Unreachable on purpose: reaching it at all is the fix landing.
		TokenEndpoint: "http://127.0.0.1:1/token", TokenEndpointAuthMethod: "client_secret_basic",
		AccessToken: "rejected-by-the-server", RefreshToken: "r", TokenType: "Bearer",
		Expiry: time.Now().Add(time.Hour),
	}
	if err := saveMCPOAuthState(dir, state); err != nil {
		t.Fatal(err)
	}
	c := &mcpOAuthClient{stateDir: dir, state: state, client: http.DefaultClient}

	header, used, err := c.authorizationHeader(context.Background(), true)
	if err != nil || !used {
		t.Fatalf("forced refresh answered (%q, %v, %v); the gap is that it answers at all", header, used, err)
	}
	if header != "Bearer rejected-by-the-server" {
		t.Fatalf("header = %q — a forced refresh no longer hands back the refused token, so this tripwire is done", header)
	}
}
