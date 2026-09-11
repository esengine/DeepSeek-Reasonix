package extension

import (
	"fmt"
	"maps"
	"strings"
)

// Slot names a replaceable runtime seam. Unlike additive contributions, a
// slot has exactly one owner at a time: two packages both replacing the
// system prompt would produce a hybrid neither intended, so the kernel
// refuses the combination instead of picking one silently.
type Slot string

const (
	SlotSystemPrompt     Slot = "system_prompt"
	SlotContext          Slot = "context"
	SlotProviderRequest  Slot = "provider_request"
	SlotProviderResponse Slot = "provider_response"
	SlotCompaction       Slot = "compaction"
	SlotSessionPolicy    Slot = "session_policy"
	SlotPermission       Slot = "permission"
	SlotFrontendEvents   Slot = "frontend_events"
)

// namedSlots is the closed set of bare slot names ParseSlot accepts. The set
// is deliberately closed: a slot is a runtime seam the kernel knows how to
// honor, so inventing one in a manifest must fail loudly, not be ignored.
var namedSlots = map[Slot]bool{
	SlotSystemPrompt:     true,
	SlotContext:          true,
	SlotProviderRequest:  true,
	SlotProviderResponse: true,
	SlotCompaction:       true,
	SlotSessionPolicy:    true,
	SlotPermission:       true,
	SlotFrontendEvents:   true,
}

const (
	// slotToolPrefix namespaces per-tool replacement slots.
	slotToolPrefix = "tool:"
	// slotProviderPrefix namespaces per-provider-ref replacement slots.
	slotProviderPrefix = "provider:"
)

// SlotTool returns the replacement slot for one tool, e.g. "tool:bash".
func SlotTool(name string) Slot { return Slot(slotToolPrefix + name) }

// SlotProviderRef returns the replacement slot for one provider ref, e.g.
// "provider:openai/gpt-5".
func SlotProviderRef(ref string) Slot { return Slot(slotProviderPrefix + ref) }

// ParseSlot validates a slot string. Bare names must be one of the declared
// slots; the tool:/provider: forms must carry a well-formed target so a typo
// cannot create a slot nothing will ever read.
func ParseSlot(s string) (Slot, error) {
	if namedSlots[Slot(s)] {
		return Slot(s), nil
	}
	if rest, ok := strings.CutPrefix(s, slotToolPrefix); ok {
		if rest == "" || strings.ContainsAny(rest, " \t\n") {
			return "", fmt.Errorf("extension: invalid tool slot %q: empty or whitespace tool name", s)
		}
		return Slot(s), nil
	}
	if rest, ok := strings.CutPrefix(s, slotProviderPrefix); ok {
		if !validProviderSlotTarget(rest) {
			return "", fmt.Errorf("extension: invalid provider slot %q: want provider:<name>/<model> or provider:plugin/<plugin>/<name>/<model>", s)
		}
		return Slot(s), nil
	}
	return "", fmt.Errorf("extension: unknown slot %q", s)
}

// validProviderSlotTarget reports whether ref can name a provider:<ref>
// replacement slot: an ordinary <name>/<model> ref, or an extension-hosted
// plugin/<pluginID>/<name>/<model> ref (stage 7). Plugin providers carry the
// extra namespace segments so claims can name them without relaxing the
// ordinary ref grammar.
func validProviderSlotTarget(ref string) bool {
	if _, _, ok := splitProviderRef(ref); ok {
		return true
	}
	rest, ok := strings.CutPrefix(ref, "plugin/")
	if !ok {
		return false
	}
	pluginID, nameModel, ok := strings.Cut(rest, "/")
	if !ok || pluginID == "" || strings.ContainsAny(pluginID, " \t\n") {
		return false
	}
	_, _, ok = splitProviderRef(nameModel)
	return ok
}

// splitProviderRef splits a "name/model" ref. Providers address models by
// exactly one slash (see internal/boot/resolver.go), so refs without that
// shape are malformed rather than merely unusual.
func splitProviderRef(ref string) (name, model string, ok bool) {
	name, model, found := strings.Cut(ref, "/")
	if !found || name == "" || model == "" || strings.Contains(model, "/") {
		return "", "", false
	}
	return name, model, true
}

// IsProviderRef reports whether ref is a kernel-shaped provider ID
// ("<name>/<model>", exactly one slash). Legacy provider catalogs can carry
// refs the kernel rejects at validation time — a bare provider name, or a
// model that itself contains a slash — so assemblers wrapping those catalogs
// use this to pre-filter entries instead of failing the whole build.
func IsProviderRef(ref string) bool {
	_, _, ok := splitProviderRef(ref)
	return ok
}

// SlotClaimer is implemented by contribution payloads that replace a runtime
// seam. The kernel enforces single ownership per slot at resolve time; it
// does not judge whether a contributor was entitled to claim a slot — that
// legitimacy check (claims ⊆ what the contributor declared) belongs to the
// caller at contribution time, because only the caller knows the
// contributor's manifest.
type SlotClaimer interface {
	ReplacementSlots() []Slot
}

// SlotConflictError reports a second claimant for an already-owned slot.
type SlotConflictError struct {
	Slot   Slot
	Owners []ContributionSource
}

func (e *SlotConflictError) Error() string {
	labels := make([]string, 0, len(e.Owners))
	for _, s := range e.Owners {
		labels = append(labels, s.label())
	}
	return fmt.Sprintf("extension: replacement slot %q claimed by %v", e.Slot, labels)
}

// ReplaceClaims tracks slot ownership during resolution. It is per-build
// state: the winning owners are frozen into the snapshot's Replacements map.
type ReplaceClaims struct {
	owners map[Slot]ContributionSource
}

// NewReplaceClaims returns an empty claim table.
func NewReplaceClaims() *ReplaceClaims {
	return &ReplaceClaims{owners: map[Slot]ContributionSource{}}
}

// Claim records src as the owner of slot. The second claimant for a slot gets
// a *SlotConflictError naming both owners — never a silent override. Claiming
// an invalid slot string is an error for the same reason ParseSlot rejects
// it.
func (c *ReplaceClaims) Claim(slot Slot, src ContributionSource) error {
	if _, err := ParseSlot(string(slot)); err != nil {
		return err
	}
	if owner, taken := c.owners[slot]; taken {
		return &SlotConflictError{Slot: slot, Owners: []ContributionSource{owner, src}}
	}
	c.owners[slot] = src
	return nil
}

// Owner returns the current owner of slot.
func (c *ReplaceClaims) Owner(slot Slot) (ContributionSource, bool) {
	owner, ok := c.owners[slot]
	return owner, ok
}

// Claims returns a copy of the ownership table, so callers cannot mutate
// build state through the result.
func (c *ReplaceClaims) Claims() map[Slot]ContributionSource {
	out := make(map[Slot]ContributionSource, len(c.owners))
	maps.Copy(out, c.owners)
	return out
}
