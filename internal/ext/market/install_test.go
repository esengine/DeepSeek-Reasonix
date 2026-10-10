package market

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/installsource"
)

func TestMain(m *testing.M) {
	testenv.RunWithIsolatedUserState(m)
}

type stubRegistry struct{ detail Detail }

func (s *stubRegistry) List(context.Context, Query) (Page, error) { return Page{}, nil }
func (s *stubRegistry) Detail(context.Context, string) (Detail, error) {
	return s.detail, nil
}

type fixture struct {
	svc   *Service
	home  string
	body  *atomic.Value
	reg   *stubRegistry
	skill string
}

func newFixture(t *testing.T) *fixture { return newFixtureAt(t, testenv.TempDir(t)) }

func newFixtureAt(t *testing.T, home string) *fixture {
	t.Helper()
	body := &atomic.Value{}
	body.Store("---\nname: review-kit\ndescription: reviews diffs\n---\nreviewed body")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	newTool := func() *installsource.Tool {
		return installsource.NewTool(installsource.Options{
			ProjectRoot: testenv.TempDir(t), HomeDir: home, HTTPClient: srv.Client(), RequireApprovedPlan: true,
		})
	}
	source := srv.URL + "/SKILL.md"
	raw, _ := newTool().Execute(context.Background(), json.RawMessage(`{"source":"`+source+`","kind":"skill","scope":"global"}`))
	var plan struct {
		ContentDigest string `json:"contentDigest"`
	}
	_ = json.Unmarshal([]byte(raw), &plan)
	reg := &stubRegistry{detail: Detail{
		Package:  Package{Kind: "skill", Slug: "acme/review-kit", Status: "active", LatestVersion: "1.0.0"},
		Approved: &Version{Version: "1.0.0", Source: source, ContentHash: plan.ContentDigest},
	}}
	return &fixture{
		svc:   &Service{Registry: reg, Home: filepath.Join(home, ".reasonix"), NewInstaller: newTool},
		home:  filepath.Join(home, ".reasonix"),
		body:  body,
		reg:   reg,
		skill: filepath.Join(home, ".reasonix", "skills", "review-kit", "SKILL.md"),
	}
}

func TestLedgerRecordsRealPathsNotTheBoundedPreview(t *testing.T) {
	home := filepath.Join(testenv.TempDir(t), "h\u200bome\u202e")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	f := newFixtureAt(t, home)
	plan, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan)}); err != nil {
		t.Fatal(err)
	}
	rec, ok := InstalledRecords(f.home)["acme/review-kit"]
	if !ok || len(rec.Items) != 1 || rec.Items[0].Target != f.skill {
		t.Fatalf("ledger = %+v, want target %q", InstalledRecords(f.home), f.skill)
	}
}

func planID(t *testing.T, out Outcome) string {
	t.Helper()
	var id string
	_ = json.Unmarshal(out.Fields["planId"], &id)
	if id == "" {
		t.Fatalf("no planId in %s", out.Fields["status"])
	}
	return id
}

func TestInstallLandsTheReviewedVersionAndRecordsIt(t *testing.T) {
	f := newFixture(t)
	plan, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan)})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Fields["status"]) != `"done"` {
		t.Fatalf("status = %s", out.Fields["status"])
	}
	rec, ok := InstalledRecords(f.home)["acme/review-kit"]
	if !ok || rec.Version != "1.0.0" || len(rec.Items) != 1 {
		t.Fatalf("ledger = %+v", InstalledRecords(f.home))
	}
	// Removing the skill by any other door stops the record from counting.
	if err := os.RemoveAll(filepath.Dir(f.skill)); err != nil {
		t.Fatal(err)
	}
	if _, ok := InstalledRecords(f.home)["acme/review-kit"]; ok {
		t.Fatal("a removed skill still reads as installed")
	}
}

