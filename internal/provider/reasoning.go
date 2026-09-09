package provider

import (
	"fmt"
	"slices"
	"strings"
)

// ReasoningOption is an adapter-owned identifier, not a global effort enum.
type ReasoningOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ReasoningCapability describes one resolved endpoint/model. Empty Default
// preserves provider-default behavior; an empty option list offers no control.
type ReasoningCapability struct {
	Options []ReasoningOption `json:"options"`
	Default string            `json:"default,omitempty"`
}

type ReasoningProvider interface{ ReasoningCapability() ReasoningCapability }

// UnsupportedReasoningEffort is returned before provider I/O. Never clamp an
// explicit selection or silently replace it with the configured default.
type UnsupportedReasoningEffort struct {
	Model, Effort string
	Supported     []string
}

func (e *UnsupportedReasoningEffort) Error() string {
	return fmt.Sprintf("UNSUPPORTED_REASONING_EFFORT: model %q does not support %q (supported: %v)", e.Model, e.Effort, e.Supported)
}
func (c ReasoningCapability) IDs() []string {
	ids := make([]string, 0, len(c.Options))
	for _, option := range c.Options {
		ids = append(ids, option.ID)
	}
	return ids
}
func (c ReasoningCapability) Clone() ReasoningCapability {
	c.Options = slices.Clone(c.Options)
	return c
}
func (c ReasoningCapability) Validate(model, effort string) error {
	ids := c.IDs()
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || id == "auto" || seen[id] {
			return fmt.Errorf("INVALID_MODEL_REASONING: model %q has invalid or repeated effort ID %q", model, id)
		}
		seen[id] = true
	}
	if c.Default != "" && !slices.Contains(ids, c.Default) {
		return &UnsupportedReasoningEffort{Model: model, Effort: c.Default, Supported: ids}
	}
	if effort == "" {
		return nil
	}
	if !slices.Contains(c.IDs(), effort) {
		return &UnsupportedReasoningEffort{Model: model, Effort: effort, Supported: c.IDs()}
	}
	return nil
}
func ReasoningOptions(def string, ids ...string) ReasoningCapability {
	c := ReasoningCapability{Options: make([]ReasoningOption, 0, len(ids)), Default: def}
	for _, id := range ids {
		c.Options = append(c.Options, ReasoningOption{ID: id, Name: id})
	}
	return c
}

// DeclaredReasoning replaces fallback vocabulary only when a deployment has
// explicitly declared it. The adapter still owns the serializer and policy.
func DeclaredReasoning(cfg Config, fallback ReasoningCapability) ReasoningCapability {
	ids, _ := cfg.Extra["supported_efforts"].([]string)
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != "" && strings.TrimSpace(id) != "auto" && !slices.Contains(clean, id) {
			clean = append(clean, id)
		}
	}
	ids = clean
	if len(ids) > 0 {
		fallback = ReasoningOptions(ids[0], ids...)
	}
	if def, _ := cfg.Extra["default_effort"].(string); def != "" {
		fallback.Default = def
	}
	return fallback
}

// Reasoning factories register alongside adapter factories during init. They
// consume non-secret configuration and must not perform I/O.
var reasoningRegistry = map[string]func(Config) ReasoningCapability{}

func RegisterReasoning(kind string, resolve func(Config) ReasoningCapability) {
	if _, exists := reasoningRegistry[kind]; exists {
		panic("duplicate reasoning adapter: " + kind)
	}
	reasoningRegistry[kind] = resolve
}
func ReasoningForConfig(kind string, cfg Config) ReasoningCapability {
	if resolve, ok := reasoningRegistry[kind]; ok {
		return resolve(cfg).Clone()
	}
	return ReasoningOptions("")
}

// RestrictReasoning keeps deployment declarations within a fixed wire protocol.
func RestrictReasoning(c ReasoningCapability, ids ...string) ReasoningCapability {
	options := make([]ReasoningOption, 0, len(c.Options))
	for _, option := range c.Options {
		if slices.Contains(ids, option.ID) {
			options = append(options, option)
		}
	}
	c.Options = options
	return c
}

// PreferredReasoning is for host-owned automatic policies only. Explicit user
// selections must use Validate and report rejection, never this fallback.
func PreferredReasoning(p Provider, id string) string {
	if owner, ok := p.(ReasoningProvider); ok && owner.ReasoningCapability().Validate("", id) == nil {
		return id
	}
	return ""
}
