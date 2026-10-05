// protocol.go — the wire formats a provider entry may speak.
package config

import (
	"slices"
	"strings"
)

// Protocol is one request/response wire format a ProviderEntry.Kind names. The
// table below is the single source of truth: a kind absent from it cannot be
// saved, and every frontend renders the choice from Protocols rather than
// keeping a list of its own.
type Protocol struct {
	Kind string
	// Answers names what a model on this wire produces, which is what decides
	// the jobs it may be given. A decision wire returns verdicts to a question
	// set and holds no conversation, so it is offered for that role alone.
	Answers Answers
	// Discovery names the model-listing shape an endpoint answers under.
	// Protocols sharing one value are indistinguishable to a probe.
	Discovery string
	// ServerWebSearch marks wires with a format for a provider-executed search
	// tool. OpenAI Chat Completions has none: DeepSeek's documented
	// chat-completions tool contract carries functions only.
	ServerWebSearch bool
	// ReasoningParams marks wires whose thinking/reasoning_effort fields a
	// relay can reject outright, so reasoning_protocol governs the shape.
	ReasoningParams bool
	// StatefulContinuation marks wires that can carry context by reference
	// rather than replaying it, which an endpoint may not honour — so
	// responses_mode governs it. Chat Completions replays by construction.
	StatefulContinuation bool
}

// protocols is ordered as a chooser should offer them: the common wire first,
// then the ones a particular endpoint adds on top of it.
var protocols = []Protocol{
	{Kind: "openai", Answers: AnswersChat, Discovery: "openai", ReasoningParams: true},
	{Kind: "responses", Answers: AnswersChat, Discovery: "openai", ServerWebSearch: true, StatefulContinuation: true},
	{Kind: "anthropic", Answers: AnswersChat, Discovery: "anthropic", ServerWebSearch: true},
	// No Discovery: the decision API has no model listing, so the model id is
	// declared rather than probed for.
	{Kind: "typesafe", Answers: AnswersDecision},
}

// protocolAliases are registry keys that resolve to a protocol above but are
// not an independent choice, so a hand-written config spelling one gets the
// same capabilities the wire actually has.
var protocolAliases = map[string]string{"dashscope-responses": "responses"}

// Answers is what a wire produces. Declared per protocol, never inferred from
// a kind's spelling.
type Answers string

const (
	AnswersChat     Answers = "chat"
	AnswersDecision Answers = "decision"
)

// AnswersFor reports what a configured kind produces. An unknown kind answers
// chat: it is what every wire before decision backends produced, so a config
// written against an older build keeps the meaning it was written with.
func AnswersFor(kind string) Answers {
	if p, ok := ProtocolFor(kind); ok && p.Answers != "" {
		return p.Answers
	}
	return AnswersChat
}

// Protocols returns the wire formats a chooser may offer, in display order.
func Protocols() []Protocol {
	return slices.Clone(protocols)
}

// ProtocolFor resolves a config kind, following aliases.
func ProtocolFor(kind string) (Protocol, bool) {
	k := strings.ToLower(strings.TrimSpace(kind))
	if alias, ok := protocolAliases[k]; ok {
		k = alias
	}
	for _, p := range protocols {
		if p.Kind == k {
			return p, true
		}
	}
	return Protocol{}, false
}

// SupportedProviderKind reports whether a kind names a wire Reasonix speaks.
func SupportedProviderKind(kind string) bool {
	_, ok := ProtocolFor(kind)
	return ok
}

// ProtocolsDiscoveredAs returns the kinds an endpoint answering under this
// listing shape may be driven with. A listing proves the account and the model
// ids, never which chat contract lives at the base URL, so the alternatives are
// offered rather than guessed between.
func ProtocolsDiscoveredAs(kind string) []string {
	p, ok := ProtocolFor(kind)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(protocols))
	for _, candidate := range protocols {
		if candidate.Discovery == p.Discovery {
			out = append(out, candidate.Kind)
		}
	}
	return out
}

// ProtocolAnswerMatches reports whether a probe result is consistent with the
// kind an entry declares. Sharing a listing shape is the whole test: a
// Responses entry answers the OpenAI listing, and calling that a mismatch would
// warn about a gateway that never changed.
func ProtocolAnswerMatches(declared, probed string) bool {
	want, ok := ProtocolFor(declared)
	got, found := ProtocolFor(probed)
	return ok && found && want.Discovery == got.Discovery
}