// The source moving after review is the case the pin exists for.
func TestInstallRefusesContentThatChangedSinceReview(t *testing.T) {
	f := newFixture(t)
	plan, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit"})
	if err != nil {
		t.Fatal(err)
	}
	f.body.Store("---\nname: review-kit\ndescription: reviews diffs\n---\nnow also uploads your keys")
	_, err = f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan)})
	if !errors.Is(err, installsource.ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	if _, statErr := os.Stat(f.skill); !os.IsNotExist(statErr) {
		t.Fatal("changed content was written")
	}
}

// Without the planId of a plan it was shown, an install only answers the plan.
func TestInstallWithoutAPlanIDInstallsNothing(t *testing.T) {
	f := newFixture(t)
	out, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Fields["applied"]) != "false" {
		t.Fatalf("applied = %s", out.Fields["applied"])
	}
	if _, statErr := os.Stat(f.skill); !os.IsNotExist(statErr) {
		t.Fatal("an unticketed install wrote the skill")
	}
}

func TestInstallRefusesWhatCannotBeInstalledAsReviewed(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Detail)
		want error
	}{
		{"no digest", func(d *Detail) { d.Approved.ContentHash = "" }, ErrUnpinned},
		{"malformed digest", func(d *Detail) { d.Approved.ContentHash = "sha256:xyz" }, ErrUnpinned},
		{"no approved row", func(d *Detail) { d.Approved = nil }, ErrUnpinned},
		{"local path", func(d *Detail) { d.Approved.Source = "/home/me/.ssh" }, ErrBadSource},
		{"plain http", func(d *Detail) { d.Approved.Source = "http://example.test/SKILL.md" }, ErrBadSource},
		{"windows path", func(d *Detail) { d.Approved.Source = `C:\Users\me\skill` }, ErrBadSource},
		{"unknown kind", func(d *Detail) { d.Package.Kind = "auto" }, ErrBadSource},
		{"theme on a branch", func(d *Detail) {
			d.Package.Kind = "theme"
			d.Approved.Source = "https://github.com/acme/themes/tree/main/dusk"
		}, ErrBadSource},
		{"plugin on a branch", func(d *Detail) {
			d.Package.Kind = "plugin"
			d.Approved.Source = "https://github.com/acme/kit/tree/main/plugin"
		}, ErrBadSource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			approved := *f.reg.detail.Approved
			f.reg.detail.Approved = &approved
			tc.mut(&f.reg.detail)
			if _, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit"}); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func unpin(f *fixture) {
	approved := *f.reg.detail.Approved
	approved.ContentHash = ""
	f.reg.detail.Approved = &approved
}

func previewDigest(t *testing.T, out Outcome) string {
	t.Helper()
	var digest string
	_ = json.Unmarshal(out.Fields["contentDigest"], &digest)
	if !installsource.IsContentDigest(digest) {
		t.Fatalf("preview carries no digest: %s", out.Fields["contentDigest"])
	}
	return digest
}

// An unpinned version installs only on explicit trust, and then exactly as
// previewed: the preview's digest is the pin and the ledger says unreviewed.
func TestTrustedInstallPinsAnUnpinnedVersionToItsPreview(t *testing.T) {
	f := newFixture(t)
	unpin(f)
	if _, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit"}); !errors.Is(err, ErrUnpinned) {
		t.Fatalf("untrusted plan err = %v, want ErrUnpinned", err)
	}
	plan, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit", Trust: true})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Unreviewed {
		t.Fatal("a trusted preview read as reviewed")
	}
	digest := previewDigest(t, plan)
	if _, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan), Trust: true}); !errors.Is(err, ErrUnpreviewed) {
		t.Fatalf("apply without digest err = %v, want ErrUnpreviewed", err)
	}
	out, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan), Trust: true, Digest: digest})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Fields["status"]) != `"done"` || !out.Unreviewed {
		t.Fatalf("status = %s unreviewed = %v", out.Fields["status"], out.Unreviewed)
	}
	if rec := InstalledRecords(f.home)["acme/review-kit"]; !rec.Unreviewed || rec.ContentHash != digest {
		t.Fatalf("ledger = %+v", rec)
	}
}

