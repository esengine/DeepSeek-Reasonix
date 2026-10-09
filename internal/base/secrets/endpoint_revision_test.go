package secrets

import (
	"strings"
	"testing"
)

func TestRevisionEndpointPathAndOpaqueFragment(t *testing.T) {
	for _, raw := range []string{"https://host/token/fixturesecret", "https://host/api_key/fixturesecret", "https://host/%74oken/fixturesecret", "https://host/mcp#fixturesecret"} {
		if got := RedactEndpoint(raw); strings.Contains(got, "fixturesecret") {
			t.Errorf("endpoint leaked: %s", got)
		}
	}
}

func TestRevisionEmbeddedURLUserinfoSuffix(t *testing.T) {
	for _, quote := range []string{"'", "\""} {
		got := RedactConfigValue("ordinary", "prefix https://neutral:head"+quote+"fixturesecret@host/mcp")
		if strings.Contains(got, "fixturesecret") {
			t.Errorf("embedded userinfo leaked: %s", got)
		}
	}
}

func TestRevisionCredentialCarriers(t *testing.T) {
	for _, args := range [][]string{
		{"-e", "KEY=fixturesecret"},
		{"Authorization: Bearer fixturesecret"},
		{"Authorization: Basic fixturesecret"},
		{"--header 'Authorization: Bearer fixturesecret'"},
		{"--env 'KEY=fixturesecret'"},
		{"--header 'X-Custom: fixturesecret'"},
		{"node --header 'Authorization: Bearer fixturesecret' && true"},
		{"node --env 'KEY=fixturesecret"},
	} {
		if got := strings.Join(RedactArgs(args), " "); strings.Contains(got, "fixturesecret") {
			t.Errorf("argument leaked: %s", got)
		}
	}
	if got := RedactConfigValue("ordinary", "Authorization: Basic fixturesecret"); strings.Contains(got, "fixturesecret") {
		t.Errorf("value leaked: %s", got)
	}
}
