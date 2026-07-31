package types

import (
	"time"
)

// RejectReason defines why a message was rejected by the policy gate.
type RejectReason string

const (
	RejectReasonGroupNotAllowed  RejectReason = "group_not_allowed"
	RejectReasonNoMention        RejectReason = "no_mention"
	RejectReasonMentionAll       RejectReason = "mention_all_blocked"
	RejectReasonDMDisabled       RejectReason = "dm_disabled"
	RejectReasonSenderNotAllowed RejectReason = "sender_not_allowed"
)

// PolicyDecision represents the result of evaluating a message against policies.
type PolicyDecision struct {
	Allowed bool
	Reason  RejectReason
}

// BotIdentity represents the resolved identity of the bot
type BotIdentity struct {
	OpenID         string
	UserID         string // Optional
	Name           string
	ActivateStatus int
}

// PolicyConfig configures the PolicyGate.
type PolicyConfig struct {
	GroupAllowlist      []string
	RequireMention      *bool
	RespondToMentionAll *bool
	DMMode              string // "open", "disabled", "allowlist"
	DMAllowlist         []string
}

// BatchConfig configures the ChatPipeline batching behavior.
type BatchConfig struct {
	DelayMs            time.Duration
	LongThresholdChars int
	LongDelayMs        time.Duration
	MaxMessages        int
	MaxChars           int
}

// Default configuration constants.
const (
	DefaultStaleWindow = 30 * time.Minute
	DefaultLockTTL     = 5 * time.Minute
)

// DefaultBatchConfig returns the default batching configuration.
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		DelayMs:            600 * time.Millisecond,
		LongThresholdChars: 1000,
		LongDelayMs:        2000 * time.Millisecond,
		MaxMessages:        8,
		MaxChars:           4000,
	}
}

// BatchedDispatch represents a flushed batch of messages.
type BatchedDispatch struct {
	Message   *NormalizedMessage
	SourceIDs []string
}
