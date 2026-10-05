package boot

import (
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/browser"
)

// A look is owed only where one can be taken: a browser to open the page in and
// a model that can see the screenshot. Either missing, nothing is asked for.
func TestRenderRootNeedsABrowserAndAModelThatSees(t *testing.T) {
	session := browser.NewSession(browser.Config{Roots: []string{"/ws"}})
	seeing := &config.ProviderEntry{Vision: true}
	blind := &config.ProviderEntry{Vision: false}
	cases := []struct {
		name    string
		session *browser.Session
		entry   *config.ProviderEntry
		want    string
	}{
		{"both", session, seeing, "/ws"},
		{"no browser", nil, seeing, ""},
		{"blind model", session, blind, ""},
		{"no model", session, nil, ""},
	}
	for _, c := range cases {
		if got := renderRoot(c.session, c.entry, "/ws"); got != c.want {
			t.Errorf("%s: renderRoot = %q, want %q", c.name, got, c.want)
		}
	}
}
