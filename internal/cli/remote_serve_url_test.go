package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/serve"
)

func TestRemoteServeBrowserURLUsesFragmentForCurrentServe(t *testing.T) {
	ctrl := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(ctrl.Close)
	srv := serve.New(ctrl, serve.NewBroadcaster(), config.ServeConfig{AuthMode: "token", Token: "current secret/+"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bound := strings.TrimPrefix(ts.URL, "http://")
	got := remoteServeBrowserURL(context.Background(), bound, "current secret/+")
	want := ts.URL + "/#token=current+secret%2F%2B"
	if got != want {
		t.Fatalf("remoteServeBrowserURL() = %q, want %q", got, want)
	}
}

func TestRemoteServeBrowserURLFallsBackForReusedV1214ServeContract(t *testing.T) {
	const token = "legacy secret/+"
	// v1.21.4 token auth denies unauthenticated /auth/token requests and
	// bootstraps browser cookies only from the legacy query parameter.
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "reasonix_token", Value: token, Path: "/", HttpOnly: true})
		http.Redirect(w, r, "/", http.StatusFound)
	}))
	t.Cleanup(legacy.Close)

	bound := strings.TrimPrefix(legacy.URL, "http://")
	got := remoteServeBrowserURL(context.Background(), bound, token)
	want := legacy.URL + "/?token=legacy+secret%2F%2B"
	if got != want {
		t.Fatalf("remoteServeBrowserURL() = %q, want legacy query bootstrap %q", got, want)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(got)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("legacy bootstrap status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "reasonix_token" || !cookies[0].HttpOnly {
		t.Fatalf("legacy bootstrap cookies = %#v, want HttpOnly reasonix_token", cookies)
	}
}
