package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/testenv"
)

// What forceRefresh has to mean. The question is not "is this token stale by my
// clock" but "is the one the server just refused still the canonical one" — and
// the credential's identity is the access token, being the bytes that were
// shown and refused. No generation field is needed or wanted: a refresh handing
// back the same access token would not have moved the state either.
type refreshFixture struct {
	dir   string
	calls *atomic.Int64
	state mcpOAuthState
}

// newRefreshFixture stores one token and stands up a token endpoint that counts
// what reaches it and hands back "granted".
func newRefreshFixture(t *testing.T, access string, expiry time.Time) *refreshFixture {
	t.Helper()
	calls := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "granted", "token_type": "Bearer", "expires_in": 3600,
		})
	}))
	t.Cleanup(srv.Close)
	dir := testenv.TempDir(t)
	state := mcpOAuthState{
		Version: 1, Resource: "https://example.invalid/mcp", Issuer: "https://example.invalid",
		ClientID: "c", ClientSecret: "s", TokenEndpoint: srv.URL, TokenEndpointAuthMethod: "client_secret_basic",
		AccessToken: access, RefreshToken: "r", TokenType: "Bearer", Expiry: expiry,
	}
	if err := saveMCPOAuthState(dir, state); err != nil {
		t.Fatal(err)
	}
	return &refreshFixture{dir: dir, calls: calls, state: state}
}

func (f *refreshFixture) client() *mcpOAuthClient {
	return &mcpOAuthClient{stateDir: f.dir, state: f.state, client: http.DefaultClient}
}

func (f *refreshFixture) ask(t *testing.T, c *mcpOAuthClient, force bool) string {
	t.Helper()
	header, used, err := c.authorizationHeader(context.Background(), force)
	if err != nil {
		t.Fatalf("authorizationHeader(force=%v): %v", force, err)
	}
	if !used {
		t.Fatalf("authorizationHeader(force=%v) sent no OAuth header", force)
	}
	return header
}

// Arm C — the ordinary path, which this must not disturb. A token nobody
// refused and the clock still likes is sent as it is.
func TestAnUnrefusedTokenIsNotRefreshed(t *testing.T) {
	f := newRefreshFixture(t, "current", time.Now().Add(time.Hour))
	if got := f.ask(t, f.client(), false); got != "Bearer current" {
		t.Errorf("header = %q, want the stored token", got)
	}
	if n := f.calls.Load(); n != 0 {
		t.Errorf("token endpoint calls = %d, want 0 for a request nobody refused", n)
	}
}

// Arm B — the concurrency this must keep. Another actor refreshed while we
// waited for the lock, so the canonical credential is no longer the refused
// one: use it, and do not ask the endpoint again. An implementation that reads
// forceRefresh as "always refresh" fails here, which is the point.
func TestATokenSomeoneElseAlreadyReplacedIsUsedRatherThanRefreshedAgain(t *testing.T) {
	f := newRefreshFixture(t, "refused", time.Now().Add(time.Hour))
	c := f.client()

	replaced := f.state
	replaced.AccessToken = "replaced-by-another-actor"
	if err := saveMCPOAuthState(f.dir, replaced); err != nil {
		t.Fatal(err)
	}

	if got := f.ask(t, c, true); got != "Bearer replaced-by-another-actor" {
		t.Errorf("header = %q, want the token the other actor stored", got)
	}
	if n := f.calls.Load(); n != 0 {
		t.Errorf("token endpoint calls = %d, want 0 — someone else had already refreshed", n)
	}
}

// Arm A — the one that is wrong today. The server refused the canonical token
// and nobody has replaced it, so how fresh the local clock finds it decides
// nothing. It is asserted here as it behaves, not as it should: when this
// fails, the recovery was fixed, and this becomes
// TestARefusedTokenThatIsStillCanonicalIsRefreshed.
func TestKnownGapARefusedTokenIsHandedBackUnchanged(t *testing.T) {
	f := newRefreshFixture(t, "refused", time.Now().Add(time.Hour))
	got := f.ask(t, f.client(), true)
	if got != "Bearer refused" || f.calls.Load() != 0 {
		t.Fatalf("header %q after %d token calls — the refusal now reaches the endpoint, so replace this with the contract",
			got, f.calls.Load())
	}
}

// And the reason a one-line fix will not do it: nothing tells the refresh which
// credential was refused, so the two arms above arrive identical. Arm B must
// keep its answer and arm A must change, and today they are one call with one
// argument. The signature is where that gets settled.
func TestTheRefreshIsNotToldWhichCredentialWasRefused(t *testing.T) {
	same := newRefreshFixture(t, "refused", time.Now().Add(time.Hour))
	other := newRefreshFixture(t, "refused", time.Now().Add(time.Hour))
	replaced := other.state
	replaced.AccessToken = "replaced-by-another-actor"
	if err := saveMCPOAuthState(other.dir, replaced); err != nil {
		t.Fatal(err)
	}

	// Two different situations, one indistinguishable request.
	same.ask(t, same.client(), true)
	other.ask(t, other.client(), true)
	if same.calls.Load() != other.calls.Load() {
		t.Fatalf("the two arms already differ (%d vs %d): the identity reached the refresh, so this note is done",
			same.calls.Load(), other.calls.Load())
	}
}

// The refused token has to come from the production chain, not from a flag a
// test set: a real endpoint answers 401 to the stored token and 200 to the
// refreshed one. When this fails the recovery landed, and it becomes the
// negative control the classification needs — a 401 the transport recovered
// from is never a failure.
func TestKnownGapA401IsRetriedWithTheTokenTheServerJustRefused(t *testing.T) {
	var sent []string
	refreshes := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			refreshes.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "granted", "token_type": "Bearer", "expires_in": 3600,
			})
		default:
			sent = append(sent, r.Header.Get("Authorization"))
			if r.Header.Get("Authorization") != "Bearer granted" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		}
	}))
	defer srv.Close()

	dir := testenv.TempDir(t)
	// Not expired: the clock has no objection, only the server does.
	if err := saveMCPOAuthState(dir, mcpOAuthState{
		Version: 1, Resource: srv.URL + "/mcp", Issuer: srv.URL,
		ClientID: "c", ClientSecret: "s", TokenEndpoint: srv.URL + "/token",
		TokenEndpointAuthMethod: "client_secret_basic",
		AccessToken:             "refused", RefreshToken: "r", TokenType: "Bearer",
		Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	transport, err := newHTTPTransport(Spec{Name: "remote", Type: "http", URL: srv.URL + "/mcp", StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()

	_, callErr := transport.call(context.Background(), "ping", nil)
	if callErr == nil || refreshes.Load() != 0 {
		t.Fatalf("the 401 reached the token endpoint (%d refreshes, err=%v): write the contract instead of this",
			refreshes.Load(), callErr)
	}
	if len(sent) != 2 || sent[0] != "Bearer refused" || sent[1] != "Bearer refused" {
		t.Fatalf("credentials sent = %v; the retry no longer reuses the refused token", sent)
	}
}
