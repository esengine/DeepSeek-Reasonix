package mcpsetup

import (
	"strings"
	"testing"
)

func TestEndpointDisplayFailsClosed(t *testing.T) {
	for _, raw := range []string{
		"https://user:fixture-secret@host/mcp",
		"HTTPS://user:fixture-secret@[::1]:8443/mcp",
		"https://host/mcp?password=fixture-secret",
		"https://host/mcp?passwd=fixture-secret",
		"https://host/mcp?%74oken=fixture-secret",
		"https://host/mcp#token=fixture-secret",
		"https://user:fixture-secret@host/mcp%zz",
		"https://host/mcp?token=fixture-secret;y=1",
		"https://host/mcp?token=fixture-secret%zz",
		"https://host/mcp#token=fixture-secret%zz",
	} {
		t.Run(raw, func(t *testing.T) {
			got := RedactURL(raw)
			if strings.Contains(got, "fixture-secret") || strings.Contains(got, "user:") {
				t.Fatalf("credential reached display: %q", got)
			}
			if twice := RedactURL(got); twice != got {
				t.Fatalf("not idempotent: %q -> %q", got, twice)
			}
		})
	}
	for _, raw := range []string{"https://[::1]:8443/mcp?author=neutral", "https://host/mcp?token="} {
		if got := RedactURL(raw); got != raw {
			t.Errorf("safe endpoint changed: %q -> %q", raw, got)
		}
	}
}

func TestCredentialFieldsUseKeyBoundaries(t *testing.T) {
	for _, key := range []string{"PASSWORD", "db_passwd", "X-Api-Key", "Authorization", "Cookie", "access_token"} {
		if got := Redact(key, "fixture-secret"); got == "fixture-secret" {
			t.Errorf("%s leaked", key)
		}
	}
	if got := Redact("author", "neutral"); got != "neutral" {
		t.Fatalf("author is not a credential: %q", got)
	}
}

func TestDraftRisksKeepOperationalCredentialsPrivate(t *testing.T) {
	for _, input := range []string{
		"HTTPS://user:fixture-secret@host/mcp",
		`node server --password fixture-secret --endpoint=https://host/mcp?%74oken=fixture-secret`,
	} {
		draft, err := Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(draft.Entries[0].URL+strings.Join(draft.Entries[0].Args, " "), "fixture-secret") {
			t.Fatal("operational credential was lost")
		}
		for _, risk := range draft.Risks {
			if strings.Contains(risk.Detail, "fixture-secret") {
				t.Fatalf("risk leaked: %+v", risk)
			}
		}
	}
}
