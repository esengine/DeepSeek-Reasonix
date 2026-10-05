package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/market"
)

type fakePublisher struct {
	token     string
	sub       market.Submission
	err       error
	owned     *market.OwnDetail
	submitted string
}

func (f *fakePublisher) Publish(_ context.Context, token string, s market.Submission) (market.Published, error) {
	f.token, f.sub = token, s
	if f.err != nil {
		return market.Published{}, f.err
	}
	return market.Published{Package: market.Package{Slug: "me/" + s.Name, Kind: s.Kind, Status: "pending"}, Created: true, Version: "0.1.0"}, nil
}

func (f *fakePublisher) Mine(_ context.Context, token string) ([]market.Package, error) {
	f.token = token
	return []market.Package{{Slug: "me/dusk", Kind: "theme", Status: "pending"}}, f.err
}

func (f *fakePublisher) Owned(_ context.Context, token, slug string) (market.OwnDetail, error) {
	f.token = token
	if f.owned == nil {
		return market.OwnDetail{}, market.ErrNotFound
	}
	return *f.owned, f.err
}

func (f *fakePublisher) Submit(_ context.Context, token, slug string) (market.Package, error) {
	f.token, f.submitted = token, slug
	if f.err != nil {
		return market.Package{}, f.err
	}
	return market.Package{Slug: slug, Status: "pending"}, nil
}

func publishServer(t *testing.T, granted bool, token string, pub *fakePublisher) string {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "home"))
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("REASONIX_ACCOUNT_TOKEN", token)
	old := marketPublisher
	marketPublisher = func(*http.Client) market.Publisher { return pub }
	t.Cleanup(func() { marketPublisher = old })
	return accountServer(t, granted).URL
}

func postPublish(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

var themeSubmission = map[string]any{"kind": "theme", "name": "dusk", "source": "https://github.com/me/t/tree/" + fmt.Sprintf("%040d", 0)}

// Publishing spends the account's session, so it is shut wherever sign-in is.
func TestMarketPublishNeedsTheAccountGrantAndASession(t *testing.T) {
	pub := &fakePublisher{}
	base := publishServer(t, false, "tok", pub)
	if code := marketCode(t, postPublish(t, base+"/market/publish", themeSubmission)); code != "account.signin_disabled" {
		t.Fatalf("without the grant = %q", code)
	}
	resp, err := http.Get(base + "/market/mine")
	if err != nil {
		t.Fatal(err)
	}
	if code := marketCode(t, resp); code != "account.signin_disabled" {
		t.Fatalf("mine without the grant = %q", code)
	}
	base = publishServer(t, true, "", pub)
	if code := marketCode(t, postPublish(t, base+"/market/publish", themeSubmission)); code != "market.signed_out" {
		t.Fatalf("signed out = %q", code)
	}
	if pub.token != "" {
		t.Fatal("the publisher was reached without a session")
	}
}

func TestMarketPublishHandsTheSessionToThePublisher(t *testing.T) {
	pub := &fakePublisher{}
	base := publishServer(t, true, "tok", pub)
	resp := postPublish(t, base+"/market/publish", themeSubmission)
	defer resp.Body.Close()
	var out market.Published
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || out.Package.Status != "pending" || pub.token != "tok" || pub.sub.Kind != "theme" {
		t.Fatalf("status=%d out=%+v token=%q sub=%+v", resp.StatusCode, out, pub.token, pub.sub)
	}
	mine, err := http.Get(base + "/market/mine")
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Body.Close()
	var list struct {
		Packages []market.Package `json:"packages"`
	}
	_ = json.NewDecoder(mine.Body).Decode(&list)
	if len(list.Packages) != 1 || list.Packages[0].Status != "pending" {
		t.Fatalf("mine = %+v", list)
	}
}

func TestMarketPublishRefusalsCarryTheirCause(t *testing.T) {
	for err, want := range map[error]string{
		market.ErrSignedOut:                                "market.signed_out",
		market.ErrEmailUnverified:                          "market.email_unverified",
		market.ErrNotOwner:                                 "market.not_owner",
		market.ErrVersionExists:                            "market.version_exists",
		market.ErrRateLimited:                              "market.rate_limited",
		fmt.Errorf("%w: x", market.ErrUnpublishable):       "market.unpublishable",
		&market.RejectedError{Message: "summary too long"}: "market.rejected",
		fmt.Errorf("%w: down", market.ErrUnreachable):      "market.unreachable",
	} {
		base := publishServer(t, true, "tok", &fakePublisher{err: err})
		if code := marketCode(t, postPublish(t, base+"/market/publish", themeSubmission)); code != want {
			t.Errorf("%v: code = %q, want %q", err, code, want)
		}
	}
}
