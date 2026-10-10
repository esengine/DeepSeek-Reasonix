package event

import "encoding/json"

// DetailIsPayload reports that a coded notice's Detail is the typed payload its
// sentence is worded from, so a sink that prints Text must not append it again.
func DetailIsPayload(code string) bool {
	return code == NoticeCodeUnappliedSteer || code == NoticeCodeExtensionSkipped || code == NoticeCodeInboxRecovered ||
		code == NoticeCodePermissionRulesDormant || code == NoticeCodeJobFinished || code == NoticeCodeJobKilled || code == NoticeCodeJobFailed
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

// JobNotice is the Detail payload of the three NoticeCodeJob* notices:
// which background job ended, by kind, id and the label it was started with.
type JobNotice struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	// Error is the job function's own error text, set on a failure only.
	Error string `json:"error,omitempty"`
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
