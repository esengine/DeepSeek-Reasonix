package sessionstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

type SubagentStatus string

const (
	SubagentRunning   SubagentStatus = "running"
	SubagentCompleted SubagentStatus = "completed"
	SubagentFailed    SubagentStatus = "failed"
	// SubagentCancelled is a run the caller stopped: a cancelled context or an
	// expired deadline. It is not failed — nothing about the work went wrong —
	// and it is not interrupted, which is what a lost owner leaves behind.
	SubagentCancelled   SubagentStatus = "cancelled"
	SubagentInterrupted SubagentStatus = "interrupted"
)

// Terminal reasons refine a status without splitting it. The run graph maps a
// cancelled context and an expired deadline onto one state, so the store keeps
// one status and records which of them it was beside it.
const (
	TerminalCancelled = "cancel"
	TerminalDeadline  = "deadline"
)

// SubagentMeta is the sidecar for a persisted sub-agent transcript. It captures
// the execution identity that must stay stable for continuation/fork.
type SubagentMeta struct {
	Ref       string         `json:"ref"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Status    SubagentStatus `json:"status"`
	// TerminalReason refines a terminal status the way its producer meant it,
	// so a reader tells a deadline from a cancellation without a second status.
	TerminalReason string `json:"terminalReason,omitempty"`
	Kind           string `json:"kind"` // task | skill
	Name           string `json:"name"`
	// ExecutionID is which execution this transcript belongs to — a different
	// question from ParentToolCallID, which names the provider-visible call it
	// descends from. Empty is a record written before the two were told apart.
	ExecutionID      string   `json:"executionId,omitempty"`
	WorkspaceRoot    string   `json:"workspaceRoot"`
	ParentSession    string   `json:"parentSession,omitempty"`
	ParentToolCallID string   `json:"parentToolCallId,omitempty"`
	ForkedFrom       string   `json:"forkedFrom,omitempty"`
	SystemPromptHash string   `json:"systemPromptHash"`
	ToolScope        []string `json:"toolScope"`
	ToolSchemaHash   string   `json:"toolSchemaHash"`
	Model            string   `json:"model"`
	Effort           string   `json:"effort"`
	// Capsule records what context this run was given; CapsuleHash is its
	// stable identity for comparing two runs.
	Capsule     ContextCapsule `json:"capsule"`
	CapsuleHash string         `json:"capsuleHash"`
}

// SubagentMetaDecodeError distinguishes malformed metadata content from file
// I/O failures. Cleanup may safely skip one undecodable record, but storage
// errors must remain visible because they can affect every subagent record.
type SubagentMetaDecodeError struct {
	Ref string
	Err error
}

func IsSubagentMetaDecodeError(err error) bool {
	var decodeErr *SubagentMetaDecodeError
	return errors.As(err, &decodeErr)
}

// SubagentArtifact is a persisted sub-agent transcript and metadata pair owned
// by a parent session. One file may be missing after a crash; lifecycle cleanup
// should operate on the paths that exist.
type SubagentArtifact struct {
	Ref         string
	SessionPath string
	MetaPath    string
	Meta        SubagentMeta
}

// ListSubagentsByParent returns persisted sub-agent artifacts whose metadata
// declares the given parent session owner.
func ListSubagentsByParent(sessionDir, parentSession string) ([]SubagentArtifact, error) {
	parentSession = strings.TrimSpace(parentSession)
	if strings.TrimSpace(sessionDir) == "" || parentSession == "" {
		return nil, nil
	}
	dir := filepath.Join(sessionDir, "subagents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []SubagentArtifact{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		ref := strings.TrimSuffix(entry.Name(), ".meta.json")
		if !ValidSubagentRef(ref) {
			continue
		}
		metaPath := filepath.Join(dir, entry.Name())
		data, err := fileencoding.ReadFileUTF8(metaPath)
		if err != nil {
			return nil, err
		}
		var meta SubagentMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if strings.TrimSpace(meta.ParentSession) != parentSession {
			continue
		}
		out = append(out, SubagentArtifact{
			Ref:         ref,
			SessionPath: filepath.Join(dir, ref+".jsonl"),
			MetaPath:    metaPath,
			Meta:        meta,
		})
	}
	return out, nil
}

func ValidSubagentRef(ref string) bool {
	if !strings.HasPrefix(ref, "sa_") {
		return false
	}
	for _, r := range ref {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (e *SubagentMetaDecodeError) Error() string {
	return fmt.Sprintf("decode subagent metadata %q: %v", e.Ref, e.Err)
}

func (e *SubagentMetaDecodeError) Unwrap() error { return e.Err }

// InheritedContext states, item by item, what a child received from its parent.
// Children are isolated by construction, so every field is false unless the
// caller asked for it; this record exists so that stays a decision rather than
// an accident.
type InheritedContext struct {
	StandingInstructions bool `json:"standingInstructions"`
	Memory               bool `json:"memory"`
	ParentConversation   bool `json:"parentConversation"`
	Goal                 bool `json:"goal"`
	PlannerOutput        bool `json:"plannerOutput"`
	// UpstreamFrom names the dependencies whose answers opened this run, which
	// a bool in its place could not. Nil means none; empty means a legacy
	// record that knew there were sources without naming them.
	UpstreamFrom []string `json:"upstreamFrom"`
}

// ContextCapsule is the immutable manifest of what one child run was actually
// given. It holds references and digests, never copied parent context, so a
// later question — why did this reviewer not see that constraint? — is answered
// from the record instead of guessed from logs.
type ContextCapsule struct {
	WorkspaceRoot      string           `json:"workspaceRoot,omitempty"`
	SystemPromptSource string           `json:"systemPromptSource"`
	SystemPromptHash   string           `json:"systemPromptHash"`
	ToolScope          []string         `json:"toolScope,omitempty"`
	ToolSchemaHash     string           `json:"toolSchemaHash,omitempty"`
	Model              string           `json:"model,omitempty"`
	Effort             string           `json:"effort,omitempty"`
	ParentSession      string           `json:"parentSession,omitempty"`
	ParentToolCallID   string           `json:"parentToolCallId,omitempty"`
	ResumedFrom        string           `json:"resumedFrom,omitempty"`
	Inherited          InheritedContext `json:"inherited"`
}

// HasUpstream reports whether a dependency's answer opened this run.
func (c InheritedContext) HasUpstream() bool { return c.UpstreamFrom != nil }

// UnmarshalJSON reads sidecars written while the field was a flag. Such a
// record proves a dependency opened the run without saying which, so it decodes
// to a named-nothing slice rather than to nil: dropping it would silently
// rewrite what the record says the run was given, and inventing a source would
// be worse.
func (c *InheritedContext) UnmarshalJSON(data []byte) error {
	type plain InheritedContext
	var raw struct {
		plain
		Legacy bool `json:"upstream"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = InheritedContext(raw.plain)
	if raw.Legacy && c.UpstreamFrom == nil {
		c.UpstreamFrom = []string{}
	}
	return nil
}

// Hash is the stable identity of a capsule. Two children with the same hash saw
// the same context; a hash that moves between runs is the thing to explain.
func (c ContextCapsule) Hash() string {
	encoded, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
