package extension

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestParseSlot pins the accepted slot forms: the eight named slots, tool:
// with a bare tool name, and provider: with a name/model ref — including the
// extension-hosted plugin/<plugin>/<name>/<model> form (stage 7). Everything
// else is rejected so a typo can never open a slot nothing reads.
func TestParseSlot(t *testing.T) {
	valid := []string{
		"system_prompt", "context", "provider_request", "provider_response",
		"compaction", "session_policy", "permission", "frontend_events",
		"tool:bash", "tool:read_file", "provider:openai/gpt-5", "provider:deepseek/deepseek-chat",
		"provider:plugin/demo/fake/x",
	}
	for _, s := range valid {
		if _, err := ParseSlot(s); err != nil {
			t.Errorf("ParseSlot(%q) = %v, want ok", s, err)
		}
	}
	invalid := []string{
		"", "bogus", "SYSTEM_PROMPT", "tool:", "tool:a b", "provider:", "provider:openai",
		"provider:openai/x/y", "tool", "provider",
		"provider:plugin/demo/fake", "provider:plugin//fake/x", "provider:plugin/ /fake/x",
	}
	for _, s := range invalid {
		if _, err := ParseSlot(s); err == nil {
			t.Errorf("ParseSlot(%q) succeeded, want error", s)
		}
	}
	// Constructors produce exactly the forms ParseSlot accepts.
	if got := SlotTool("bash"); got != Slot("tool:bash") {
		t.Fatalf("SlotTool(bash) = %q", got)
	}
	if _, err := ParseSlot(string(SlotTool("bash"))); err != nil {
		t.Fatalf("SlotTool output rejected by ParseSlot: %v", err)
	}
	if got := SlotProviderRef("openai/x"); got != Slot("provider:openai/x") {
		t.Fatalf("SlotProviderRef(openai/x) = %q", got)
	}
	if _, err := ParseSlot(string(SlotProviderRef("openai/x"))); err != nil {
		t.Fatalf("SlotProviderRef output rejected by ParseSlot: %v", err)
	}
	if _, err := ParseSlot(string(SlotProviderRef("plugin/demo/fake/x"))); err != nil {
		t.Fatalf("SlotProviderRef plugin output rejected by ParseSlot: %v", err)
	}
}

// TestReplaceClaims: single ownership per slot; the second claimant gets a
// SlotConflictError naming both owners and ownership stays with the first.
func TestReplaceClaims(t *testing.T) {
	claims := NewReplaceClaims()
	ownerA := src(ScopePlugin, "pa", "plugin")
	ownerB := src(ScopePlugin, "pb", "plugin")

	if err := claims.Claim(SlotSystemPrompt, ownerA); err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if err := claims.Claim(SlotTool("bash"), ownerA); err != nil {
		t.Fatalf("tool slot claim failed: %v", err)
	}
	if err := claims.Claim(SlotProviderRef("openai/x"), ownerB); err != nil {
		t.Fatalf("provider slot claim failed: %v", err)
	}

	err := claims.Claim(SlotSystemPrompt, ownerB)
	var conflict *SlotConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("second claim error = %v, want *SlotConflictError", err)
	}
	if conflict.Slot != SlotSystemPrompt {
		t.Fatalf("conflict slot = %q, want system_prompt", conflict.Slot)
	}
	if len(conflict.Owners) != 2 || conflict.Owners[0].PluginID != "pa" || conflict.Owners[1].PluginID != "pb" {
		t.Fatalf("conflict owners = %+v, want pa then pb", conflict.Owners)
	}
	if !strings.Contains(err.Error(), "pa") || !strings.Contains(err.Error(), "pb") {
		t.Fatalf("conflict message must name both owners: %v", err)
	}

	// The failed claim must not have taken over the slot.
	if owner, ok := claims.Owner(SlotSystemPrompt); !ok || owner.PluginID != "pa" {
		t.Fatalf("owner after conflict = %+v, want pa", owner)
	}
	// Claiming an invalid slot string is an error, not a silent new slot.
	if err := claims.Claim(Slot("not-a-slot"), ownerA); err == nil {
		t.Fatal("claiming an invalid slot succeeded")
	}
	// Claims() returns a copy.
	cp := claims.Claims()
	cp[SlotSystemPrompt] = ownerB
	if owner, _ := claims.Owner(SlotSystemPrompt); owner.PluginID != "pa" {
		t.Fatal("mutating Claims() result changed the claim table")
	}
}

// claimPayload is a contribution payload that claims replacement slots.
type claimPayload struct {
	body  string
	slots []Slot
}

func (p claimPayload) ReplacementSlots() []Slot { return p.slots }

// TestBuildSlotConflict: slot claims from two contributions collide at
// resolve time even though the contributions themselves do not.
func TestBuildSlotConflict(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(
		staticContributor("a", Contribution{
			Kind: KindStrategy, ID: "strat-a",
			Source:  src(ScopePlugin, "pa", "plugin"),
			Payload: claimPayload{body: "a", slots: []Slot{SlotSystemPrompt}},
		}),
		staticContributor("b", Contribution{
			Kind: KindStrategy, ID: "strat-b",
			Source:  src(ScopePlugin, "pb", "plugin"),
			Payload: claimPayload{body: "b", slots: []Slot{SlotSystemPrompt}},
		}),
	)
	_, _, err := b.Build(context.Background())
	var conflict *SlotConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Build error = %v, want *SlotConflictError", err)
	}
	if conflict.Slot != SlotSystemPrompt {
		t.Fatalf("conflict slot = %q", conflict.Slot)
	}
}

// TestBuildSlotWinner: a single claim lands in the snapshot's Replacements.
func TestBuildSlotWinner(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(staticContributor("a", Contribution{
		Kind: KindStrategy, ID: "strat-a",
		Source:  src(ScopePlugin, "pa", "plugin"),
		Payload: claimPayload{body: "a", slots: []Slot{SlotSystemPrompt, SlotTool("bash")}},
	}))
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	repl := snap.Replacements()
	if len(repl) != 2 {
		t.Fatalf("Replacements = %v, want 2 entries", repl)
	}
	if repl[SlotSystemPrompt].PluginID != "pa" || repl[SlotTool("bash")].PluginID != "pa" {
		t.Fatalf("slot owners wrong: %v", repl)
	}
}
