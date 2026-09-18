package session

import (
	"encoding/json"
	"fmt"
	"maps"
)

// SubmissionReceipt is host-only metadata in an optional event. Keeping it out
// of provider.Message lets older strict message decoders read new sessions.
type SubmissionReceipt struct {
	FingerprintVersion        int    `json:"fingerprintVersion,omitempty"`
	AcceptedAttachmentDigests string `json:"acceptedAttachmentDigests,omitempty"`
	SessionID                 string `json:"sessionId"`
	SubmissionID              string `json:"submissionId"`
	Fingerprint               string `json:"fingerprint"`
	TurnID                    string `json:"turnId"`
	MessageID                 string `json:"messageId"`
}

// Immutable indexes are shared by projection snapshots. Only admission clones
// them; streaming progress must not copy the entire submission history.
type SubmissionIndex struct {
	byID      map[string]SubmissionReceipt
	byTurn    map[string]SubmissionReceipt
	byMessage map[string]SubmissionReceipt
}

// Lookup returns an immutable receipt from a query projection.
func (index SubmissionIndex) Lookup(sessionID, submissionID string) (SubmissionReceipt, bool) {
	receipt, ok := index.byID[sessionID+"\x00"+submissionID]
	return receipt, ok
}

func attachSubmissionEntries(index SubmissionIndex, sessionID string, entries []PersistentMessage) {
	for i := range entries {
		if receipt, ok := index.byMessage[sessionID+"\x00"+entries[i].MessageID]; ok {
			entries[i].SubmissionID = receipt.SubmissionID
		}
	}
}

func (index SubmissionIndex) MarshalJSON() ([]byte, error) { return json.Marshal(index.byID) }

func (index *SubmissionIndex) UnmarshalJSON(data []byte) error {
	var receipts map[string]SubmissionReceipt
	if err := json.Unmarshal(data, &receipts); err != nil {
		return err
	}
	next := SubmissionIndex{byID: receipts, byTurn: make(map[string]SubmissionReceipt), byMessage: make(map[string]SubmissionReceipt)}
	for key, receipt := range receipts {
		if key != receipt.SessionID+"\x00"+receipt.SubmissionID || receipt.TurnID == "" || receipt.MessageID == "" {
			return fmt.Errorf("invalid cached submission identity")
		}
		next.byTurn[receipt.SessionID+"\x00"+receipt.TurnID] = receipt
		next.byMessage[receipt.SessionID+"\x00"+receipt.MessageID] = receipt
	}
	*index = next
	return nil
}

func (s *Session) SubmissionForTurn(turnID string) (SubmissionReceipt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receipt, ok := s.projection.Submissions.byTurn[s.id+"\x00"+turnID]
	return receipt, ok
}

func (s *Session) Submission(id string) (SubmissionReceipt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receipt, ok := s.projection.Submissions.byID[s.id+"\x00"+id]
	return receipt, ok
}

func projectSubmission(p *Projection, commit Commit, ev Event) error {
	var receipt SubmissionReceipt
	if err := json.Unmarshal(ev.Payload, &receipt); err != nil {
		return err
	}
	if receipt.SessionID == "" || receipt.SubmissionID == "" || receipt.Fingerprint == "" || receipt.MessageID == "" || receipt.TurnID == "" || receipt.TurnID != commit.TurnID {
		return fmt.Errorf("invalid submission receipt")
	}
	key := receipt.SessionID + "\x00" + receipt.SubmissionID
	if prior, ok := p.Submissions.byID[key]; ok && prior != receipt {
		return fmt.Errorf("conflicting submission receipt")
	}
	index := SubmissionIndex{maps.Clone(p.Submissions.byID), maps.Clone(p.Submissions.byTurn), maps.Clone(p.Submissions.byMessage)}
	if index.byID == nil {
		index.byID = make(map[string]SubmissionReceipt)
		index.byTurn = make(map[string]SubmissionReceipt)
		index.byMessage = make(map[string]SubmissionReceipt)
	}
	index.byID[key] = receipt
	index.byTurn[receipt.SessionID+"\x00"+receipt.TurnID] = receipt
	index.byMessage[receipt.SessionID+"\x00"+receipt.MessageID] = receipt
	p.Submissions = index
	return nil
}
