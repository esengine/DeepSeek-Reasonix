package serve

import (
	"net/http"
	"os"
	"slices"
	"testing"

	"reasonix/internal/contract/config"
)

const responsesRelayConfig = `default_model = "relay/gpt-5.5"

[[providers]]
name = "relay"
kind = "responses"
base_url = "https://www.dmxapi.cn/v1"
models = ["gpt-5.5"]
default = "gpt-5.5"
api_key_env = "RICH_API_KEY"
`

func newResponsesRelayServer(t *testing.T) string {
	t.Helper()
	srv := newRichProviderServer(t)
	if err := os.WriteFile(config.UserConfigPath(), []byte(responsesRelayConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return srv.URL
}

func TestProtocolCatalogCarriesWhereEffortLands(t *testing.T) {
	base := newResponsesRelayServer(t)
	var got []struct {
		Kind            string   `json:"kind"`
		ReasoningParams bool     `json:"reasoningParams"`
		EffortField     string   `json:"effortField"`
		EffortUnder     []string `json:"effortUnder"`
	}
	getJSON(t, base+"/providers/protocols", &got)
	want := map[string]string{}
	under := map[string][]string{}
	for _, p := range config.Protocols() {
		want[p.Kind] = p.EffortField
		under[p.Kind] = p.EffortUnder
	}
	for _, p := range got {
		if p.EffortField != want[p.Kind] {
			t.Errorf("%s: effortField = %q, want the table's %q", p.Kind, p.EffortField, want[p.Kind])
		}
		if !slices.Equal(p.EffortUnder, under[p.Kind]) && len(p.EffortUnder)+len(under[p.Kind]) > 0 {
			t.Errorf("%s: effortUnder = %v, want %v", p.Kind, p.EffortUnder, under[p.Kind])
		}
		if p.Kind == "responses" && (!p.ReasoningParams || p.EffortField != "reasoning.effort") {
			t.Errorf("responses catalog row = %+v", p)
		}
	}
	if len(got) != len(want) {
		t.Errorf("catalog has %d rows, table has %d", len(got), len(want))
	}
}

func TestResponsesProviderIsDescribedByTheSameDeclarationAsChat(t *testing.T) {
	base := newResponsesRelayServer(t)
	var list []providerView
	getJSON(t, base+"/providers", &list)
	if len(list) != 1 || list[0].Kind != "responses" {
		t.Fatalf("list = %+v", list)
	}
	if !list[0].CanSetThinking || list[0].EffortField != "reasoning.effort" {
		t.Fatalf("responses entry canSetThinking=%v effortField=%q", list[0].CanSetThinking, list[0].EffortField)
	}
}

func TestResponsesProviderAcceptsTheThinkingSwitch(t *testing.T) {
	base := newResponsesRelayServer(t)
	resp := postProvider(t, base, "/providers/thinking", `{"name":"relay","on":false}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/thinking on a responses entry = %d, want 204", resp.StatusCode)
	}
	entry, _ := loadEntry(t, "relay/gpt-5.5")
	if entry.ReasoningProtocol != "none" {
		t.Fatalf("reasoning_protocol = %q, want none", entry.ReasoningProtocol)
	}
}

func TestResponsesProviderSavesItsEffortDeclaration(t *testing.T) {
	base := newResponsesRelayServer(t)
	resp := postProvider(t, base, "/providers/edit", `{
		"name":"relay","models":["gpt-5.5"],"default":"gpt-5.5","vision":[],
		"reasoningProtocol":"openai","supportedEfforts":["low","high","xhigh"],"defaultEffort":"high"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("edit status = %d, want 204", resp.StatusCode)
	}
	entry, _ := loadEntry(t, "relay/gpt-5.5")
	if entry.ReasoningProtocol != "openai" || !slices.Equal(entry.SupportedEfforts, []string{"low", "high", "xhigh"}) {
		t.Fatalf("stored = %q %v", entry.ReasoningProtocol, entry.SupportedEfforts)
	}
	if got := config.EffortCapabilityForEntry(entry); !slices.Equal(got.Levels, []string{"auto", "low", "high", "xhigh"}) {
		t.Fatalf("menu = %v", got.Levels)
	}
	if _, err := config.NormalizeEffort(entry, "xhigh"); err != nil {
		t.Fatalf("/effort xhigh on the saved entry: %v", err)
	}
}

func TestProviderListWithholdsTheFieldWhenTheResolvedProtocolReshapesIt(t *testing.T) {
	base := newResponsesRelayServer(t)
	cfg := `default_model = "relay/m"

[[providers]]
name = "relay"
kind = "openai"
base_url = "https://relay.example.com/v1"
models = ["m"]
default = "m"
api_key_env = "RICH_API_KEY"
`
	for protocol, want := range map[string]string{"": "reasoning_effort", "openai": "reasoning_effort", "glm": "", "none": ""} {
		body := cfg
		if protocol != "" {
			body += "reasoning_protocol = \"" + protocol + "\"\n"
		}
		if err := os.WriteFile(config.UserConfigPath(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		var list []providerView
		getJSON(t, base+"/providers", &list)
		if len(list) != 1 || list[0].EffortField != want {
			t.Errorf("protocol %q: effortField = %+v, want %q", protocol, list, want)
		}
	}
}
