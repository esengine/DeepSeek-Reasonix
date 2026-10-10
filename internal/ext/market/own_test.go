package market

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"reasonix/internal/ext/installsource"
)

type stubOwner struct {
	detail OwnDetail
	token  string
	err    error
}

func (s *stubOwner) Owned(_ context.Context, token, _ string) (OwnDetail, error) {
	s.token = token
	return s.detail, s.err
}

func (s *stubOwner) Submit(context.Context, string, string) (Package, error) { return Package{}, nil }

// ownFixture turns the market fixture's skill into the account's own package,
// pending review, carrying whatever hash the publisher claimed.
func ownFixture(t *testing.T, status, claimed string) (*fixture, *stubOwner) {
	t.Helper()
	f := newFixture(t)
	approved := f.reg.detail.Approved
	owner := &stubOwner{detail: OwnDetail{
		Package: Package{Kind: "skill", Slug: "acme/review-kit", Status: status, LatestVersion: "1.0.0"},
		Latest:  &Version{Version: "1.0.0", Source: approved.Source, ContentHash: claimed},
	}}
	f.svc.Owner = owner
	return f, owner
}

func digestOf(t *testing.T, out Outcome) string {
	t.Helper()
	var d string
	_ = json.Unmarshal(out.Fields["contentDigest"], &d)
	return d
}

func ownReq(slug, version, planID, digest string) Request {
	return Request{Slug: slug, Version: version, PlanID: planID, Digest: digest}
}

