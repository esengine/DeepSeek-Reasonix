package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The shapes below are transcribed from workers/accounts/src/routes/device.ts
// and me.ts. They are camelCase and the errors are {error:{code,message}} —
// guessing either is how a client 500s against a service that is working.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, "reasonix-cli/test", srv.Client())
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatal(err)
	}
}

func apiError(t *testing.T, w http.ResponseWriter, status int, code, message string) {
	t.Helper()
	writeJSON(t, w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func TestStartDeviceReadsTheGrant(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/device/start" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, 200, map[string]any{
			"deviceCode":              "dc-1",
			"userCode":                "WDJB-MJHT",
			"verificationUri":         "https://id.reasonix.io/device",
			"verificationUriComplete": "https://id.reasonix.io/device?code=WDJB-MJHT",
			"interval":                5,
			"expiresIn":               900,
		})
	})
	grant, err := c.StartDevice(context.Background())
	if err != nil {
		t.Fatalf("StartDevice: %v", err)
	}
	if grant.DeviceCode != "dc-1" || grant.UserCode != "WDJB-MJHT" {
		t.Errorf("grant = %+v, want the server's codes", grant)
	}
	if grant.VerificationURI != "https://id.reasonix.io/device" || grant.Interval != 5 {
		t.Errorf("grant = %+v, want the server's uri and interval", grant)
	}
}

func TestPollDeviceMapsEveryOutcome(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   any
		want   error
	}{
		{"pending", 200, map[string]any{"status": "authorization_pending", "interval": 5}, ErrPending},
		{"slow down", 200, map[string]any{"status": "slow_down", "interval": 5}, ErrSlowDown},
		{"denied", 403, map[string]any{"error": map[string]string{"code": "access_denied", "message": "denied"}}, ErrDenied},
		{"expired", 410, map[string]any{"error": map[string]string{"code": "expired_token", "message": "expired"}}, ErrExpired},
		{"unknown code", 400, map[string]any{"error": map[string]string{"code": "invalid_grant", "message": "unknown"}}, ErrInvalidGrant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tc.status, tc.body)
			})
			if _, _, err := c.PollDevice(context.Background(), "dc-1"); !errors.Is(err, tc.want) {
				t.Errorf("PollDevice error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPollDeviceReturnsTheSession(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DeviceCode string `json:"deviceCode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.DeviceCode != "dc-1" {
			t.Errorf("poll sent deviceCode %q", body.DeviceCode)
		}
		writeJSON(t, w, 200, map[string]any{
			"status":       "complete",
			"sessionToken": "sess-abc",
			"user":         map[string]any{"handle": "yhh", "email": "a@b.c", "displayName": "YHH"},
		})
	})
	token, user, err := c.PollDevice(context.Background(), "dc-1")
	if err != nil {
		t.Fatalf("PollDevice: %v", err)
	}
	if token != "sess-abc" {
		t.Errorf("token = %q, want sess-abc", token)
	}
	if user == nil || user.Label() != "YHH" || user.Email != "a@b.c" {
		t.Errorf("user = %+v, want the server's identity", user)
	}
}

func TestMeSendsBearerAndSeparatesSignedOutFromDown(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sess-abc" {
			t.Errorf("Authorization = %q, want a Bearer token", got)
		}
		writeJSON(t, w, 200, map[string]any{"user": map[string]any{"handle": "yhh", "email": "a@b.c"}})
	})
	user, err := c.Me(context.Background(), "sess-abc")
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if user.Label() != "yhh" {
		t.Errorf("label = %q, want the handle when there is no display name", user.Label())
	}

	rejected := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		apiError(t, w, 401, "unauthorized", "Sign in first.")
	})
	if _, err := rejected.Me(context.Background(), "stale"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("rejected token error = %v, want ErrUnauthorized", err)
	}

	down := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		apiError(t, w, 500, "internal", "Something went wrong.")
	})
	_, err = down.Me(context.Background(), "sess-abc")
	if errors.Is(err, ErrUnauthorized) {
		t.Error("a 500 must not read as signed out — the user would sign in again and fail again")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 500 {
		t.Errorf("error = %v, want the server's 500 preserved", err)
	}
}

// Signing out has to work against a server that already forgot the session,
// or a stale token could never be cleared.
func TestLogoutTreatsAnAlreadyDeadSessionAsDone(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		apiError(t, w, 401, "unauthorized", "no session")
	})
	if err := c.Logout(context.Background(), "stale"); err != nil {
		t.Errorf("Logout on a dead session = %v, want nil", err)
	}
}

func TestWaitForApprovalStopsWhenCancelled(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]any{"status": "authorization_pending", "interval": 1})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := c.WaitForApproval(ctx, &DeviceGrant{DeviceCode: "dc-1", Interval: 1, ExpiresIn: 900})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled wait = %v, want context.Canceled", err)
	}
}

func TestClientWithoutAUserAgentFallsBackToTheClientIdentity(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		writeJSON(t, w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()
	_ = New(srv.URL, "", srv.Client()).do(context.Background(), http.MethodGet, "/x", "", nil, nil)
	if got == "" || strings.HasPrefix(got, "Go-http-client") {
		t.Fatalf("User-Agent = %q", got)
	}
}
