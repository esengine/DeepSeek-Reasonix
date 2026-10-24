package event

import "encoding/json"

// DetailIsPayload reports that a coded notice's Detail is the typed payload its
// sentence is worded from, so a sink that prints Text must not append it again.
func DetailIsPayload(code string) bool {
	return code == NoticeCodeUnappliedSteer || code == NoticeCodeExtensionSkipped || code == NoticeCodeInboxRecovered ||
		code == NoticeCodeInboxUnrecovered ||
		code == NoticeCodePermissionRulesDormant || code == NoticeCodeSubagentWaiting || code == NoticeCodeContextComposition || code == NoticeCodeCompactionTelemetry || code == NoticeCodeJobStarted || code == NoticeCodeJobFinished || code == NoticeCodeJobKilled || code == NoticeCodeJobFailed || code == NoticeCodeJobStalled
}

// ExtensionSkipReasonNoLiveSidecar: the extension's companion process is not running.
const ExtensionSkipReasonNoLiveSidecar = "no_live_sidecar"

// ExtensionSkipped is the Detail payload of NoticeCodeExtensionSkipped: which
// extension was left out, at which interception point, and the stable reason.
type ExtensionSkipped struct {
	Extension string `json:"extension"`
	Point     string `json:"point"`
	Reason    string `json:"reason"`
}

// Encode renders the payload as the notice's Detail.
func (p ExtensionSkipped) Encode() string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeExtensionSkipped reads the payload back; ok is false for a Detail that
// is not one, so a caller keeps the kernel's English.
func DecodeExtensionSkipped(detail string) (ExtensionSkipped, bool) {
	var p ExtensionSkipped
	if err := json.Unmarshal([]byte(detail), &p); err != nil || p.Extension == "" {
		return ExtensionSkipped{}, false
	}
	return p, true
}

// InboxRecovered is the Detail payload of NoticeCodeInboxRecovered: how many
// unfinished instructions the reopened inbox holds while it is paused.
type InboxRecovered struct {
	Count int `json:"count"`
}

// Encode renders the payload as the notice's Detail.
func (p InboxRecovered) Encode() string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeInboxRecovered reads the payload back; ok is false for a Detail that is
// not one, so a caller keeps the kernel's English.
func DecodeInboxRecovered(detail string) (InboxRecovered, bool) {
	var p InboxRecovered
	if err := json.Unmarshal([]byte(detail), &p); err != nil || p.Count <= 0 {
		return InboxRecovered{}, false
	}
	return p, true
}

// InboxUnrecovered is the Detail payload of NoticeCodeInboxUnrecovered: the
// queue directory whose bodies could not be read, so the queue is paused with
// contents that are unknown rather than empty.
type InboxUnrecovered struct {
	Dir string `json:"dir"`
}

// Encode renders the payload as the notice's Detail.
func (p InboxUnrecovered) Encode() string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeInboxUnrecovered reads the payload back; ok is false for a Detail that
// is not one, so a caller keeps the kernel's English.
func DecodeInboxUnrecovered(detail string) (InboxUnrecovered, bool) {
	var p InboxUnrecovered
	if err := json.Unmarshal([]byte(detail), &p); err != nil || p.Dir == "" {
		return InboxUnrecovered{}, false
	}
	return p, true
}

// JobNotice is the Detail payload of the three NoticeCodeJob* notices:
// which background job ended, by kind, id and the label it was started with.
type JobNotice struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	// Error is the job function's own error text, set on a failure only.
	Error string `json:"error,omitempty"`
	// ExitCode and Signal say how a failed process ended, when the producer
	// knew: Signal is the signal number that ended it, zero when none did.
	ExitCode *int `json:"exitCode,omitempty"`
	Signal   int  `json:"signal,omitempty"`
	// QuietSeconds is how long the job has been inactive, on a stalled notice only.
	QuietSeconds int `json:"quietSeconds,omitempty"`
	// Steps is what the job had reported before it went quiet, on a stalled
	// notice only. Zero means it never reported any, which is a different
	// situation from a job that worked and then stopped.
	Steps int `json:"steps,omitempty"`
}

