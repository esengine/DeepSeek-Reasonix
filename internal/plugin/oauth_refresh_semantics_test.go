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

func (f *refreshFixture) ask(t *testing.T, c *mcpOAuthClient) string {
	t.Helper()
	header, used, err := c.authorizationHeader(context.Background())
	return checked(t, header, used, err)
}

func (f *refreshFixture) askAfterReject(t *testing.T, c *mcpOAuthClient, rejected string) string {
	t.Helper()
	header, used, err := c.authorizationHeaderAfterReject(context.Background(), rejected)
	return checked(t, header, used, err)
}

func checked(t *testing.T, header string, used bool, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("authorization: %v", err)
	}
	if !used {
		t.Fatal("no OAuth header was produced")
	}
	return header
}

// Arm C — the ordinary path, which this must not disturb. A token nobody
// refused and the clock still likes is sent as it is.
func TestAnUnrefusedTokenIsNotRefreshed(t *testing.T) {
	f := newRefreshFixture(t, "current", time.Now().Add(time.Hour))
	if got := f.ask(t, f.client()); got != "Bearer current" {
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

	if got := f.askAfterReject(t, c, "refused"); got != "Bearer replaced-by-another-actor" {
		t.Errorf("header = %q, want the token the other actor stored", got)
	}
	if n := f.calls.Load(); n != 0 {
		t.Errorf("token endpoint calls = %d, want 0 — someone else had already refreshed", n)
	}
}

// Arm A — a server's refusal outranks this machine's opinion. The refused token
// is still the canonical one and the clock likes it for another hour, and none
// of that survives the endpoint having said no.
func TestARefusedTokenThatIsStillCanonicalIsRefreshed(t *testing.T) {
	f := newRefreshFixture(t, "refused", time.Now().Add(time.Hour))
	if got := f.askAfterReject(t, f.client(), "refused"); got != "Bearer granted" {
		t.Errorf("header = %q, want the refreshed token", got)
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("token endpoint calls = %d, want exactly 1", n)
	}
}

// Arm B2 — the identity test must not swallow the freshness one. A credential
// that is merely different from the refused one is not thereby a credential
// worth sending.
func TestAReplacementThatIsItselfExpiredIsNotUsed(t *testing.T) {
	f := newRefreshFixture(t, "refused", time.Now().Add(time.Hour))
	c := f.client()

	stale := f.state
	stale.AccessToken = "replaced-but-already-expired"
	stale.Expiry = time.Now().Add(-time.Minute)
	if err := saveMCPOAuthState(f.dir, stale); err != nil {
		t.Fatal(err)
	}

	if got := f.askAfterReject(t, c, "refused"); got != "Bearer granted" {
		t.Errorf("header = %q, want a refreshed token rather than an expired replacement", got)
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("token endpoint calls = %d, want exactly 1", n)
	}
}

// The rejection has to name what actually went out. Asking about a credential
// nobody sent is the shape a caller falls into by re-reading the store after
// the refusal, and it answers the wrong question.
func TestTheRejectionNamesTheCredentialThatWasSent(t *testing.T) {
	f := newRefreshFixture(t, "current", time.Now().Add(time.Hour))
	// Someone else's rejected token: what we hold was never refused, so it
	// stands, and the endpoint hears nothing.
	if got := f.askAfterReject(t, f.client(), "a-token-this-client-never-sent"); got != "Bearer current" {
		t.Errorf("header = %q, want the credential this client holds", got)
	}
	if n := f.calls.Load(); n != 0 {
		t.Errorf("token endpoint calls = %d, want 0", n)
	}
}

// Arm D — the whole thing through the transport that does it for real. A server
// refuses the stored credential, the refresh answers with another, and the
// retry carries the new one. Two requests, two different credentials, and no
// failure for anyone downstream to classify: a 401 the transport recovered
// from never becomes one.
func TestA401TheTransportRecoveredFromIsNotAFailure(t *testing.T) {
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

	if _, err := transport.call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("a 401 the refresh should have fixed still surfaced: %v", err)
	}
	if n := refreshes.Load(); n != 1 {
		t.Errorf("refreshes = %d, want exactly 1", n)
	}
	if len(sent) != 2 || sent[0] != "Bearer refused" || sent[1] != "Bearer granted" {
		t.Errorf("credentials sent = %v, want the refused one then the granted one", sent)
	}
}

// The ordinary request must not have become more expensive. This is a fix to
// the rejection path, and a proactive ask still touches no endpoint and leaves
// the store as it found it.
func TestAnOrdinaryRequestStillCostsNoTokenTraffic(t *testing.T) {
	f := newRefreshFixture(t, "current", time.Now().Add(time.Hour))
	before, err := loadMCPOAuthState(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.ask(t, f.client()); got != "Bearer current" {
		t.Errorf("header = %q", got)
	}
	after, err := loadMCPOAuthState(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 0 {
		t.Errorf("token endpoint calls = %d, want 0", f.calls.Load())
	}
	if after.AccessToken != before.AccessToken || !after.Expiry.Equal(before.Expiry) {
		t.Errorf("the store moved: %+v -> %+v", before, after)
	}
}