func TestOwnInstallPinsTheUnreviewedVersionToItsPreview(t *testing.T) {
	f, owner := ownFixture(t, "private", "sha256:"+strings.Repeat("0", 64))
	plan, err := f.svc.PlanOwn(context.Background(), "tok", ownReq("acme/review-kit", "", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Unreviewed || owner.token != "tok" {
		t.Fatalf("unreviewed = %v, token = %q", plan.Unreviewed, owner.token)
	}
	digest := digestOf(t, plan)
	if !installsource.IsContentDigest(digest) {
		t.Fatalf("preview digest = %q", digest)
	}
	out, err := f.svc.InstallOwn(context.Background(), "tok", ownReq("acme/review-kit", "1.0.0", planID(t, plan), digest))
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Fields["status"]) != `"done"` || !out.Unreviewed {
		t.Fatalf("status = %s unreviewed = %v", out.Fields["status"], out.Unreviewed)
	}
	rec := InstalledRecords(f.home)["acme/review-kit"]
	if !rec.Unreviewed || rec.ContentHash != digest {
		t.Fatalf("ledger = %+v", rec)
	}
}

// The preview is what the person confirmed; content that moves after it is
// refused exactly as content that moves after review is.
func TestOwnInstallRefusesContentThatChangedSincePreview(t *testing.T) {
	f, _ := ownFixture(t, "pending", "")
	plan, err := f.svc.PlanOwn(context.Background(), "tok", ownReq("acme/review-kit", "", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	f.body.Store("---\nname: review-kit\ndescription: reviews diffs\n---\nswapped after preview")
	_, err = f.svc.InstallOwn(context.Background(), "tok", ownReq("acme/review-kit", "1.0.0", planID(t, plan), digestOf(t, plan)))
	if !errors.Is(err, installsource.ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	if _, statErr := os.Stat(f.skill); !os.IsNotExist(statErr) {
		t.Fatal("changed content was written")
	}
}

func TestOwnInstallWithoutAPreviewedDigestInstallsNothing(t *testing.T) {
	for _, digest := range []string{"", "sha256:xyz"} {
		f, _ := ownFixture(t, "rejected", "")
		_, err := f.svc.InstallOwn(context.Background(), "tok", ownReq("acme/review-kit", "1.0.0", "low:sha256:x", digest))
		if !errors.Is(err, ErrUnpreviewed) {
			t.Fatalf("digest %q: err = %v, want ErrUnpreviewed", digest, err)
		}
		if _, statErr := os.Stat(f.skill); !os.IsNotExist(statErr) {
			t.Fatal("an unpreviewed install wrote the skill")
		}
	}
}

// A pending hash is only the publisher's claim, so a digest the window sends
// that matches it but not the content is still refused.
func TestOwnInstallIgnoresThePublishersClaimedHash(t *testing.T) {
	claimed := "sha256:" + strings.Repeat("1", 64)
	f, _ := ownFixture(t, "pending", claimed)
	plan, err := f.svc.PlanOwn(context.Background(), "tok", ownReq("acme/review-kit", "", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if digestOf(t, plan) == claimed {
		t.Fatal("the preview echoed the claim")
	}
	_, err = f.svc.InstallOwn(context.Background(), "tok", ownReq("acme/review-kit", "1.0.0", planID(t, plan), claimed))
	if !errors.Is(err, installsource.ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
}

// A live version already carries its reviewer's pin; a preview digest that
// differs from it means the version was approved after the person looked.
func TestOwnInstallOfALiveVersionKeepsTheReviewersPin(t *testing.T) {
	f := newFixture(t)
	reviewed := f.reg.detail.Approved.ContentHash
	f.svc.Owner = &stubOwner{detail: OwnDetail{
		Package: Package{Kind: "skill", Slug: "acme/review-kit", Status: "active", LatestVersion: "1.0.0"},
		Latest:  f.reg.detail.Approved,
	}}
	plan, err := f.svc.PlanOwn(context.Background(), "tok", ownReq("acme/review-kit", "", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Unreviewed {
		t.Fatal("a reviewed version read as unreviewed")
	}
	stale := ownReq("acme/review-kit", "1.0.0", planID(t, plan), "sha256:"+strings.Repeat("2", 64))
	if _, err := f.svc.InstallOwn(context.Background(), "tok", stale); !errors.Is(err, ErrVersionChanged) {
		t.Fatalf("a digest other than the reviewer's: err = %v", err)
	}
	out, err := f.svc.InstallOwn(context.Background(), "tok", ownReq("acme/review-kit", "1.0.0", planID(t, plan), ""))
	if err != nil {
		t.Fatal(err)
	}
	if rec := InstalledRecords(f.home)["acme/review-kit"]; rec.Unreviewed || rec.ContentHash != reviewed || out.Unreviewed {
		t.Fatalf("ledger = %+v", rec)
	}
}

func TestOwnInstallRefusesWhatTheRegistryWillNotVouchFor(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*stubOwner)
		want error
	}{
		{"not the account's", func(o *stubOwner) { o.err = ErrNotFound }, ErrNotFound},
		{"no version row", func(o *stubOwner) { o.detail.Latest = nil }, ErrBadSource},
		{"local path", func(o *stubOwner) { o.detail.Latest.Source = "/home/me/.ssh" }, ErrBadSource},
		{"theme on a branch", func(o *stubOwner) {
			o.detail.Package.Kind = "theme"
			o.detail.Latest.Source = "https://github.com/acme/themes/tree/main/dusk"
		}, ErrBadSource},
		{"newer version", func(o *stubOwner) { o.detail.Latest.Version = "1.1.0" }, ErrVersionChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, owner := ownFixture(t, "private", "")
			tc.mut(owner)
			if _, err := f.svc.PlanOwn(context.Background(), "tok", ownReq("acme/review-kit", "1.0.0", "", "")); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestOwnedReadsTheAccountsPackageWithItsToken(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/packages/me/dusk" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"package":{"kind":"theme","slug":"me/dusk","status":"private","latestVersion":"0.2.0"},
			"versions":[{"version":"0.2.0","source":"` + pinnedTree + `","content_hash":""},{"version":"0.1.0","source":"x"}]}`))
	})
	d, err := c.Owned(context.Background(), "tok", "me/dusk")
	if err != nil {
		t.Fatal(err)
	}
	if d.Package.Status != "private" || d.Latest == nil || d.Latest.Source != pinnedTree {
		t.Fatalf("detail = %+v latest = %+v", d.Package, d.Latest)
	}
}

func TestOwnedAndSubmitProjectTheRegistrysRefusals(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		call   func(*Client) error
		want   error
	}{
		{"someone else's", 404, `{"error":{"code":"not_found"}}`, func(c *Client) error { _, err := c.Owned(context.Background(), "tok", "bob/x"); return err }, ErrNotFound},
		{"another slug answered", 200, `{"package":{"slug":"bob/y"}}`, func(c *Client) error { _, err := c.Owned(context.Background(), "tok", "bob/x"); return err }, ErrNotFound},
		{"signed out", 401, `{}`, func(c *Client) error { _, err := c.Owned(context.Background(), "tok", "me/x"); return err }, ErrSignedOut},
		{"already submitted", 409, `{"error":{"code":"not_private"}}`, func(c *Client) error { _, err := c.Submit(context.Background(), "tok", "me/x"); return err }, ErrNotPrivate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			if err := tc.call(c); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	var hits int
	c := testClient(t, func(http.ResponseWriter, *http.Request) { hits++ })
	if _, err := c.Owned(context.Background(), "", "me/x"); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("no token: %v", err)
	}
	if _, err := c.Submit(context.Background(), "tok", "../x"); !errors.Is(err, ErrBadSlug) {
		t.Fatalf("bad slug: %v", err)
	}
	if hits != 0 {
		t.Fatalf("%d requests left the machine", hits)
	}
}

func TestSubmitPostsToTheOwnersSubmitRoute(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/me/packages/me/dusk/submit" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"package":{"slug":"me/dusk","status":"pending"}}`))
	})
	p, err := c.Submit(context.Background(), "tok", "me/dusk")
	if err != nil || p.Status != "pending" {
		t.Fatalf("p = %+v err = %v", p, err)
	}
}

func TestSubmissionVisibilityIsPublicOrPrivate(t *testing.T) {
	base := Submission{Kind: "theme", Name: "dusk", Source: pinnedTree}
	for v, ok := range map[string]bool{"": true, "public": true, " private ": true, "friends": false} {
		s := base
		s.Visibility = v
		got, err := s.Normalize()
		if (err == nil) != ok {
			t.Errorf("visibility %q: err = %v", v, err)
		}
		if ok && got.Visibility != map[string]string{"": "", "public": "", " private ": "private"}[v] {
			t.Errorf("visibility %q sent as %q", v, got.Visibility)
		}
	}
}
