package secrets

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"strings"
	"testing"
)

func TestRound3MixedCredentialContext(t *testing.T) {
	for _, suffix := range []string{" Bearer neutralprobe", " Basic neutralprobe", "\rTOKEN=neutralprobe", "\fTOKEN=neutralprobe", "\tclientToken=neutralprobe"} {
		input := "https://host/mcp" + suffix
		for _, got := range []string{RedactConfigValue("ordinary", input), strings.Join(RedactArgs([]string{input}), " ")} {
			if strings.Contains(got, "neutralprobe") {
				t.Errorf("credential survived: %q", got)
			}
		}
	}
}

func TestRound3DottedComponents(t *testing.T) {
	for _, key := range []string{"client.token", "client.password", "client.secret", "clientToken", "clientJWT", "CLIENT.ſecret"} {
		if !CredentialKey(key) {
			t.Errorf("credential component missed: %s", key)
		}
		for _, carrier := range []string{"?", "#"} {
			got := RedactEndpoint("https://host/mcp" + carrier + url.QueryEscape(key) + "=neutralprobe")
			if strings.Contains(got, "neutralprobe") {
				t.Errorf("credential survived: %q", got)
			}
		}
	}
}

func TestRound3NestedValues(t *testing.T) {
	for _, input := range []string{
		"https://host/mcp?next=" + url.QueryEscape("https://u:neutralprobe@host/mcp"),
		"https://host/mcp#redirect=https://u:neutralprobe@host/mcp",
		"https://host/mcp?next=" + url.QueryEscape("client.token=neutralprobe"),
	} {
		if got := RedactEndpoint(input); strings.Contains(got, "neutralprobe") {
			t.Errorf("nested credential survived: %q", got)
		}
	}
}

func TestStructuralRepeatedKeyEncoding(t *testing.T) {
	key := "client.token"
	for range 4 {
		key = strings.ReplaceAll(url.QueryEscape(key), ".", "%2e")
		if !CredentialKey(key) {
			t.Errorf("encoded key missed: %s", key)
		}
		input := "https://host/?" + url.QueryEscape(key) + "=neutralprobe"
		if got := RedactEndpoint(input); strings.Contains(got, "neutralprobe") {
			t.Errorf("encoded credential survived: %s", got)
		}
	}
}

func TestStructuralGeneratedCorpus(t *testing.T) {
	rng := rand.New(rand.NewPCG(11785, 11816))
	components := []string{"token", "password", "passwd", "secret", "key", "auth", "authorization", "credential", "signature", "sig", "session", "cookie", "bearer", "jwt", "apikey", "access", "private"}
	separators := []string{".", "-", "_", "/", ":", "\t", "\r", "\f"}
	for seed := range 512 {
		canary := fmt.Sprintf("neutralprobe%08d", seed)
		part := components[rng.IntN(len(components))]
		key := "client" + separators[rng.IntN(len(separators))] + strings.ToUpper(part)
		nested := "https://u:" + canary + "@[::1]:8443/mcp?" + url.QueryEscape(key) + "=" + canary
		inputs := []string{"https://host/?next=" + url.QueryEscape(nested), "https://host/#redirect=" + url.QueryEscape(nested), key + "=" + canary, key + ": token " + canary}
		var encoded strings.Builder
		for _, b := range []byte(canary) {
			fmt.Fprintf(&encoded, "%%%02x", b)
		}
		inputs = append(inputs, "https://host/?"+url.QueryEscape(key)+"="+encoded.String(), "https://host/?next="+url.QueryEscape("https://host/?clientToken="+encoded.String()), "client.ſecret="+encoded.String())
		for _, input := range inputs {
			outputs := []string{RedactConfigValue("ordinary", input), strings.Join(RedactArgs([]string{input}), " "), RedactConfigMap(map[string]string{key: canary})[key]}
			for _, got := range outputs {
				decoded := got
				for range projectionDepth {
					if strings.Contains(decoded, canary) {
						t.Fatalf("seed %d input %q output %q", seed, input, got)
					}
					next, err := url.QueryUnescape(decoded)
					if err != nil || next == decoded {
						break
					}
					decoded = next
				}
			}
		}
		for _, args := range [][]string{{"--client" + separators[rng.IntN(len(separators))] + part, canary}, {"-e", key + "=" + canary}, {"-H", key + ": Digest " + canary}, {"https://host/mcp", "Basic", canary}} {
			if got := strings.Join(RedactArgs(args), " "); strings.Contains(got, canary) {
				t.Fatalf("seed %d args %q output %q", seed, args, got)
			}
		}
	}
}
