package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestMCPURLSchemeCasePreservesRemoteDeclaration(t *testing.T) {
	for _, scheme := range []string{"http", "HTTP", "HtTp", "https", "HTTPS", "hTtPs"} {
		endpoint := scheme + "://MCP.Example.com/CaseSensitive/MCP?mode=KeepCase"
		for _, tc := range []struct {
			suffix  string
			args    []string
			headers map[string]string
		}{
			{"", []string{endpoint}, nil},
			{" --header 'X-Client=KeepCase'", []string{endpoint, "--header", "X-Client=KeepCase"}, map[string]string{"X-Client": "KeepCase"}},
		} {
			t.Run(scheme+tc.suffix, func(t *testing.T) {
				entry, err := ParseArgs(tc.args)
				if err != nil || entry.Name != "mcp" || entry.Type != "http" || entry.URL != endpoint || entry.Command != "" || !reflect.DeepEqual(entry.Headers, tc.headers) {
					t.Errorf("CLI entry = %+v, err=%v", entry, err)
				}
				for _, prefix := range []string{"", "$ ", "> ", "% "} {
					draft, err := Parse(prefix + endpoint + tc.suffix)
					if err != nil || len(draft.Entries) != 1 {
						t.Fatalf("paste %q: %+v, err=%v", prefix, draft, err)
					}
					got := draft.Entries[0]
					if got.Name != "mcp" || got.Type != "http" || got.URL != endpoint || got.Command != "" || !reflect.DeepEqual(got.Headers, tc.headers) {
						t.Errorf("paste %q entry = %+v", prefix, got)
					}
					if len(draft.Risks) != 1 || draft.Risks[0].Kind != "unknown-host" || draft.Risks[0].Server != "mcp" {
						t.Errorf("paste %q disclosures = %+v", prefix, draft.Risks)
					}
				}
				manual, err := ParseArgs([]string{"manual", "--http", endpoint})
				if err != nil || manual.Name != "manual" || manual.URL != endpoint || manual.Command != "" {
					t.Errorf("explicit entry = %+v, err=%v", manual, err)
				}
			})
		}
	}
	for _, command := range []string{"https-server", "HTTP-helper", "./HTTPS-server"} {
		entry, err := ParseArgs([]string{"--", command, "--mode", "KeepCase"})
		if err != nil || entry.Command != command || entry.URL != "" || !reflect.DeepEqual(entry.Args, []string{"--mode", "KeepCase"}) {
			t.Errorf("command control = %+v, err=%v", entry, err)
		}
	}
	for _, scheme := range []string{"ftp", "file", "wss"} {
		if looksLikeRemoteURL(strings.ToUpper(scheme) + "://example.com/mcp") {
			t.Errorf("unsupported scheme %q recognized as an HTTP endpoint", scheme)
		}
	}
}
