package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"reasonix/internal/provider"
)

// Message ids are 128 bits in Crockford base32 (26 chars, ULID layout for new
// ids). They never reach a provider and never enter transcript digests, so a
// history with or without ids hashes and serializes identically on the wire.
const (
	messageIDLen      = 26
	messageIDAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	legacyMessageIDNS = "reasonix-legacy-entry-v1"
)

// NewMessageID mints a time-prefixed random id: 48 bits of unix milliseconds
// followed by 80 random bits, so ids sort roughly by creation while staying
// unguessable across processes.
func NewMessageID() string {
	var raw [16]byte
	ms := uint64(time.Now().UnixMilli())
	binary.BigEndian.PutUint64(raw[:8], ms<<16)
	if _, err := rand.Read(raw[6:]); err != nil {
		binary.BigEndian.PutUint64(raw[8:], uint64(time.Now().UnixNano()))
	}
	return encodeMessageID(raw)
}

func encodeMessageID(raw [16]byte) string {
	var out [messageIDLen]byte
	hi := binary.BigEndian.Uint64(raw[:8])
	lo := binary.BigEndian.Uint64(raw[8:])
	for i := messageIDLen - 1; i >= 0; i-- {
		out[i] = messageIDAlphabet[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}

// legacyMessageID derives the id of a message that predates ids. The running
// transcript digest binds it to everything before it, so two processes loading
// the same bytes agree and a moved or edited prefix cannot alias an id.
func legacyMessageID(branchID string, index int, running []byte) string {
	h := sha256.New()
	h.Write([]byte(legacyMessageIDNS))
	h.Write([]byte{0})
	h.Write([]byte(branchID))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(index)))
	h.Write([]byte{0})
	h.Write(running)
	var raw [16]byte
	copy(raw[:], h.Sum(nil))
	return encodeMessageID(raw)
}

// assignLegacyMessageIDs fills in ids for messages loaded from storage that
// carries none. Messages that already have an id keep it; the running digest
// mirrors sessionTranscriptHasher so it is independent of the ids themselves.
func assignLegacyMessageIDs(path string, msgs []provider.Message) {
	branchID := BranchID(path)
	h := sha256.New()
	for i := range msgs {
		b, err := json.Marshal(messageForSessionIdentity(msgs[i]))
		if err != nil {
			return
		}
		h.Write(b)
		h.Write([]byte{'\n'})
		if msgs[i].ID == "" {
			msgs[i].ID = legacyMessageID(branchID, i, h.Sum(nil))
		}
	}
}

func mintMessageIDs(msgs []provider.Message) {
	for i := range msgs {
		if msgs[i].ID == "" {
			msgs[i].ID = NewMessageID()
		}
	}
}

// LeafID reports the id of the newest message, or "" for an empty session.
func (s *Session) LeafID() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Messages) == 0 {
		return ""
	}
	return s.Messages[len(s.Messages)-1].ID
}

// IndexOfID resolves a message id to its current position, or -1. It scans
// from the tail because callers almost always ask about recent messages.
func (s *Session) IndexOfID(id string) int {
	if s == nil || id == "" {
		return -1
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, m := range slices.Backward(s.Messages) {
		if m.ID == id {
			return i
		}
	}
	return -1
}
