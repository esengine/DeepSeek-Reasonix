package provider

import "errors"

// ErrNativeContextEditingUnsupported marks an explicit provider rejection of
// the native context-management request shape. Callers may use it for the one
// permitted pre-success fallback to local maintenance; unrelated 4xx errors
// must remain ordinary request failures.
var ErrNativeContextEditingUnsupported = errors.New("native context editing is unsupported")

func IsNativeContextEditingUnsupported(err error) bool {
	return errors.Is(err, ErrNativeContextEditingUnsupported)
}

// ContextEditingPolicy describes the stable Anthropic native tool-result edit
// policy. It is provider-neutral so frozen requests can include it without
// coupling agent code to Anthropic wire structs.
type ContextEditingPolicy struct {
	Mode                    string `json:"mode,omitempty"` // local | native
	TriggerInputTokens      int    `json:"triggerInputTokens,omitempty"`
	KeepToolUses            int    `json:"keepToolUses,omitempty"`
	ClearAtLeastInputTokens int    `json:"clearAtLeastInputTokens,omitempty"`
	ClearToolInputs         bool   `json:"clearToolInputs,omitempty"`
}

// ContextEditingCapabilities is explicit because protocol compatibility alone
// does not make an endpoint support Anthropic's native edit semantics.
type ContextEditingCapabilities struct {
	NativeToolUseClear bool
	PolicyVersion      string
}

type ContextEditingCapabler interface {
	ContextEditingCapabilities() ContextEditingCapabilities
}

func ContextEditingCapabilitiesOf(p Provider) ContextEditingCapabilities {
	if p == nil {
		return ContextEditingCapabilities{}
	}
	capable, ok := p.(ContextEditingCapabler)
	if !ok {
		return ContextEditingCapabilities{}
	}
	return capable.ContextEditingCapabilities()
}
