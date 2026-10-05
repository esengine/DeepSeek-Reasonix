package mcpsetup

import (
	"strings"
	"testing"
)

// The three shapes a server is handed out in all have to land on the same entry,
// because the user does not know which one they have — they have whatever the
// docs printed.
func TestParseAcceptsEveryShapeDocsHandOut(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		server  string
		command string
		url     string
	}{
		{
			name:    "claude mcpServers block",
			input:   `{"mcpServers":{"github":{"command":"npx","args":["-y","@modelcontextprotocol/server-github"]}}}`,
			server:  "github",
			command: "npx",
		},
		{
			name:    "bare server map",
			input:   `{"github":{"command":"npx","args":["-y","server-github"]}}`,
			server:  "github",
			command: "npx",
		},
		{
			name:    "readme command line",
			input:   "npx -y chrome-devtools-mcp@latest",
			server:  "chrome-devtools-mcp",
			command: "npx",
		},
		{
			name:    "copied terminal line",
			input:   "$ npx -y chrome-devtools-mcp@latest",
			server:  "chrome-devtools-mcp",
			command: "npx",
		},
		{
			name:   "hosted url",
			input:  "https://mcp.example.com/sse",
			server: "mcp",
			url:    "https://mcp.example.com/sse",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q) failed: %v", tc.input, err)
			}
			if len(draft.Entries) != 1 {
				t.Fatalf("got %d entries, want 1", len(draft.Entries))
			}
			e := draft.Entries[0]
			if e.Name != tc.server {
				t.Errorf("name = %q, want %q", e.Name, tc.server)
			}
			if e.Command != tc.command {
				t.Errorf("command = %q, want %q", e.Command, tc.command)
			}
			if e.URL != tc.url {
				t.Errorf("url = %q, want %q", e.URL, tc.url)
			}
		})
	}
}

// A literal token in a pasted block is the risk worth interrupting for: it ends
// up in a file that gets synced and screenshotted. A ${VAR} reference does not.
func TestParseFlagsLiteralSecretsOnly(t *testing.T) {
	draft, err := Parse(`{"mcpServers":{"gh":{"command":"npx","env":{"GITHUB_TOKEN":"ghp_realLookingSecret"}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	var found *Risk
	for i := range draft.Risks {
		if draft.Risks[i].Kind == "secret" {
			found = &draft.Risks[i]
		}
	}
	if found == nil {
		t.Fatal("a literal token in env raised no secret risk")
	}
	if found.Field != "env.GITHUB_TOKEN" {
		t.Errorf("risk field = %q, want env.GITHUB_TOKEN", found.Field)
	}
	if !strings.Contains(found.Detail, "${GITHUB_TOKEN}") {
		t.Errorf("risk detail does not offer the reference form: %q", found.Detail)
	}

	referenced, err := Parse(`{"mcpServers":{"gh":{"command":"npx","env":{"GITHUB_TOKEN":"${GITHUB_TOKEN}"}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range referenced.Risks {
		if k.Kind == "secret" {
			t.Errorf("a ${VAR} reference was reported as a leaked secret: %+v", k)
		}
	}
}

// Every install runs something on the user's machine. The card has to be able to
// say what, so a shell or host risk is always present — it is disclosure, not an
// alarm.
func TestParseAlwaysDisclosesWhatWillRun(t *testing.T) {
	stdio, err := Parse("npx -y some-mcp")
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(stdio.Risks, "shell") {
		t.Error("a stdio server disclosed no command")
	}
	remote, err := Parse("https://mcp.example.com/sse?api_key=secret123")
	if err != nil {
		t.Fatal(err)
	}
	host := riskOf(remote.Risks, "unknown-host")
	if host == nil {
		t.Fatal("a hosted server disclosed no endpoint")
	}
	if strings.Contains(host.Detail, "secret123") {
		t.Errorf("the disclosed endpoint leaked its key: %q", host.Detail)
	}
}

func TestParseRejectsWhatItCannotResolve(t *testing.T) {
	for _, in := range []string{"", "   ", "{}", `{"not":"a server"}`} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", in)
		}
	}
}

func hasKind(risks []Risk, kind string) bool { return riskOf(risks, kind) != nil }

func riskOf(risks []Risk, kind string) *Risk {
	for i := range risks {
		if risks[i].Kind == kind {
			return &risks[i]
		}
	}
	return nil
}
