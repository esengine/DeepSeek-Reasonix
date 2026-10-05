package permission

import (
	"encoding/json"
	"testing"
)

func TestBrowserGrantNamesTheOriginAndCoversEveryBrowserTool(t *testing.T) {
	const github = "https://github.com"
	for _, scope := range []func(string, string) string{SessionGrantRuleForScope, RememberRuleForScope} {
		rule := scope("browser_act", github)
		if rule != "Browser="+github {
			t.Fatalf("grant rule = %q, want Browser=%s", rule, github)
		}
		for _, tool := range []string{"browser_open", "browser_read", "browser_act"} {
			if !SessionGrantMatches(rule, tool, github) {
				t.Errorf("%s on %s is not covered by %q", tool, github, rule)
			}
		}
		if SessionGrantMatches(rule, "browser_open", "https://github.com.evil.example") {
			t.Errorf("%q covered another origin", rule)
		}
		if SessionGrantMatches(rule, "web_fetch", github) {
			t.Errorf("%q covered a tool that is not the browser", rule)
		}
	}
	if SessionGrantMatches("Browser", "browser_act", github) {
		t.Fatal("a bare Browser grant answered for an origin nobody approved")
	}
	if SessionGrantMatches("browser_act", "browser_act", github) {
		t.Fatal("a bare tool-name grant answered for an origin nobody approved")
	}
}

func TestBrowserDecisionsFollowTheModeAndTheOrigin(t *testing.T) {
	args := func(origin string) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"steps": []any{}, "origin": origin})
		return raw
	}
	ask := New("ask", nil, nil, []string{"Browser(https://*.bank.example)"})
	if got := ask.Decide("browser_act", false, args("https://example.com")); got != Ask {
		t.Fatalf("ask mode on a new origin = %v, want ask", got)
	}
	if got := ask.Decide("browser_read", true, args("https://example.com")); got != Allow {
		t.Fatalf("reading a page = %v, want allow", got)
	}
	if got := ask.Decide("browser_read", true, args("https://www.bank.example")); got != Deny {
		t.Fatalf("a denied origin = %v, want deny even for a read", got)
	}
	auto := New("allow", nil, nil, nil)
	if got := auto.Decide("browser_act", false, args("https://example.com")); got != Allow {
		t.Fatalf("auto on a new origin = %v, want allow", got)
	}
	if subjectRequiresHuman("browser_act", "https://example.com") {
		t.Fatal("a browser origin must not force a person in auto")
	}
}

func TestBrowserCredentialAnswersOnlyToItsExactSubject(t *testing.T) {
	const site = "https://bank.example"
	credential := BrowserCredentialPrefix + site
	args, _ := json.Marshal(map[string]any{"steps": []any{}, "origin": credential})
	if got := Subjects(args); len(got) != 2 || got[0] != credential || got[1] != site {
		t.Fatalf("subjects = %v, want the credential then its site", got)
	}
	cases := []struct {
		name   string
		policy Policy
		want   Decision
	}{
		{"auto", New("allow", nil, nil, nil), Ask},
		{"ask", New("ask", nil, nil, nil), Ask},
		{"a site rule", New("allow", []string{"Browser(https://bank.example)"}, nil, nil), Ask},
		{"a glob", New("allow", []string{"Browser(*)", "browser_act"}, nil, nil), Ask},
		{"the exact subject", New("allow", []string{"Browser=" + credential}, nil, nil), Allow},
		{"a denied site", New("allow", []string{"Browser=" + credential}, nil, []string{"Browser(https://*.example)"}), Deny},
		{"deny mode", New("deny", nil, nil, nil), Deny},
	}
	for _, tc := range cases {
		if got := tc.policy.Decide("browser_act", false, args); got != tc.want {
			t.Errorf("%s: decision = %v, want %v", tc.name, got, tc.want)
		}
	}
	if SessionGrantMatches(SessionGrantRuleForScope("browser_open", site), "browser_act", credential) {
		t.Fatal("a grant for the site answered for typing a secret into it")
	}
	grant := SessionGrantRuleForScope("browser_act", credential)
	if !SessionGrantMatches(grant, "browser_act", credential) || SessionGrantMatches(grant, "browser_act", BrowserCredentialPrefix+"https://other.example") {
		t.Fatalf("credential grant %q does not cover exactly its own subject", grant)
	}
}

func TestBrowserScriptAnswersOnlyToItsExactSubject(t *testing.T) {
	const site = "https://example.com"
	script := BrowserScriptPrefix + site
	args, _ := json.Marshal(map[string]any{"origin": script})
	if got := Subjects(args); len(got) != 2 || got[0] != script || got[1] != site {
		t.Fatalf("subjects = %v, want the script then its site", got)
	}
	if got := New("allow", []string{"Browser=" + site}, nil, nil).Decide("browser_act", false, args); got != Ask {
		t.Fatalf("site grant decided arbitrary script as %v, want ask", got)
	}
	if got := New("allow", []string{"Browser=" + script}, nil, nil).Decide("browser_act", false, args); got != Allow {
		t.Fatalf("exact script grant decided %v, want allow", got)
	}
}
