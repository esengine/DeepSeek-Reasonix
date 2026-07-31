package safety

import (
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"sync"
)

// PolicyGate evaluates messages against configured policies.
type PolicyGate struct {
	mu  sync.RWMutex
	cfg types.PolicyConfig
	bot *types.BotIdentity
}

// NewPolicyGate creates a new PolicyGate.
func NewPolicyGate(cfg *types.PolicyConfig, bot *types.BotIdentity) *PolicyGate {
	pg := &PolicyGate{
		bot: bot,
	}
	if cfg != nil {
		pg.cfg = *cfg
	}
	return pg
}

// Evaluate evaluates a message against the policy config.
func (pg *PolicyGate) Evaluate(msg *types.NormalizedMessage) types.PolicyDecision {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	if msg.ChatType == "group" {
		return pg.evaluateGroup(msg)
	}
	return pg.evaluateDm(msg)
}

func (pg *PolicyGate) evaluateGroup(msg *types.NormalizedMessage) types.PolicyDecision {
	if len(pg.cfg.GroupAllowlist) > 0 {
		allowed := false
		for _, id := range pg.cfg.GroupAllowlist {
			if id == msg.ChatID {
				allowed = true
				break
			}
		}
		if !allowed {
			return types.PolicyDecision{Allowed: false, Reason: types.RejectReasonGroupNotAllowed}
		}
	}

	requireMention := true
	if pg.cfg.RequireMention != nil {
		requireMention = *pg.cfg.RequireMention
	}
	if requireMention && !msg.MentionedBot && !msg.MentionAll {
		return types.PolicyDecision{Allowed: false, Reason: types.RejectReasonNoMention}
	}

	respondToMentionAll := false
	if pg.cfg.RespondToMentionAll != nil {
		respondToMentionAll = *pg.cfg.RespondToMentionAll
	}
	if msg.MentionAll && !respondToMentionAll {
		return types.PolicyDecision{Allowed: false, Reason: types.RejectReasonMentionAll}
	}

	return types.PolicyDecision{Allowed: true}
}

func (pg *PolicyGate) evaluateDm(msg *types.NormalizedMessage) types.PolicyDecision {
	mode := pg.cfg.DMMode
	if mode == "" {
		mode = "open"
	}

	if mode == "disabled" {
		return types.PolicyDecision{Allowed: false, Reason: types.RejectReasonDMDisabled}
	}

	if mode == "allowlist" {
		allowed := false
		for _, id := range pg.cfg.DMAllowlist {
			if id == msg.UserID {
				allowed = true
				break
			}
		}
		if !allowed {
			return types.PolicyDecision{Allowed: false, Reason: types.RejectReasonSenderNotAllowed}
		}
	}

	return types.PolicyDecision{Allowed: true}
}

// UpdateConfig updates the policy configuration.
func (pg *PolicyGate) UpdateConfig(partial types.PolicyConfig) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	// Need to clear GroupAllowlist if it is explicitly set to empty to override previous tests
	if partial.GroupAllowlist != nil {
		if len(partial.GroupAllowlist) == 0 {
			pg.cfg.GroupAllowlist = nil
		} else {
			pg.cfg.GroupAllowlist = partial.GroupAllowlist
		}
	}
	if partial.RequireMention != nil {
		pg.cfg.RequireMention = partial.RequireMention
	}
	if partial.RespondToMentionAll != nil {
		pg.cfg.RespondToMentionAll = partial.RespondToMentionAll
	}
	if partial.DMMode != "" {
		pg.cfg.DMMode = partial.DMMode
	}
	if partial.DMAllowlist != nil {
		if len(partial.DMAllowlist) == 0 {
			pg.cfg.DMAllowlist = nil
		} else {
			pg.cfg.DMAllowlist = partial.DMAllowlist
		}
	}
}

// GetConfig returns the current policy configuration.
func (pg *PolicyGate) GetConfig() types.PolicyConfig {
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	return pg.cfg
}

// SetBotIdentity updates the bot identity.
func (pg *PolicyGate) SetBotIdentity(bot *types.BotIdentity) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	pg.bot = bot
}

// GetBotIdentity returns the current bot identity.
func (pg *PolicyGate) GetBotIdentity() *types.BotIdentity {
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	return pg.bot
}
