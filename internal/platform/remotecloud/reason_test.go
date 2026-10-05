package remotecloud

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"reasonix/internal/platform/account"
)

func TestOfflineReasonReadsTheErrorsIdentity(t *testing.T) {
	refusedDial := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"expired sign-in", fmt.Errorf("remote cloud: %w", account.ErrUnauthorized), ReasonSignedOut},
		{"relay handshake refused", websocket.ErrBadHandshake, ReasonRefused},
		{"account service refuses the request", &account.Error{Status: 403, Code: "forbidden"}, ReasonRefused},
		{"account service throttles", &account.Error{Status: 429, Code: "rate_limited"}, ReasonUnavailable},
		{"account service is down", fmt.Errorf("remote cloud: %w", &account.Error{Status: 503}), ReasonUnavailable},
		{"relay cannot be dialled", refusedDial, ReasonUnreachable},
		{"account service cannot be reached", &url.Error{Op: "Get", URL: "https://accounts.example/me", Err: refusedDial}, ReasonUnreachable},
		{"name does not resolve", &net.DNSError{Err: "no such host", Name: "remote.example", IsNotFound: true}, ReasonUnreachable},
		{"unclassified", errors.New("account: identity service returned no user"), ""},
		// A sentence that names a cause is still not that cause.
		{"wording is not identity", errors.New("connection refused: not signed in"), ""},
	} {
		if got := offlineReason(tc.err); got != tc.want {
			t.Errorf("%s: offlineReason(%v) = %q, want %q", tc.name, tc.err, got, tc.want)
		}
	}
}

// The relay's handshake status decides the class: a throttled or failing relay
// is not one that refused this computer.
func TestRelayHandshakeIsClassifiedByItsStatus(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusForbidden, ReasonRefused},
		{http.StatusTooManyRequests, ReasonUnavailable},
		{http.StatusBadGateway, ReasonUnavailable},
	} {
		relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		host := New(nil, nil, "ws"+strings.TrimPrefix(relay.URL, "http"), "test")
		err := host.connect(context.Background(), "token", &identity{DeviceID: "d"}, nil)
		relay.Close()
		if got := offlineReason(err); got != tc.want {
			t.Errorf("relay answered %d: offlineReason(%v) = %q, want %q", tc.status, err, got, tc.want)
		}
	}
}

func TestSignedOutHostPublishesTheSignedOutReason(t *testing.T) {
	host := New(nil, nil, "", "test")
	host.token = func() string { return "" }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { host.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	for range 200 {
		if host.Status().Reason == ReasonSignedOut {
			if host.Status().Online {
				t.Fatal("a signed-out host reports itself online")
			}
			return
		}
		select {
		case <-done:
			t.Fatal("Run returned before publishing a status")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("status = %+v, want reason %q", host.Status(), ReasonSignedOut)
}