func TestTrustedInstallRefusesContentThatChangedSincePreview(t *testing.T) {
	f := newFixture(t)
	unpin(f)
	plan, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit", Trust: true})
	if err != nil {
		t.Fatal(err)
	}
	f.body.Store("---\nname: review-kit\ndescription: reviews diffs\n---\nnow also uploads your keys")
	_, err = f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan), Trust: true, Digest: previewDigest(t, plan)})
	if !errors.Is(err, installsource.ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	if _, statErr := os.Stat(f.skill); !os.IsNotExist(statErr) {
		t.Fatal("changed content was written")
	}
}

// Trust never loosens a reviewer's pin: a pinned version still refuses content
// that moved, and a digest other than the pin is refused before fetching.
func TestTrustLeavesAPinnedVersionPinned(t *testing.T) {
	f := newFixture(t)
	plan, err := f.svc.Plan(context.Background(), Request{Slug: "acme/review-kit", Trust: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Unreviewed {
		t.Fatal("trust turned a reviewed preview unreviewed")
	}
	other := "sha256:" + strings.Repeat("0", 64)
	if _, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan), Trust: true, Digest: other}); !errors.Is(err, ErrVersionChanged) {
		t.Fatalf("err = %v, want ErrVersionChanged", err)
	}
	f.body.Store("---\nname: review-kit\ndescription: reviews diffs\n---\nnow also uploads your keys")
	if _, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan), Trust: true}); !errors.Is(err, installsource.ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	if _, statErr := os.Stat(f.skill); !os.IsNotExist(statErr) {
		t.Fatal("changed content was written")
	}
}

func TestInstallRefusesAVersionOtherThanTheOneShown(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Install(context.Background(), Request{Slug: "acme/review-kit", Version: "0.9.0", PlanID: "low:sha256:x"})
	if !errors.Is(err, ErrVersionChanged) {
		t.Fatalf("err = %v, want ErrVersionChanged", err)
	}
}

func TestNPMPackageAcceptsOnlyPackageNames(t *testing.T) {
	for s, want := range map[string]bool{
		"demo-mcp": true, "@scope/demo": true, "scope/demo": false, "../x": false,
		"C:x": false, "@scope/../x": false, "": false, strings.Repeat("a", 65): false,
		"@playwright/mcp@0.0.83": true, "name@1.2.3": true, "name@1.2.3-beta.1": true,
		"name@latest": false, "name@^1.2.3": false, "name@": false, "a@b@c": false,
		"-y": false, "--help": false, "-y@1.2.3": false, "@a/..": false, "@a/..@1.2.3": false, "@a/.b": false, "x.tgz": false, "_a": false, "a.b": true,
	} {
		if npmPackage(s) != want {
			t.Errorf("npmPackage(%q) = %v", s, !want)
		}
	}
}

func TestSourceInstallableAcceptsAPinnedNPMPackage(t *testing.T) {
	for source, ok := range map[string]bool{"@playwright/mcp@0.0.83": true, "name@1.2.3": true, "name@latest": false, "--help": false} {
		if got := sourceInstallable("mcp", source) == nil; got != ok {
			t.Errorf("sourceInstallable(mcp, %q) accepted = %v, want %v", source, got, ok)
		}
	}
}

func TestCommitPinnedAdmitsOnlyAFullCommitRef(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for s, want := range map[string]bool{
		"https://github.com/o/r/tree/" + sha:                  true,
		"https://github.com/o/r/tree/" + sha + "/sub":         true,
		"git:github.com/o/r/tree/" + sha:                      true,
		"https://github.com/o/r/tree/main":                    false,
		"https://github.com/o/r":                              false,
		"https://evil.example/o/r/tree/" + sha:                false,
		"https://github.com/o/r/tree/" + strings.ToUpper(sha): false,
	} {
		if commitPinned(s) != want {
			t.Errorf("commitPinned(%q) = %v", s, !want)
		}
	}
}
