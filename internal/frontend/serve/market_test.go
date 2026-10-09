package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/ext/market"
)

type fakeRegistry struct {
	page   market.Page
	detail market.Detail
	err    error
	asked  market.Query
}

func (f *fakeRegistry) List(_ context.Context, q market.Query) (market.Page, error) {
	f.asked = q
	return f.page, f.err
}
func (f *fakeRegistry) Detail(context.Context, string) (market.Detail, error) {
	return f.detail, f.err
}

func withRegistry(t *testing.T, reg market.Registry) {
	t.Helper()
	old, oldBrowse := marketRegistry, marketBrowser
	marketRegistry = func(*http.Client) market.Registry { return reg }
	marketBrowser = marketRegistry
	t.Cleanup(func() { marketRegistry, marketBrowser = old, oldBrowse })
}

func marketCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var r Reason
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	return r.Code
}

// "Installed" is read off the ledger only while what it recorded is still on
// disk, so the listing says it for the one whose skill file exists.
func TestMarketListMarksWhatIsInstalledHere(t *testing.T) {
	home, _, base := pluginHome(t)
	kept := filepath.Join(home, "skills", "kept", "SKILL.md")
	writePluginFile(t, kept, "x")
	ledger := fmt.Sprintf(`{"records":[
		{"slug":"a/kept","kind":"skill","version":"1.0.0","contentHash":"sha256:1","items":[{"kind":"skill","name":"kept","target":%q}]},
		{"slug":"a/gone","kind":"skill","version":"1.0.0","contentHash":"sha256:2","items":[{"kind":"skill","name":"gone","target":%q}]}]}`,
		kept, filepath.Join(home, "skills", "gone", "SKILL.md"))
	writePluginFile(t, filepath.Join(home, "market", "installed.json"), ledger)
	withRegistry(t, &fakeRegistry{page: market.Page{Packages: []market.Package{
		{Slug: "a/kept", Status: "active"}, {Slug: "a/gone", Status: "active"}, {Slug: "a/new", Status: "active"},
	}}})

	resp, err := http.Get(base + "/market/packages?q=x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Packages []marketEntry `json:"packages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range out.Packages {
		got[p.Slug] = p.Installed != nil
	}
	if !got["a/kept"] || got["a/gone"] || got["a/new"] || len(got) != 3 {
		t.Fatalf("installed flags = %v", got)
	}
}

func TestMarketDetailSaysWhetherTheApprovedVersionIsPinned(t *testing.T) {
	_, _, base := pluginHome(t)
	reg := &fakeRegistry{detail: market.Detail{
		Package:  market.Package{Slug: "a/b", Kind: "skill", Status: "active", LatestVersion: "1"},
		Approved: &market.Version{Version: "1", Source: "https://example.test/SKILL.md"},
	}}
	withRegistry(t, reg)
	var view marketDetailView
	resp, err := http.Get(base + "/market/packages/a/b")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&view)
	resp.Body.Close()
	if view.Pinned {
		t.Fatal("an approved row with no digest read as pinned")
	}
	reg.detail.Approved.ContentHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	resp, err = http.Get(base + "/market/packages/a/b")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&view)
	resp.Body.Close()
	if !view.Pinned {
		t.Fatal("a digest-bearing approved row read as unpinned")
	}
}

// Each way a market request fails reaches the window as its own code.
func TestMarketRefusalsCarryTheirCause(t *testing.T) {
	unpinned := market.Detail{
		Package:  market.Package{Slug: "a/b", Kind: "skill", Status: "active", LatestVersion: "1"},
		Approved: &market.Version{Version: "1", Source: "https://example.test/SKILL.md"},
	}
	local := unpinned
	local.Approved = &market.Version{Version: "1", Source: "/etc", ContentHash: "sha256:" + fmt.Sprintf("%064d", 0)}
	cases := []struct {
		name   string
		reg    *fakeRegistry
		path   string
		body   map[string]any
		status int
		code   string
	}{
		{"unpinned", &fakeRegistry{detail: unpinned}, "/market/plan", map[string]any{"slug": "a/b"}, http.StatusConflict, "market.unpinned"},
		{"local source", &fakeRegistry{detail: local}, "/market/plan", map[string]any{"slug": "a/b"}, http.StatusConflict, "market.bad_source"},
		{"down", &fakeRegistry{err: market.ErrUnreachable}, "/market/plan", map[string]any{"slug": "a/b"}, http.StatusBadGateway, "market.unreachable"},
		{"gone", &fakeRegistry{err: market.ErrNotFound}, "/market/plan", map[string]any{"slug": "a/b"}, http.StatusNotFound, "market.not_found"},
		{"trusted install names no preview", &fakeRegistry{detail: unpinned}, "/market/install", map[string]any{"slug": "a/b", "version": "1", "trust": true}, http.StatusBadRequest, "market.unpreviewed"},
		{"install names no version", &fakeRegistry{detail: unpinned}, "/market/install", map[string]any{"slug": "a/b"}, http.StatusBadRequest, codeMissingField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, base := pluginHome(t)
			withRegistry(t, tc.reg)
			resp := postJSON(t, base+tc.path, tc.body)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if code := marketCode(t, resp); code != tc.code {
				t.Fatalf("code = %q, want %q", code, tc.code)
			}
		})
	}
}

func TestMarketLedgerNeverCountsWithoutFiles(t *testing.T) {
	home, _, _ := pluginHome(t)
	if len(market.InstalledRecords(home)) != 0 {
		t.Fatal("an empty home reported installs")
	}
	if _, err := os.Stat(filepath.Join(home, "market")); !os.IsNotExist(err) {
		t.Fatal("reading the ledger created it")
	}
}

// The filter is the registry's to apply, and a registry that cannot is its own
// code rather than a listing quietly missing rows.
func TestMarketListPassesTheInstallableFilterThrough(t *testing.T) {
	_, _, base := pluginHome(t)
	yes := true
	reg := &fakeRegistry{page: market.Page{Packages: []market.Package{{Slug: "a/kit", Status: "active", Pinned: &yes}}}}
	withRegistry(t, reg)
	resp, err := http.Get(base + "/market/packages?pinned=1")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Packages []struct {
			Slug   string `json:"slug"`
			Pinned *bool  `json:"pinned"`
		} `json:"packages"`
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !reg.asked.Pinned || len(out.Packages) != 1 || out.Packages[0].Pinned == nil || !*out.Packages[0].Pinned {
		t.Fatalf("asked = %+v, out = %+v", reg.asked, out)
	}

	withRegistry(t, &fakeRegistry{err: market.ErrFilterUnsupported})
	resp, err = http.Get(base + "/market/packages?pinned=1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if code := marketCode(t, resp); code != "market.filter_unsupported" {
		t.Errorf("code = %q", code)
	}
}
