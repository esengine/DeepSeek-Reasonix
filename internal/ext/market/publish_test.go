package market

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

var pinnedTree = "https://github.com/acme/themes/tree/" + strings.Repeat("a", 40) + "/dusk"

func TestPublishSendsTheSubmissionWithTheTokenToTheRegistry(t *testing.T) {
	var got Submission
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/packages" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"package":{"kind":"theme","slug":"me/dusk","status":"pending"},"created":true,"version":"0.1.0"}`))
	})
	out, err := c.Publish(context.Background(), " tok ", Submission{
		Kind: "theme", Name: " Dusk ", Source: " " + pinnedTree + " ", Tags: []string{" dark ", "", "calm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Created || out.Package.Status != "pending" || out.Version != "0.1.0" {
		t.Fatalf("out = %+v", out)
	}
	if got.Kind != "theme" || got.Name != "dusk" || got.Source != pinnedTree || strings.Join(got.Tags, ",") != "dark,calm" {
		t.Fatalf("sent = %+v", got)
	}
}

// Nothing leaves the machine for a submission that could never be installed,
// or when there is no session to send.
func TestPublishRefusesLocallyWithoutARequest(t *testing.T) {
	var hits atomic.Int32
	c := testClient(t, func(http.ResponseWriter, *http.Request) { hits.Add(1) })
	cases := []struct {
		name  string
		token string
		sub   Submission
		want  error
	}{
		{"no token", "", Submission{Kind: "theme", Name: "dusk", Source: pinnedTree}, ErrSignedOut},
		{"theme on a branch", "tok", Submission{Kind: "theme", Name: "dusk", Source: "https://github.com/acme/themes/tree/main/dusk"}, ErrUnpublishable},
		{"plugin repo root", "tok", Submission{Kind: "plugin", Name: "kit", Source: "https://github.com/acme/kit"}, ErrUnpublishable},
		{"skill on a local path", "tok", Submission{Kind: "skill", Name: "s", Source: "/home/me/skill"}, ErrUnpublishable},
		{"unknown kind", "tok", Submission{Kind: "auto", Name: "s", Source: pinnedTree}, ErrUnpublishable},
		{"bad name", "tok", Submission{Kind: "theme", Name: "../x", Source: pinnedTree}, ErrRejected},
		{"too many tags", "tok", Submission{Kind: "theme", Name: "dusk", Source: pinnedTree, Tags: strings.Split("a,b,c,d,e,f,g,h,i", ",")}, ErrRejected},
	}
	for _, tc := range cases {
		if _, err := c.Publish(context.Background(), tc.token, tc.sub); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests reached the registry", hits.Load())
	}
}

func TestPublishProjectsTheRegistrysRefusalCode(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{401, `{"error":{"code":"unauthorized","message":"Sign in"}}`, ErrSignedOut},
		{403, `{"error":{"code":"email_unverified","message":"Verify"}}`, ErrEmailUnverified},
		{403, `{"error":{"code":"not_owner","message":"taken"}}`, ErrNotOwner},
		{409, `{"error":{"code":"version_exists","message":"exists"}}`, ErrVersionExists},
		{422, `{"error":{"code":"invalid_input","message":"summary: too long"}}`, ErrRejected},
		{429, `{"error":{"code":"rate_limited","message":"wait"}}`, ErrRateLimited},
		{500, `{"error":{"code":"internal","message":"x"}}`, ErrBadResponse},
		{503, `{"error":{"code":"accounts_unavailable","message":"x"}}`, ErrUnreachable},
	}
	for _, tc := range cases {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		_, err := c.Publish(context.Background(), "tok", Submission{Kind: "theme", Name: "dusk", Source: pinnedTree})
		if !errors.Is(err, tc.want) {
			t.Errorf("%d: err = %v, want %v", tc.status, err, tc.want)
		}
	}
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_input","message":"summary: too long"}}`))
	})
	_, err := c.Publish(context.Background(), "tok", Submission{Kind: "theme", Name: "dusk", Source: pinnedTree})
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Message != "summary: too long" {
		t.Fatalf("err = %v", err)
	}
}

// A redirect is where a bearer token would leave the registry; it is never
// followed, so the token never reaches the second host.
func TestPublishDoesNotCarryTheTokenAcrossARedirect(t *testing.T) {
	var leaked atomic.Int32
	elsewhere := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Add(1) }))
	t.Cleanup(elsewhere.Close)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/v1/packages", http.StatusTemporaryRedirect)
	})
	if _, err := c.Publish(context.Background(), "tok", Submission{Kind: "theme", Name: "dusk", Source: pinnedTree}); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
	if _, err := c.Mine(context.Background(), "tok"); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("mine err = %v, want ErrBadResponse", err)
	}
	if leaked.Load() != 0 {
		t.Fatal("the redirect target was contacted")
	}
}

func TestMineListsEveryReviewState(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/me/packages" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("%s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"packages":[{"slug":"me/a","status":"pending"},{"slug":"me/b","status":"rejected"},{"slug":"me/c","status":"active"}]}`))
	})
	got, err := c.Mine(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[1].Status != "rejected" {
		t.Fatalf("mine = %+v", got)
	}
	if _, err := c.Mine(context.Background(), ""); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("no token: err = %v", err)
	}
}

func TestThemesOnlyAdmitsAPackageThatOnlyCarriesThemes(t *testing.T) {
	for raw, want := range map[string]bool{
		`[{"kind":"plugin","themeCount":2}]`:                   true,
		`[{"kind":"plugin","themeCount":1,"runtime":null}]`:    true,
		`[{"kind":"plugin","themeCount":1,"hookCount":1}]`:     false,
		`[{"kind":"plugin","themeCount":1,"toolCount":1}]`:     false,
		`[{"kind":"plugin","themeCount":1,"skillCount":1}]`:    false,
		`[{"kind":"plugin","themeCount":1,"runtime":{"x":1}}]`: false,
		`[{"kind":"plugin","themeCount":0}]`:                   false,
		`[{"kind":"skill","themeCount":1}]`:                    false,
		`[]`:                                                   false,
		`not json`:                                             false,
	} {
		if themesOnly(json.RawMessage(raw)) != want {
			t.Errorf("themesOnly(%s) = %v", raw, !want)
		}
	}
	if Installer("theme") != "plugin" || Installer("skill") != "skill" {
		t.Fatal("a theme must install through the plugin installer")
	}
}
