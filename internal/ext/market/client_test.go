package market

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return newClient(url.URL{Scheme: "https", Host: u.Host}, srv.Client())
}

func TestNewClientTalksToTheFixedHostOverHTTPS(t *testing.T) {
	c := NewClient(nil)
	if c.base.Scheme != "https" || c.base.Host != registryHost {
		t.Fatalf("base = %s", c.base.String())
	}
}

func TestDetailTakesTheLatestVersionRowAsApproved(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/packages/acme/review-kit" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"package":{"kind":"skill","slug":"acme/review-kit","status":"active","latestVersion":"1.1.0"},
		"versions":[{"version":"1.1.0","source":"https://example.test/SKILL.md","content_hash":"sha256:ab","risk_level":"low"},
		{"version":"1.0.0","source":"https://example.test/old.md","content_hash":"","risk_level":""}]}`))
	})
	d, err := c.Detail(context.Background(), "acme/review-kit")
	if err != nil {
		t.Fatal(err)
	}
	if d.Approved == nil || d.Approved.Version != "1.1.0" || d.Approved.ContentHash != "sha256:ab" {
		t.Fatalf("approved = %+v", d.Approved)
	}
}

// A row that is not the one asked for, or not live, is not an answer to use.
func TestDetailRefusesAMismatchedOrInactivePackage(t *testing.T) {
	for _, body := range []string{
		`{"package":{"slug":"other/pkg","status":"active","latestVersion":"1"},"versions":[]}`,
		`{"package":{"slug":"acme/kit","status":"pending","latestVersion":"1"},"versions":[]}`,
	} {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := c.Detail(context.Background(), "acme/kit"); !errors.Is(err, ErrNotFound) {
			t.Errorf("body %s: err = %v, want ErrNotFound", body, err)
		}
	}
}

func TestListDropsRowsThatAreNotActive(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "review" || r.URL.Query().Get("kind") != "skill" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"packages":[{"slug":"a/live","status":"active"},{"slug":"a/queued","status":"pending"}],"limit":24,"offset":0}`))
	})
	page, err := c.List(context.Background(), Query{Kind: "skill", Q: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Packages) != 1 || page.Packages[0].Slug != "a/live" {
		t.Fatalf("packages = %+v", page.Packages)
	}
}

func TestListAsksTheRegistryToFilterToInstallable(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pinned") != "1" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"packages":[{"slug":"a/kit","status":"active","pinned":true}],"limit":24,"offset":0}`))
	})
	page, err := c.List(context.Background(), Query{Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Packages) != 1 || page.Packages[0].Pinned == nil || !*page.Packages[0].Pinned {
		t.Fatalf("packages = %+v", page.Packages)
	}
}

// A registry that ignores the filter answers a page the person did not ask
// for; handing it on filtered here would leave the paging wrong.
func TestListRefusesAnUnfilteredAnswerToAFilteredAsk(t *testing.T) {
	for _, body := range []string{
		`{"packages":[{"slug":"a/old","status":"active"}]}`,
		`{"packages":[{"slug":"a/no","status":"active","pinned":false}]}`,
	} {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := c.List(context.Background(), Query{Pinned: true}); !errors.Is(err, ErrFilterUnsupported) {
			t.Errorf("body %s: err = %v, want ErrFilterUnsupported", body, err)
		}
	}
	plain := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("pinned") {
			t.Errorf("unfiltered ask sent pinned: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"packages":[{"slug":"a/old","status":"active"}]}`))
	})
	page, err := plain.List(context.Background(), Query{})
	if err != nil || len(page.Packages) != 1 || page.Packages[0].Pinned != nil {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

// A redirect could hand the listing to any host; it is an unexpected answer,
// never a hop.
func TestClientDoesNotFollowRedirects(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/v1/packages", http.StatusFound)
	})
	if _, err := c.List(context.Background(), Query{}); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

func TestClientCapsTheBody(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"packages":[{"summary":"` + strings.Repeat("x", maxListBody) + `"}]}`))
	})
	if _, err := c.List(context.Background(), Query{}); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

func TestClientSortsFailuresByCause(t *testing.T) {
	missing := testClient(t, func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	if _, err := missing.Detail(context.Background(), "a/b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("404: err = %v", err)
	}
	broken := testClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	if _, err := broken.Detail(context.Background(), "a/b"); !errors.Is(err, ErrBadResponse) {
		t.Errorf("502: err = %v", err)
	}
	down := newClient(url.URL{Scheme: "https", Host: "127.0.0.1:1"}, nil)
	if _, err := down.Detail(context.Background(), "a/b"); !errors.Is(err, ErrUnreachable) {
		t.Errorf("closed port: err = %v", err)
	}
}

func TestSplitSlugRefusesPathTricks(t *testing.T) {
	for _, s := range []string{"", "a", "a/b/c", "../b", "a/..", "a/b?x=1", "a/%2e%2e", `a\b/c`} {
		if _, _, err := SplitSlug(s); !errors.Is(err, ErrBadSlug) {
			t.Errorf("SplitSlug(%q) accepted", s)
		}
	}
	if h, n, err := SplitSlug("1374665203/reasonix-computer-use"); err != nil || h != "1374665203" || n != "reasonix-computer-use" {
		t.Errorf("real slug refused: %v", err)
	}
}
