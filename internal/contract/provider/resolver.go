package provider

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnknownModel is a ref no catalog the resolver reads carries. It is one
// identity on both sides of a broker, so a caller tells it apart the same way
// whether the resolver answered here or on the machine holding the keys.
var ErrUnknownModel = errors.New("unknown model")

// Descriptor is the non-sensitive provider/model metadata shared across
// process boundaries. It intentionally contains no endpoint, credential,
// header, proxy, or environment-variable information.
type Descriptor struct {
	Ref                            string   `json:"ref"`
	DisplayName                    string   `json:"displayName,omitempty"`
	Model                          string   `json:"model,omitempty"`
	ContextWindow                  int      `json:"contextWindow,omitempty"`
	PricingCurrency                string   `json:"pricingCurrency,omitempty"`
	CacheHitPerMillion             float64  `json:"cacheHitPerMillion,omitempty"`
	InputPerMillion                float64  `json:"inputPerMillion,omitempty"`
	OutputPerMillion               float64  `json:"outputPerMillion,omitempty"`
	Vision                         bool     `json:"vision,omitempty"`
	Tools                          bool     `json:"tools,omitempty"`
	Reasoning                      bool     `json:"reasoning,omitempty"`
	Efforts                        []string `json:"efforts,omitempty"`
	DefaultEffort                  string   `json:"defaultEffort,omitempty"`
	ToolCallReasoning              bool     `json:"toolCallReasoning,omitempty"`
	ReasoningRoundTrip             bool     `json:"reasoningRoundTrip,omitempty"`
	WarnOnMissingToolCallReasoning bool     `json:"warnOnMissingToolCallReasoning,omitempty"`
	// ForcesThinking tells frontends that even the lowest effort still reasons.
	ForcesThinking bool `json:"forcesThinking,omitempty"`
	// Default marks the model a new session starts on when nobody names one.
	// The catalog's owner decides it, because only the owner knows which of
	// its models can answer.
	Default bool `json:"default,omitempty"`
}

// DefaultRef is the ref the catalog marks as its default, or "" when it marks
// none.
func DefaultRef(catalog []Descriptor) string {
	for _, d := range catalog {
		if d.Default {
			return strings.TrimSpace(d.Ref)
		}
	}
	return ""
}

// Selection identifies a catalog provider and an optional session-local
// effort override.
type Selection struct {
	Ref    string  `json:"ref"`
	Effort *string `json:"effort,omitempty"`
}

// Resolver creates providers without exposing credential material to callers.
// Remote runtimes use a Broker-backed resolver; ordinary boots keep using the
// local config-backed resolver.
type Resolver interface {
	Catalog() []Descriptor
	Resolve(Selection) (Provider, error)
}

// StaticResolver is a small deterministic test double.
type StaticResolver struct {
	Descriptors []Descriptor
	Providers   map[string]Provider
}

func (r *StaticResolver) Catalog() []Descriptor {
	if r == nil {
		return nil
	}
	out := make([]Descriptor, len(r.Descriptors))
	copy(out, r.Descriptors)
	return out
}

func (r *StaticResolver) Resolve(selection Selection) (Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("provider resolver is nil")
	}
	ref := strings.TrimSpace(selection.Ref)
	if ref == "" {
		return nil, fmt.Errorf("provider selection ref is required")
	}
	if p, ok := r.Providers[ref]; ok {
		return p, nil
	}
	for key, p := range r.Providers {
		if strings.HasPrefix(key, ref+"/") || strings.HasSuffix(key, "/"+ref) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("unknown provider ref %q", ref)
}
