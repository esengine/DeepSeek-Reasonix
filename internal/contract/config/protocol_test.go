package config

import (
	"slices"
	"testing"

	"reasonix/internal/contract/provider"
	_ "reasonix/internal/model/anthropic"
	_ "reasonix/internal/model/openai"
	_ "reasonix/internal/model/responses"
)

// The catalog is what every frontend offers and the registry is what the kernel
// can build a conversation on. A chat kind in one and not the other is a wire
// nobody can pick or a menu entry that fails on the first turn. A decision wire
// holds no conversation, so a factory for it would be a chat client that cannot
// chat: it is built by the decision path instead, which internal/assembly/boot holds.
func TestCatalogAndRegistryDescribeTheSameWires(t *testing.T) {
	registered := provider.Kinds()
	for _, p := range Protocols() {
		chat := p.Answers != AnswersDecision
		if chat && !slices.Contains(registered, p.Kind) {
			t.Errorf("catalog offers chat wire %q, which no provider factory registers", p.Kind)
		}
		if !chat && slices.Contains(registered, p.Kind) {
			t.Errorf("decision wire %q has a chat factory, so it can be chosen as a main model", p.Kind)
		}
	}
	for _, kind := range registered {
		if _, ok := ProtocolFor(kind); !ok {
			t.Errorf("kind %q is registered but absent from the protocol catalog, so nothing can offer it", kind)
		}
	}
}

// Every protocol says what it produces, or a chooser has to guess from the kind.
func TestEveryProtocolDeclaresWhatItAnswers(t *testing.T) {
	for _, p := range Protocols() {
		if p.Answers != AnswersChat && p.Answers != AnswersDecision {
			t.Errorf("protocol %q answers %q, which is neither chat nor decision", p.Kind, p.Answers)
		}
	}
	if AnswersFor("nonesuch") != AnswersChat {
		t.Error("an unknown kind must read as chat, which is what every wire was before decision backends")
	}
}

func TestProtocolsDiscoveredAsGroupsWiresByTheirListing(t *testing.T) {
	openai := ProtocolsDiscoveredAs("openai")
	if !slices.Contains(openai, "openai") || !slices.Contains(openai, "responses") {
		t.Fatalf("openai listing offers %v, want the chat wire beside the Responses API", openai)
	}
	if slices.Contains(openai, "anthropic") {
		t.Fatalf("openai listing offers %v, which reaches a different chat contract", openai)
	}
	if got := ProtocolsDiscoveredAs("nonesuch"); got != nil {
		t.Fatalf("unknown kind discovered as %v, want nothing", got)
	}
}

// A Responses source answers the OpenAI model listing, so probing one and
// comparing kinds would report a protocol change that never happened.
func TestProtocolAnswerMatchesReadsTheListingFamily(t *testing.T) {
	for _, tc := range []struct {
		declared, probed string
		want             bool
	}{
		{"responses", "openai", true},
		{"openai", "openai", true},
		{"anthropic", "anthropic", true},
		{"anthropic", "openai", false},
		{"responses", "anthropic", false},
		{"responses", "nonesuch", false},
	} {
		if got := ProtocolAnswerMatches(tc.declared, tc.probed); got != tc.want {
			t.Errorf("ProtocolAnswerMatches(%q, %q) = %v, want %v", tc.declared, tc.probed, got, tc.want)
		}
	}
}

// The alias resolves to the wire it names, so a config spelling it gets the
// capabilities that wire actually has rather than an unknown kind's none.
func TestRegistryAliasResolvesToItsWire(t *testing.T) {
	p, ok := ProtocolFor("dashscope-responses")
	if !ok || p.Kind != "responses" || !p.ServerWebSearch {
		t.Fatalf("ProtocolFor(dashscope-responses) = %#v, %v", p, ok)
	}
	if slices.ContainsFunc(Protocols(), func(c Protocol) bool { return c.Kind == "dashscope-responses" }) {
		t.Error("an alias is offered as an independent choice")
	}
}

func TestProtocolCapabilitiesDriveTheEntryQueries(t *testing.T) {
	if SupportsServerWebSearch(&ProviderEntry{Kind: "openai"}) {
		t.Error("the chat wire has no provider-executed search format")
	}
	for _, kind := range []string{"responses", "anthropic"} {
		if !SupportsServerWebSearch(&ProviderEntry{Kind: kind}) {
			t.Errorf("%s carries a provider-executed search tool", kind)
		}
	}
	if !CanConfigureThinkingParams(&ProviderEntry{Kind: "openai"}) {
		t.Error("reasoning_protocol governs the chat wire")
	}
	if CanConfigureThinkingParams(&ProviderEntry{Kind: "responses"}) {
		t.Error("the Responses wire carries reasoning.effort, not the pinned chat shape")
	}
}