// Encode renders the payload as the notice's Detail.
func (p JobNotice) Encode() string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeJobNotice reads the payload back; ok is false for a Detail that is not
// one, so a caller keeps the kernel's English.
func DecodeJobNotice(detail string) (JobNotice, bool) {
	var p JobNotice
	if err := json.Unmarshal([]byte(detail), &p); err != nil || p.ID == "" {
		return JobNotice{}, false
	}
	return p, true
}

// DormantPermissionRule is one saved rule no tool can match: the list it sits
// in, the rule as written, and the tool name it parsed to.
type DormantPermissionRule struct {
	List string `json:"list"`
	Rule string `json:"rule"`
	Tool string `json:"tool"`
}

// PermissionRulesDormant is the Detail payload of NoticeCodePermissionRulesDormant.
type PermissionRulesDormant struct {
	Rules []DormantPermissionRule `json:"rules"`
}

// Encode renders the payload as the notice's Detail.
func (p PermissionRulesDormant) Encode() string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// ContextPart is one class of what the prompt holds and the tokens it costs.
type ContextPart struct {
	Name   string `json:"name"`
	Tokens int    `json:"tokens"`
}

// ContextItem is one message among the largest the prompt holds. Tool is the
// producing tool's name for a tool result; Active says it belongs to the turn
// still running, which a fold cannot take.
type ContextItem struct {
	Role   string `json:"role"`
	Tool   string `json:"tool,omitempty"`
	Tokens int    `json:"tokens"`
	Active bool   `json:"active,omitempty"`
}

// ContextComposition is the Detail payload of NoticeCodeContextComposition: what
// holds the tokens of a prompt that is over its compaction threshold. Parts are
// the six classes by size, largest first; Largest is the biggest single messages.
type ContextComposition struct {
	Total   int           `json:"total"`
	At      int           `json:"at"`
	Active  int           `json:"active"`
	Parts   []ContextPart `json:"parts"`
	Largest []ContextItem `json:"largest,omitempty"`
}

// Encode renders the payload as the notice's Detail.
func (p ContextComposition) Encode() string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// SubagentWaiting is the Detail payload of NoticeCodeSubagentWaiting: which
// sub-agent is held back, whose write claim holds it (empty when that run gave
// no name), and whether it is the two whole-workspace claims of writers that
// declared no write_paths.
type SubagentWaiting struct {
	Task  string `json:"task,omitempty"`
	By    string `json:"by,omitempty"`
	Whole bool   `json:"whole,omitempty"`
}

// Encode renders the payload as the notice's Detail.
func (p SubagentWaiting) Encode() string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// CompactionNotice is the Detail payload of NoticeCodeCompactionTelemetry: how a
// finished fold went, as the numbers a reader can word in their own language.
// Mode and Trigger are the fold's own identifiers (summarized, degraded, native,
// snip; pressure, manual, overflow, snip, tool), never sentences.
type CompactionNotice struct {
	Trigger          string `json:"trigger"`
	Mode             string `json:"mode"`
	CacheState       string `json:"cache_state,omitempty"`
	SourceTokens     int    `json:"source_tokens"`
	FoldTokens       int    `json:"fold_tokens"`
	Spans            int    `json:"spans"`
	ProjectionTokens int    `json:"projection_tokens"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	CacheHitTokens   int    `json:"cache_hit_tokens"`
	CacheMissTokens  int    `json:"cache_miss_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
	RequestCount     int    `json:"request_count"`
	UserTurnsKept    int    `json:"user_turns_kept"`
	UserTurnsDropped int    `json:"user_turns_dropped"`
	// ProviderRequestID names the summary request, for support to look up.
	ProviderRequestID string `json:"provider_request_id,omitempty"`
	// Error is the summarizer's failure class on a degraded fold, empty otherwise.
	Error string `json:"error,omitempty"`
}

// Encode renders the payload as the notice's Detail.
func (p CompactionNotice) Encode() string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeCompactionNotice reads the payload back; ok is false for a Detail that
// is not one, so a caller keeps the kernel's English.
func DecodeCompactionNotice(detail string) (CompactionNotice, bool) {
	var p CompactionNotice
	if err := json.Unmarshal([]byte(detail), &p); err != nil || p.Mode == "" {
		return CompactionNotice{}, false
	}
	return p, true
}
