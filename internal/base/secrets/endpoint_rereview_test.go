package secrets

import (
	"reflect"
	"strings"
	"testing"
)

func TestRereviewAttachedShortCredentialCarriers(t *testing.T) {
	for _, arg := range []string{"-eKEY=fixturesecret", "-HAuthorization:Basic fixturesecret", "-HAuthorization:Bearer fixturesecret", "-e=KEY=fixturesecret", "-H=Authorization:Basic fixturesecret", "-HX-Custom:fixturesecret"} {
		t.Run(arg[:2]+strings.ReplaceAll(arg[2:], " ", "_"), func(t *testing.T) {
			input := []string{arg, "ordinary", "https://host/clean"}
			original := append([]string(nil), input...)
			got := RedactArgs(input)
			if strings.Contains(got[0], "fixturesecret") || got[1] != input[1] || got[2] != input[2] {
				t.Fatalf("argument projection = %v", got)
			}
			if !reflect.DeepEqual(input, original) {
				t.Fatal("operational argv changed")
			}
		})
	}
	input := []string{"-v", "--help", "ordinary=value", "https://host/clean", "-e", "KEY=fixturesecret", "tail"}
	got := RedactArgs(input)
	if !reflect.DeepEqual(got[:4], input[:4]) || got[5] != EndpointRedacted || got[6] != "tail" {
		t.Fatalf("neighbouring arguments = %v", got)
	}
}

func TestRereviewEndpointPrefixMixedCredentials(t *testing.T) {
	for _, suffix := range []string{" Authorization: Bearer fixturesecret", " Authorization: Basic fixturesecret", " --header 'X-Custom: fixturesecret'", " -eKEY=fixturesecret", "\tTOKEN=fixturesecret", "\nAuthorization: Basic fixturesecret"} {
		value := "https://host/mcp" + suffix
		if got := RedactConfigValue("ORDINARY", value); strings.Contains(got, "fixturesecret") {
			t.Errorf("mixed value leaked: %s", got)
		}
		if got := RedactArgs([]string{value}); strings.Contains(got[0], "fixturesecret") {
			t.Errorf("mixed argument leaked: %v", got)
		}
	}
	for _, value := range []string{"https://host/mcp", "https://host/mcp?author=neutral", "https://host/a%20b", "https://host/mcp ordinary", "ordinary value"} {
		if got := RedactConfigValue("ORDINARY", value); got != value {
			t.Errorf("harmless value changed: %q -> %q", value, got)
		}
	}
}

func TestRereviewURLArgumentQueryProjection(t *testing.T) {
	for _, value := range []string{"https://host/mcp?token=fixturesecret", "https://host/mcp?%74oken=fixturesecret", "https://host/mcp#token=fixturesecret"} {
		if got := RedactArgs([]string{value}); strings.Contains(got[0], "fixturesecret") {
			t.Errorf("URL argument leaked: %v", got)
		}
	}
}
