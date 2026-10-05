package catalog

import (
	"reasonix/internal/contract/config"
	"slices"
	"testing"

	"reasonix/internal/model/openai"
)

// A relay serving Claude behind an OpenAI-shaped listing is invisible to
// discovery: the listing shape says "openai", and nothing in it says the
// Anthropic door is open too — so the add flow never offered it and thinking
// could not be reached at all. Such relays name their wires per model.
func TestProtocolsDeclaredByReadsTheWiresARelayNames(t *testing.T) {
	listed := []openai.ListedModel{
		{ID: "claude-opus-5", Endpoints: []string{"anthropic", "openai"}},
		{ID: "gpt-5", Endpoints: []string{"openai"}},
	}
	got := ProtocolsDeclaredBy(listed)
	if !slices.Contains(got, "anthropic") {
		t.Fatalf("declared wires = %v, want the Anthropic door the relay named", got)
	}
	// Display order, so a declared list reads like a discovered one.
	if !slices.Equal(got, []string{"openai", "anthropic"}) {
		t.Fatalf("declared wires = %v, want them in chooser order", got)
	}
}

// Most listings say nothing, and silence must not shrink the choice down from
// what discovery already established.
func TestProtocolsDeclaredByIsEmptyWhenNothingSaysAnything(t *testing.T) {
	listed := []openai.ListedModel{{ID: "gpt-5"}, {ID: "gpt-4o"}}
	if got := ProtocolsDeclaredBy(listed); len(got) != 0 {
		t.Fatalf("declared wires = %v, want none", got)
	}
	discovered := config.ProtocolsDiscoveredAs("openai")
	if got := mergeProtocolChoices(discovered, nil); !slices.Equal(got, discovered) {
		t.Fatalf("merge with nothing declared = %v, want discovery untouched %v", got, discovered)
	}
}

// A name this build has no wire for is not actionable, and a declaration is
// added to discovery rather than replacing it: naming "openai" is not a claim
// that the Responses route is shut.
func TestMergeProtocolChoicesAddsWithoutNarrowing(t *testing.T) {
	listed := []openai.ListedModel{{ID: "x", Endpoints: []string{"anthropic", "carrier-pigeon", ""}}}
	declared := ProtocolsDeclaredBy(listed)
	if !slices.Equal(declared, []string{"anthropic"}) {
		t.Fatalf("declared = %v, want the unknown name ignored", declared)
	}
	got := mergeProtocolChoices(config.ProtocolsDiscoveredAs("openai"), declared)
	for _, want := range []string{"openai", "responses", "anthropic"} {
		if !slices.Contains(got, want) {
			t.Fatalf("merged = %v, want it to still offer %q", got, want)
		}
	}
}
