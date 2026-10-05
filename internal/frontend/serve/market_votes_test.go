package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/market"
)

type fakeVoting struct {
	token, slug string
	value       int
	vote        market.Vote
	err         error
}

func (f *fakeVoting) MyVote(_ context.Context, token, slug string) (market.Vote, error) {
	f.token, f.slug = token, slug
	return f.vote, f.err
}

func (f *fakeVoting) Vote(_ context.Context, token, slug string, value int) (market.Vote, error) {
	f.token, f.slug, f.value = token, slug, value
	return f.vote, f.err
}

func withVoting(t *testing.T, v market.Voting) {
	t.Helper()
	old := marketVoting
	marketVoting = func(*http.Client) market.Voting { return v }
	t.Cleanup(func() { marketVoting = old })
}

func voteServer(t *testing.T, granted bool) (*Server, string) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	s := New(&pluginCtl{root: testenv.TempDir(t)}, NewBroadcaster(), config.ServeConfig{})
	if granted {
		s.AllowAccountAuth()
	}
	srv := httptest.NewServer(operatorHandler(s))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

// The token lives in this machine's credential store; a server on the network
// must not spend it, so voting is shut exactly where signing in is.
func TestMarketVoteRoutesNeedTheAccountGrant(t *testing.T) {
	t.Setenv("REASONIX_ACCOUNT_TOKEN", "tok")
	withVoting(t, &fakeVoting{})
	_, base := voteServer(t, false)
	resp := postJSON(t, base+"/market/packages/acme/kit/vote", map[string]any{"value": 1})
	if resp.StatusCode != http.StatusForbidden || marketCode(t, resp) != "account.signin_disabled" {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMarketVoteSignedOutIsAnAnswerForReadsAndARefusalForWrites(t *testing.T) {
	t.Setenv("REASONIX_ACCOUNT_TOKEN", "")
	fake := &fakeVoting{}
	withVoting(t, fake)
	_, base := voteServer(t, true)
	resp, err := http.Get(base + "/market/packages/acme/kit/vote")
	if err != nil {
		t.Fatal(err)
	}
	var mine map[string]any
	voteBody(t, resp, &mine)
	if mine["signedIn"] != false {
		t.Fatalf("read = %v", mine)
	}
	resp = postJSON(t, base+"/market/packages/acme/kit/vote", map[string]any{"value": 1})
	if resp.StatusCode != http.StatusUnauthorized || marketCode(t, resp) != "market.signed_out" {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if fake.token != "" {
		t.Fatal("the registry was asked without a token")
	}
}

func TestMarketVotePassesTheStoredTokenAndValue(t *testing.T) {
	t.Setenv("REASONIX_ACCOUNT_TOKEN", "tok")
	rate := 0.5
	fake := &fakeVoting{vote: market.Vote{Value: -1, UpCount: 1, DownCount: 1, ApprovalRate: &rate}}
	withVoting(t, fake)
	_, base := voteServer(t, true)
	resp := postJSON(t, base+"/market/packages/acme/kit/vote", map[string]any{"value": -1})
	var got map[string]any
	voteBody(t, resp, &got)
	if fake.token != "tok" || fake.slug != "acme/kit" || fake.value != -1 {
		t.Fatalf("registry saw %+v", fake)
	}
	if got["value"] != float64(-1) || got["approvalRate"] != 0.5 || got["canVote"] != true {
		t.Fatalf("answer = %v", got)
	}
	if resp := postJSON(t, base+"/market/packages/acme/kit/vote", map[string]any{}); marketCode(t, resp) != codeMissingField {
		t.Fatal("a vote without a value was accepted")
	}
}

func TestMarketVoteRefusalsCarryTheirCause(t *testing.T) {
	t.Setenv("REASONIX_ACCOUNT_TOKEN", "tok")
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{market.ErrOwnPackage, http.StatusForbidden, "market.own_package"},
		{market.ErrEmailUnverified, http.StatusForbidden, "market.email_unverified"},
		{market.ErrSignedOut, http.StatusUnauthorized, "market.signed_out"},
		{market.ErrBadVote, http.StatusBadRequest, "market.bad_vote"},
		{market.ErrRateLimited, http.StatusTooManyRequests, "market.rate_limited"},
	} {
		withVoting(t, &fakeVoting{err: tc.err})
		_, base := voteServer(t, true)
		resp := postJSON(t, base+"/market/packages/acme/kit/vote", map[string]any{"value": 1})
		if resp.StatusCode != tc.status {
			t.Errorf("%v: status = %d", tc.err, resp.StatusCode)
		}
		if code := marketCode(t, resp); code != tc.code {
			t.Errorf("%v: code = %q", tc.err, code)
		}
	}
}

func clearOptOuts(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DO_NOT_TRACK", "REASONIX_TELEMETRY", "CI", "CONTINUOUS_INTEGRATION", "GITHUB_ACTIONS", "GITLAB_CI",
		"BUILDKITE", "CIRCLECI", "JENKINS_URL", "TEAMCITY_VERSION", "TF_BUILD"} {
		t.Setenv(k, "")
	}
}

// An install report goes out only when the host allows it, both desktop
// statistics switches are on, and nothing in the environment opts out.
func TestMarketInstallKeyFollowsTheStatisticsConsent(t *testing.T) {
	clearOptOuts(t)
	s, _ := voteServer(t, true)
	req := httptest.NewRequest(http.MethodPost, "/market/install", nil)
	if s.marketInstallKey(req) != "" {
		t.Fatal("reported without the host grant")
	}
	s.AllowMarketInstallReport()
	key := s.marketInstallKey(req)
	if len(key) != 32 {
		t.Fatalf("key = %q with every switch on", key)
	}
	raw, err := os.ReadFile(filepath.Join(config.ReasonixHomeDir(), "market", "install-id"))
	if err != nil || key != strings.TrimSpace(string(raw)) {
		t.Fatalf("key is not the market's own stored id (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(config.ReasonixHomeDir(), "cli-telemetry-install-id")); !os.IsNotExist(err) {
		t.Fatal("reporting created the usage-statistics id")
	}
	for _, off := range []string{"telemetry = false", "metrics = false"} {
		if err := os.WriteFile(config.UserConfigPath(), []byte("[desktop]\n"+off+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if s.marketInstallKey(req) != "" {
			t.Errorf("reported with %s", off)
		}
	}
	if err := os.Remove(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DO_NOT_TRACK", "1")
	if s.marketInstallKey(req) != "" {
		t.Fatal("reported under DO_NOT_TRACK")
	}
}

func voteBody(t *testing.T, resp *http.Response, into any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}
