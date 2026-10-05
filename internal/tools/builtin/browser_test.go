package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/platform/browser"
)

func TestBrowserPermissionArgsCarryTheHostsOrigin(t *testing.T) {
	session := browser.NewSession(browser.Config{})
	open := browserOpen{session: session}
	got := open.PermissionArgs(context.Background(), json.RawMessage(`{"url":"HTTPS://GitHub.com:8443/a?b","origin":"https://trusted.example"}`))
	var fields map[string]any
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["origin"] != "https://github.com:8443" || fields["url"] == nil {
		t.Fatalf("browser_open permission args = %s", got)
	}
	act := browserAct{session: session}
	got = act.PermissionArgs(context.Background(), json.RawMessage(`{"steps":[],"origin":"https://trusted.example"}`))
	fields = nil
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["origin"]; ok {
		t.Fatalf("an act with no page open kept the model's origin: %s", got)
	}
	if got := (browserOpen{}).PermissionArgs(context.Background(), json.RawMessage(`{"url":"file:///w/index.html"}`)); !json.Valid(got) || string(got) != `{"origin":"file://","url":"file:///w/index.html"}` {
		t.Fatalf("file url permission args = %s", got)
	}
}

func TestUnboundBrowserToolsSayThereIsNoBrowser(t *testing.T) {
	for _, tl := range []interface {
		Execute(context.Context, json.RawMessage) (string, error)
	}{browserOpen{}, browserRead{}, browserAct{}} {
		_, err := tl.Execute(context.Background(), json.RawMessage(`{"url":"https://example.com","steps":[{"action":"click"}]}`))
		if browser.CodeOf(err) != browser.CodeEngineMissing {
			t.Fatalf("%T without a browser = %v", tl, err)
		}
	}
	if BrowserBound(browserOpen{}) || !BrowserBound(browserAct{session: browser.NewSession(browser.Config{})}) {
		t.Fatal("BrowserBound misreports")
	}
}

func TestTheNetworkListReadsNewestFirstAndNarrowsByURL(t *testing.T) {
	requests := []browser.Request{
		{Method: "GET", URL: "https://example.com/", Kind: "document", Status: 200, Mime: "text/html", Bytes: 2048, Millis: 120},
		{Method: "GET", URL: "https://cdn.example.com/app.js", Kind: "script", Status: 304, Millis: 8},
		{Method: "POST", URL: "https://example.com/api/save", Kind: "xhr", Status: 500, Mime: "application/json", Bytes: 91},
		{Method: "GET", URL: "https://example.com/gone", Kind: "fetch", Failed: "net::ERR_NAME_NOT_RESOLVED", Millis: 30},
		{Method: "GET", URL: "https://example.com/slow", Kind: "fetch"},
	}
	all := renderRequests(requests, "", 0)
	lines := strings.Split(all, "\n")
	if len(lines) != 5 {
		t.Fatalf("rendered %d lines, want one per request:\n%s", len(lines), all)
	}
	if !strings.HasPrefix(lines[0], "- GET https://example.com/slow") {
		t.Errorf("the newest request is not first:\n%s", all)
	}
	for _, want := range []string{
		"- GET https://example.com/ [document] → 200 text/html 2.0 KB 120ms",
		"- POST https://example.com/api/save [xhr] → 500 application/json 91 B",
		"- GET https://example.com/gone [fetch] → failed: net::ERR_NAME_NOT_RESOLVED 30ms",
		"- GET https://example.com/slow [fetch] → in flight",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if got := renderRequests(requests, "api/", 0); got != "- POST https://example.com/api/save [xhr] → 500 application/json 91 B" {
		t.Errorf("narrowing by URL = %q", got)
	}
	// What the tab loaded before this page is not read as this page's.
	older := append([]browser.Request{{Method: "GET", URL: "https://example.com/old", Kind: "document", Status: 200}}, requests...)
	for i := range older[1:] {
		older[i+1].Page = 1
	}
	changed := renderRequests(older, "", 0)
	if !strings.Contains(changed, "Earlier, before the page changed:\n- GET https://example.com/old") {
		t.Errorf("the navigation is not marked:\n%s", changed)
	}
	if strings.Count(changed, "Earlier, before") != 1 {
		t.Errorf("the navigation is marked more than once:\n%s", changed)
	}
	if got := renderRequests(requests, "", 2); strings.Count(got, "\n") != 1 {
		t.Errorf("a limit of 2 gave:\n%s", got)
	}
	if got := renderRequests(requests, "nowhere", 0); got != `No request carried "nowhere".` {
		t.Errorf("a match with no answer = %q", got)
	}
	if got := renderRequests(nil, "", 0); got != "The page has requested nothing." {
		t.Errorf("a page that asked for nothing = %q", got)
	}
}
