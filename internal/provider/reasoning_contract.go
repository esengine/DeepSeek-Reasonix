package provider

import (
	"slices"
	"strings"

	"reasonix/internal/nilutil"
)

// ReasoningState distinguishes observed empty output from absent and unsafe output.
// The zero value preserves inference for sessions written before this field existed.
type ReasoningState string

const (
	ReasoningEmpty      ReasoningState = "empty"
	ReasoningComplete   ReasoningState = "complete"
	ReasoningIncomplete ReasoningState = "incomplete"
	ReasoningTruncated  ReasoningState = "truncated"
)

// ThinkingBlock retains each Anthropic proof separately, including redacted blocks.
// Strings are immutable; old readers continue using the legacy single-block fields.
type ThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
}

// ReasoningReplayCapabilities describes wire requirements independently of whether
// a particular assistant turn requires replay. Unknown endpoints keep legacy policy.
type ReasoningReplayCapabilities struct {
	Format           string
	RequireSignature bool
	EmptyFallback    string
}

type ReasoningCapabilitiesProvider interface {
	ReasoningReplayCapabilities() ReasoningReplayCapabilities
}

func ReplayCapabilities(p Provider) ReasoningReplayCapabilities {
	if nilutil.IsNil(p) {
		return ReasoningReplayCapabilities{}
	}
	if c, ok := p.(ReasoningCapabilitiesProvider); ok {
		return c.ReasoningReplayCapabilities()
	}
	return ReasoningReplayCapabilities{}
}

// HasReplayableReasoning never treats a truncated or unfinished block as empty.
func HasReplayableReasoning(p Provider, m Message) bool {
	if !completeReplayEvidence(m) {
		return false
	}
	caps := ReplayCapabilities(p)
	if caps.RequireSignature || caps.Format == "anthropic-thinking" {
		if len(m.ThinkingBlocks) > 0 {
			for _, b := range m.ThinkingBlocks {
				if b.Type == "redacted_thinking" && b.Data != "" {
					continue
				}
				if b.Type != "thinking" || (strings.TrimSpace(b.Signature) == "" && (caps.RequireSignature || strings.TrimSpace(b.Thinking) == "")) {
					return false
				}
			}
			return true
		}
		if caps.RequireSignature || m.ReasoningSignature != "" {
			return strings.TrimSpace(m.ReasoningSignature) != ""
		}
	}
	if caps.Format == "responses-items" && slices.ContainsFunc(m.ResponsesItems, IsReplayableResponsesReasoning) {
		return true
	}

	return strings.TrimSpace(m.ReasoningContent) != ""
}

func CanReplayAssistantMessage(p Provider, m Message) bool {
	decision := DecideReasoningReplay(p, m, true)
	return decision == ReplayDirect || decision == ReplayCompatible
}

// ReplayDecision keeps protocol compatibility separate from response completeness.
type ReplayDecision string

const (
	ReplayDirect     ReplayDecision = "direct"
	ReplayCompatible ReplayDecision = "compatible"
	ReplayRecover    ReplayDecision = "recover"
	ReplayReject     ReplayDecision = "reject"
)

// DecideReasoningReplay is the common adapter contract consumed by execution.
// Incomplete proof is never replaced with an empty field.
func DecideReasoningReplay(p Provider, m Message, complete bool) ReplayDecision {
	if !RequiresAssistantReasoningReplay(p, m) {
		return ReplayDirect
	}
	if !complete || !completeReplayEvidence(m) {
		return ReplayReject
	}
	if HasReplayableReasoning(p, m) {
		return ReplayDirect
	}
	if _, ok := compatibleReplayMessage(p, m); ok {
		return ReplayCompatible
	}
	if AllowsEmptyReasoningFallback(p) {
		return ReplayCompatible
	}
	return ReplayRecover
}
