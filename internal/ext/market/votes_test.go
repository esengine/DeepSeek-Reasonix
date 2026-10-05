package market

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestVoteSendsTheTokenAndValueToTheRegistry(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/packages/acme/kit/vote" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("authorization = %q", got)
		}
		var body map[string]int
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["value"] != -1 || len(body) != 1 {
			t.Errorf("body = %v", body)
		}
		_, _ = w.Write([]byte(`{"value":-1,"upCount":3,"downCount":1,"approvalRate":0.75}`))
	})
	v, err := c.Vote(context.Background(), "tok", "acme/kit", -1)
	if err != nil {
		t.Fatal(err)
	}
	if v.Value != -1 || v.UpCount != 3 || v.DownCount != 1 || v.ApprovalRate == nil || *v.ApprovalRate != 0.75 {
		t.Fatalf("vote = %+v", v)
	}
}

func TestVoteRefusesBeforeAskingWhenItCannotSucceed(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	if _, err := c.Vote(context.Background(), "tok", "acme/kit", 2); !errors.Is(err, ErrBadVote) {
		t.Errorf("value 2: err = %v", err)
	}
	if _, err := c.Vote(context.Background(), "", "acme/kit", 1); !errors.Is(err, ErrSignedOut) {
		t.Errorf("no token: err = %v", err)
	}
	if _, err := c.MyVote(context.Background(), "tok", "../x"); !errors.Is(err, ErrBadSlug) {
		t.Errorf("bad slug: err = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("registry asked %d times", calls.Load())
	}
}

// The registry is the one that knows why it refused; its code decides the
// sentinel, never the status alone and never the message.
func TestRegistryRefusalsKeepTheirIdentity(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{401, "unauthorized", ErrSignedOut},
		{403, "email_unverified", ErrEmailUnverified},
		{403, "own_package", ErrOwnPackage},
		{404, "not_found", ErrNotFound},
		{429, "rate_limited", ErrRateLimited},
		{503, "accounts_unavailable", ErrUnreachable},
		{403, "forbidden", ErrBadResponse},
		{500, "internal", ErrBadResponse},
	}
	for _, tc := range cases {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"code":"` + tc.code + `","message":"own_package email_unverified"}}`))
		})
		if _, err := c.Vote(context.Background(), "tok", "acme/kit", 1); !errors.Is(err, tc.want) {
			t.Errorf("%d %s: err = %v, want %v", tc.status, tc.code, err, tc.want)
		}
	}
}

// A redirect is where a bearer would leak to another host; it is refused as
// an answer and the token goes nowhere else.
func TestVoteTokenNeverFollowsARedirect(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Add(1) }))
	t.Cleanup(other.Close)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusTemporaryRedirect)
	})
	if _, err := c.Vote(context.Background(), "tok", "acme/kit", 1); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
	if leaked.Load() != 0 {
		t.Fatal("the redirect target was contacted")
	}
}

func TestReportInstallSendsOnlyTheSlugAndKey(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/packages/acme/kit/installed" || r.Header.Get("Authorization") != "" {
			t.Errorf("%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != `{"installId":"0123456789abcdef0123456789abcdef"}` {
			t.Errorf("body = %s", raw)
		}
		_, _ = w.Write([]byte(`{"ok":true,"counted":true,"installCount":1}`))
	})
	if err := c.ReportInstall(context.Background(), "acme/kit", "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
}

func TestInstallIDIsTheMarketsOwnRandomID(t *testing.T) {
	home := t.TempDir()
	id, err := InstallID(home)
	if err != nil {
		t.Fatal(err)
	}
	if !installIDShape.MatchString(id) {
		t.Fatalf("id %q is not 32 lowercase hex", id)
	}
	again, err := InstallID(home)
	if err != nil || again != id {
		t.Fatalf("second read = %q, %v; want the stored %q", again, err, id)
	}
	if other, _ := InstallID(t.TempDir()); other == id {
		t.Fatal("two homes drew the same id")
	}
	if err := os.WriteFile(filepath.Join(home, "market", "install-id"), []byte("not-an-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fixed, err := InstallID(home); err != nil || !installIDShape.MatchString(fixed) || fixed == id {
		t.Fatalf("a corrupt file was not replaced: %q, %v", fixed, err)
	}
	if _, err := os.Stat(filepath.Join(home, "cli-telemetry-install-id")); !os.IsNotExist(err) {
		t.Fatal("the market id touched the usage-statistics id")
	}
}

type recordingReporter struct{ got chan [2]string }

func (r *recordingReporter) ReportInstall(_ context.Context, slug, key string) error {
	r.got <- [2]string{slug, key}
	return nil
}

func TestInstallReportsOnceOnlyWhenAKeyIsGiven(t *testing.T) {
	f := newFixture(t)
	rep := &recordingReporter{got: make(chan [2]string, 4)}
	f.svc.Report = rep
	plan, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit"})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.InstallKey = "k"
	if _, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan)}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-rep.got:
		if got != [2]string{"acme/review-kit", "k"} {
			t.Fatalf("reported %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no report after an install")
	}
	select {
	case got := <-rep.got:
		t.Fatalf("a plan was reported too: %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestInstallWithoutAKeyReportsNothing(t *testing.T) {
	f := newFixture(t)
	rep := &recordingReporter{got: make(chan [2]string, 1)}
	f.svc.Report = rep
	plan, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan)}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-rep.got:
		t.Fatalf("reported without consent: %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}
