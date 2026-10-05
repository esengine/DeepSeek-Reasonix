package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/account"
)

// AllowAccountAuth grants the /account routes. Off until a host asks: signing
// in stores a token in the credential store of the machine running the kernel,
// so a server reachable over the network must not let a client mint one. The
// desktop shell asks because its only client is its own window.
func (s *Server) AllowAccountAuth() { s.grants.accountAuth = true }

// accountClient builds the identity client with the user's configured proxy —
// a machine that needs one to reach a model needs one to reach id.reasonix.io.
func (s *Server) accountClient() (*account.Client, error) {
	spec := netclient.ProxySpec{Mode: netclient.ModeAuto}
	if cfg, err := config.Load(); err == nil && cfg != nil {
		spec = cfg.NetworkProxySpec()
	}
	httpClient, err := netclient.NewHTTPClient(spec, netclient.TransportOptions{})
	if err != nil {
		return nil, err
	}
	return account.New(accountsBaseURL(), "reasonix-serve", httpClient), nil
}

func accountsBaseURL() string { return strings.TrimSpace(os.Getenv("REASONIX_ACCOUNTS_URL")) }

// account reports who is signed in. Signed out is a normal answer, not an
// error, and an unreachable identity service is reported as itself so the UI
// never tells a signed-in user to sign in again because a worker was down.
func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	if !s.accountAuthAllowed(w, r) {
		return
	}
	token := account.Token()
	if token == "" {
		writeJSON(w, map[string]any{"signedIn": false})
		return
	}
	client, err := s.accountClient()
	if err != nil {
		writeJSON(w, map[string]any{"signedIn": true, "error": err.Error()})
		return
	}
	user, err := client.Me(r.Context(), token)
	if errors.Is(err, account.ErrUnauthorized) {
		writeJSON(w, map[string]any{"signedIn": false, "expired": true})
		return
	}
	if err != nil {
		writeJSON(w, map[string]any{"signedIn": true, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"signedIn": true, "user": accountUserJSON(user)})
}

// accountLogin opens a device sign-in. The device code goes back to the caller
// exactly as it does for the CLI: it is the client's half of the exchange and
// is worthless without the human approving the user code in a browser.
func (s *Server) accountLogin(w http.ResponseWriter, r *http.Request) {
	if !s.accountAuthAllowed(w, r) {
		return
	}
	client, err := s.accountClient()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	grant, err := client.StartDevice(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, map[string]any{
		"deviceCode":              grant.DeviceCode,
		"userCode":                grant.UserCode,
		"verificationUri":         grant.VerificationURI,
		"verificationUriComplete": grant.VerificationURIComplete,
		"interval":                grant.Interval,
		"expiresIn":               grant.ExpiresIn,
	})
}

// accountPoll answers one poll. The frontend drives the loop so it can show a
// live "waiting" state and cancel; the kernel only stores the token that ends it.
func (s *Server) accountPoll(w http.ResponseWriter, r *http.Request) {
	if !s.accountAuthAllowed(w, r) {
		return
	}
	var req struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DeviceCode) == "" {
		missingField(w, "deviceCode")
		return
	}
	client, err := s.accountClient()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	token, user, err := client.PollDevice(r.Context(), req.DeviceCode)
	switch {
	case errors.Is(err, account.ErrPending), errors.Is(err, account.ErrSlowDown):
		writeJSON(w, map[string]any{"status": "pending", "slowDown": errors.Is(err, account.ErrSlowDown)})
		return
	case err != nil:
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if _, err := account.SaveToken(token); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"status": "complete", "user": accountUserJSON(user)})
}

// accountLogout revokes the session and forgets the token. Revoking remotely is
// best effort: an offline machine must still be able to sign out.
func (s *Server) accountLogout(w http.ResponseWriter, r *http.Request) {
	if !s.accountAuthAllowed(w, r) {
		return
	}
	if token := account.Token(); token != "" {
		if client, err := s.accountClient(); err == nil {
			_ = client.Logout(r.Context(), token)
		}
	}
	if err := account.ClearToken(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) accountAuthAllowed(w http.ResponseWriter, r *http.Request) bool {
	if s.grants.at(r).accountAuth {
		return true
	}
	refuse(w, http.StatusForbidden, "account.signin_disabled", "account sign-in is not enabled for this server", nil)
	return false
}

// accountUserJSON is the shape a frontend may show: a name and an email. Role
// and status are the dashboard's gate, and a user reading "pending" here would
// only get a question we do not answer.
func accountUserJSON(u *account.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{"handle": u.Handle, "email": u.Email, "label": u.Label()}
}
