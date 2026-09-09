package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"reasonix/internal/provider"
)

// Schema 2 of <id>.events.jsonl is an append-only DAG: every message entry
// names its parent, heads are named pointers into that graph, and every other
// operation (rewind, fork, redaction, compaction, turn boundaries) is a marker
// appended behind the messages it refers to. Nothing rewrites earlier bytes
// except a generation rotation under a single-writer proof. Unknown entry
// types are a hard error, so the whole vocabulary ships with the schema.
const (
	sessionDAGSchemaVersion = 2

	sessionDAGTypeLog        = "log"
	sessionDAGTypeMessage    = "message"
	sessionDAGTypePatch      = "patch"
	sessionDAGTypeSystem     = "system"
	sessionDAGTypeFork       = "fork"
	sessionDAGTypeRewind     = "rewind"
	sessionDAGTypeSelect     = "select"
	sessionDAGTypeRename     = "rename"
	sessionDAGTypeRetire     = "retire"
	sessionDAGTypeTurnBegin  = "turn_begin"
	sessionDAGTypeTurnEnd    = "turn_end"
	sessionDAGTypeCompaction = "compaction"
	sessionDAGTypeRedact     = "redact"
	sessionDAGTypeWriter     = "writer"
	sessionDAGTypeCheckpoint = "checkpoint"

	// SessionMainHead is the head every upgraded or freshly created log starts
	// with; forks, rewinds, and concurrent writers mint new head ids.
	SessionMainHead = "main"

	HeadKindMain       = "main"
	HeadKindFork       = "fork"
	HeadKindRewind     = "rewind"
	HeadKindConcurrent = "concurrent"
)

// HeadRef is a writer's position in a schema-2 log: the head it extends, that
// head's leaf message, and the log generation/offset it last observed.
type HeadRef struct {
	HeadID        string
	LeafID        string
	LogGeneration int64
	LogOffset     int64
}

// SessionHead describes one head of a schema-2 log for listings, the versions
// UI, and the catalog. MessageCount is the length of the materialized chain.
type SessionHead struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Name         string    `json:"name,omitempty"`
	ParentHead   string    `json:"parent_head,omitempty"`
	ForkFrom     string    `json:"fork_from,omitempty"`
	Writer       string    `json:"writer,omitempty"`
	LeafID       string    `json:"leaf,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`
	Retired      bool      `json:"retired,omitempty"`
	Selected     bool      `json:"selected,omitempty"`
	Covered      bool      `json:"covered,omitempty"` // live, unselected, adds nothing beyond the selected chain
	MessageCount int       `json:"message_count"`
	Turns        int       `json:"turns,omitempty"`
	Preview      string    `json:"preview,omitempty"`
}

type sessionDAGOrigin struct {
	Session string `json:"session,omitempty"`
	Entry   string `json:"entry,omitempty"`
}

// sessionDAGEntry is the wire shape shared by every entry type; the header
// fields come first so the 4 KiB probe finds schema_version and type ahead of
// any image-bearing payload. Msgs always holds exactly one message.
type sessionDAGEntry struct {
	SchemaVersion int       `json:"schema_version"`
	Type          string    `json:"type"`
	ID            string    `json:"id,omitempty"`
	Head          string    `json:"head,omitempty"`
	Writer        string    `json:"writer,omitempty"`
	Turn          string    `json:"turn,omitempty"`
	At            time.Time `json:"at"`

	Parent string          `json:"parent,omitempty"`
	Digest string          `json:"digest,omitempty"`
	Msgs   json.RawMessage `json:"msgs,omitempty"`

	Target string `json:"target,omitempty"`

	NewHead string `json:"new_head,omitempty"`
	From    string `json:"from,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name,omitempty"`

	To     string `json:"to,omitempty"`
	Cause  string `json:"cause,omitempty"`
	Reason string `json:"reason,omitempty"`

	Leaf         string `json:"leaf,omitempty"`
	PreserveUser bool   `json:"preserve_user,omitempty"`

	CoveredLeaf  string `json:"covered_leaf,omitempty"`
	CoveredCount int    `json:"covered_count,omitempty"`
	PrefixHash   string `json:"prefix_hash,omitempty"`

	Targets map[string]json.RawMessage `json:"targets,omitempty"`

	PID             int    `json:"pid,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	LeaseGeneration uint64 `json:"lease_generation,omitempty"`

	Generation   int64             `json:"generation,omitempty"`
	RotatedFrom  int64             `json:"rotated_from,omitempty"`
	UpgradedFrom int               `json:"upgraded_from_schema,omitempty"`
	Origin       *sessionDAGOrigin `json:"origin,omitempty"`

	SelectedHead string        `json:"selected_head,omitempty"`
	Heads        []SessionHead `json:"heads,omitempty"`
	Dropped      []string      `json:"dropped,omitempty"`
	Tombstones   []string      `json:"tombstones,omitempty"`
}

// NewHeadID mints a head id with the same time-prefixed layout as message ids.
func NewHeadID() string {
	return NewMessageID()
}

// sessionDAGChainDigest is the per-entry hash chain: sha256(parent digest,
// identity JSON). Unlike the flat transcript digest it can be extended from
// any ancestor, which is what a fork needs.
func sessionDAGChainDigest(parentDigest string, m provider.Message) (string, error) {
	b, err := json.Marshal(messageForSessionIdentity(m))
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(parentDigest))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func encodeSessionDAGMessage(m provider.Message) (json.RawMessage, error) {
	b, err := json.Marshal([]provider.Message{m})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// newSessionDAGMessageEntry builds a message entry for m under head with the
// given parent; the chain digest is derived from the parent's digest.
func newSessionDAGMessageEntry(head, parent, parentDigest, turn string, m provider.Message, at time.Time) (sessionDAGEntry, error) {
	raw, err := encodeSessionDAGMessage(m)
	if err != nil {
		return sessionDAGEntry{}, err
	}
	digest, err := sessionDAGChainDigest(parentDigest, m)
	if err != nil {
		return sessionDAGEntry{}, err
	}
	return sessionDAGEntry{
		Type:   sessionDAGTypeMessage,
		ID:     m.ID,
		Head:   head,
		Turn:   turn,
		At:     at,
		Parent: parent,
		Digest: digest,
		Msgs:   raw,
	}, nil
}
