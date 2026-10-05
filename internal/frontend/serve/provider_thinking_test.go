package serve

import (
	"encoding/json"
	"net/http"
	"testing"

	"reasonix/internal/contract/config"
)

// thinkingOf reads the switch's two fields off the list the panel renders from.
func thinkingOf(t *testing.T, base, name string) (canSet, sends bool) {
	t.Helper()
	resp, err := http.Get(base + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []struct {
		Name           string `json:"name"`
		CanSetThinking bool   `json:"canSetThinking"`
		SendsThinking  bool   `json:"sendsThinking"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	for _, p := range out {
		if p.Name == name {
			return p.CanSetThinking, p.SendsThinking
		}
	}
	t.Fatalf("provider %q missing from the list", name)
	return false, false
}

// TestThinkingParamsSwitchPinsThePlainChatShape covers the relay case: a
// gateway that rejects an unknown thinking field fails every request until the
// user can turn the parameters off from the panel.
func TestThinkingParamsSwitchPinsThePlainChatShape(t *testing.T) {
	srv := newRichProviderServer(t)

	if canSet, sends := thinkingOf(t, srv.URL, "rich"); !canSet || !sends {
		t.Fatalf("initial state canSet/sends = %v/%v, want true/true", canSet, sends)
	}

	resp := postProvider(t, srv.URL, "/providers/thinking", `{"name":"rich","on":false}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/thinking = %d: %s", resp.StatusCode, b)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Provider("rich")
	if !ok {
		t.Fatal("the provider disappeared")
	}
	if entry.ReasoningProtocol != "none" {
		t.Fatalf("reasoning_protocol = %q, want none", entry.ReasoningProtocol)
	}
	// Everything the switch does not own has to survive it.
	if entry.ContextWindow != 131072 || entry.BalanceURL == "" {
		t.Fatalf("the switch flattened unrelated fields: %+v", entry)
	}
	if _, sends := thinkingOf(t, srv.URL, "rich"); sends {
		t.Fatal("the list still reports thinking parameters as sent")
	}

	back := postProvider(t, srv.URL, "/providers/thinking", `{"name":"rich","on":true}`)
	defer back.Body.Close()
	if back.StatusCode != http.StatusNoContent {
		t.Fatalf("turning it back on = %d", back.StatusCode)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ = cfg.Provider("rich")
	if entry.ReasoningProtocol != "" {
		t.Fatalf("reasoning_protocol = %q, want cleared", entry.ReasoningProtocol)
	}
}

// TestThinkingParamsReleaseKeepsAnExplicitProtocol pins the asymmetry: turning
// the switch back on clears only the plain-chat pin, never a protocol the user
// chose on purpose.
func TestThinkingParamsReleaseKeepsAnExplicitProtocol(t *testing.T) {
	entry := &config.ProviderEntry{Kind: "openai", ReasoningProtocol: "deepseek"}
	config.SetThinkingParams(entry, true)
	if entry.ReasoningProtocol != "deepseek" {
		t.Fatalf("reasoning_protocol = %q, want the explicit deepseek to survive", entry.ReasoningProtocol)
	}
	config.SetThinkingParams(entry, false)
	if entry.ReasoningProtocol != "none" {
		t.Fatalf("reasoning_protocol = %q, want none", entry.ReasoningProtocol)
	}
}

// TestThinkingParamsRefusedWhereTheWireHasNoSuchField keeps the switch off
// protocols that never carry the parameters.
func TestThinkingParamsRefusedWhereTheWireHasNoSuchField(t *testing.T) {
	if config.CanConfigureThinkingParams(&config.ProviderEntry{Kind: "anthropic"}) {
		t.Fatal("the anthropic wire was offered a switch it has no field for")
	}
}
