package evolve

import (
	"strings"
	"time"
)

// Status is the lifecycle state of one proposal.
type Status string

const (
	StatusProposed  Status = "proposed"
	StatusApplied   Status = "applied"
	StatusDiscarded Status = "discarded"
)

// Tier selects where a proposal may land.
type Tier string

const (
	// TierL0 writes project-scoped background memory (remember-style facts).
	TierL0 Tier = "L0"
	// TierL1 patches the project standing instruction doc (AGENTS.md etc.).
	TierL1 Tier = "L1"
)

// TargetKind names the durable surface a proposal writes.
type TargetKind string

const (
	TargetMemory   TargetKind = "memory"
	TargetAgentsMD TargetKind = "agents_md"
	TargetSkill    TargetKind = "skill" // reserved; not applied in this package yet
)

// Target describes the write destination for a proposal.
type Target struct {
	Kind        TargetKind `json:"kind"`
	Path        string     `json:"path,omitempty"`
	MemoryType  string     `json:"memory_type,omitempty"`  // feedback|project
	MemoryScope string     `json:"memory_scope,omitempty"` // project|global (global rejected by default)
	Section     string     `json:"section,omitempty"`      // Conventions|Notes
	MemoryName  string     `json:"memory_name,omitempty"`  // optional kebab name for L0
	MemoryTitle string     `json:"memory_title,omitempty"` // optional L0 title
	Description string     `json:"description,omitempty"`  // L0 index description
}

// Evidence ties a proposal to a concrete history hit.
type Evidence struct {
	SessionPath  string `json:"session_path"`
	MessageIndex int    `json:"message_index"`
	Kind         string `json:"kind,omitempty"`
	Quote        string `json:"quote,omitempty"`
}

// Proposal is one learn-loop suggestion awaiting human apply/discard.
type Proposal struct {
	ID          string     `json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at,omitempty"`
	ProjectRoot string     `json:"project_root,omitempty"`
	Status      Status     `json:"status"`
	Tier        Tier       `json:"tier"`
	Target      Target     `json:"target"`
	Title       string     `json:"title"`
	Why         string     `json:"why"`
	HowToApply  string     `json:"how_to_apply"`
	Body        string     `json:"body"`
	Evidence    []Evidence `json:"evidence"`
	Diff        string     `json:"diff,omitempty"`
	Confidence  string     `json:"confidence,omitempty"`
	DedupeKey   string     `json:"dedupe_key,omitempty"`
	AppliedPath string     `json:"applied_path,omitempty"`
	TailNote    string     `json:"tail_note,omitempty"`
}

// MaxL1BodyLines is the hard budget for an L1 standing-instruction patch body.
const MaxL1BodyLines = 5

// MaxEvidenceQuoteRunes caps stored quote length.
const MaxEvidenceQuoteRunes = 200

// NormalizeTier coerces tier labels; empty defaults to L0.
func NormalizeTier(s string) Tier {
	switch Tier(strings.ToUpper(strings.TrimSpace(s))) {
	case TierL1:
		return TierL1
	default:
		return TierL0
	}
}

// NormalizeStatus coerces status; empty defaults to proposed.
func NormalizeStatus(s string) Status {
	switch Status(strings.ToLower(strings.TrimSpace(s))) {
	case StatusApplied:
		return StatusApplied
	case StatusDiscarded:
		return StatusDiscarded
	default:
		return StatusProposed
	}
}
