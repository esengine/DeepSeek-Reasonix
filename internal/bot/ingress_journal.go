package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// IngressJournal provides a small durable event-id fence. The adapter writes
// the provider event before dispatch; completed records become tombstones so a
// replay after restart cannot create a second user turn.
type IngressJournal struct {
	dir      string
	mu       sync.Mutex
	inflight map[string]bool
}

type ingressRecord struct {
	EventID   string          `json:"event_id"`
	State     string          `json:"state"`
	Received  time.Time       `json:"received_at"`
	Accepted  time.Time       `json:"accepted_at,omitempty"`
	Completed time.Time       `json:"completed_at,omitempty"`
	TurnID    string          `json:"turn_id,omitempty"`
	Delivery  []string        `json:"delivery,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

const (
	IngressReceived  = "received"
	IngressAccepted  = "accepted"
	IngressCompleted = "completed"
)

func NewIngressJournal(dir string) (*IngressJournal, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("ingress journal directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &IngressJournal{dir: dir, inflight: make(map[string]bool)}, nil
}

// Begin returns duplicate=true for an in-flight or completed event. Pending
// files left by a crash are replayable and therefore return duplicate=false.
func (j *IngressJournal) Begin(eventID string, payload any) (duplicate bool, err error) {
	if j == nil {
		return false, nil
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false, nil
	}
	path := j.path(eventID)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.inflight[eventID] {
		return true, nil
	}
	if data, readErr := os.ReadFile(path); readErr == nil {
		var record ingressRecord
		if json.Unmarshal(data, &record) == nil && (record.State == IngressCompleted || j.inflight[eventID]) {
			return true, nil
		}
	}
	raw, _ := json.Marshal(payload)
	record := ingressRecord{EventID: eventID, State: IngressReceived, Received: time.Now().UTC(), Payload: raw}
	if err := j.writeLocked(path, record); err != nil {
		return false, err
	}
	j.inflight[eventID] = true
	return false, nil
}

// MarkAccepted advances an event only after the shared host has durably
// accepted the corresponding user turn. It intentionally preserves the raw
// payload so a restart can replay the exact provider event.
func (j *IngressJournal) MarkAccepted(eventID, turnID string) error {
	return j.update(eventID, func(record *ingressRecord) {
		record.State = IngressAccepted
		record.Accepted = time.Now().UTC()
		record.TurnID = strings.TrimSpace(turnID)
	})
}

// Record returns a copy of the durable state. Missing records are reported as
// a zero value, allowing callers to distinguish a provider event that has not
// reached the journal from one that is still replayable.
func (j *IngressJournal) Record(eventID string) (state string, turnID string, err error) {
	if j == nil || strings.TrimSpace(eventID) == "" {
		return "", "", nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	data, err := os.ReadFile(j.path(strings.TrimSpace(eventID)))
	if os.IsNotExist(err) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	var record ingressRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return "", "", err
	}
	return record.State, record.TurnID, nil
}

// Replayable returns received and accepted events left by a crash. The raw
// payload is intentionally returned as JSON so transports can decode it into
// their provider-specific message type without copying secrets into logs.
func (j *IngressJournal) Replayable() ([]json.RawMessage, error) {
	if j == nil {
		return []json.RawMessage{}, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, err
	}
	type item struct {
		at      time.Time
		payload json.RawMessage
	}
	items := make([]item, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(j.dir, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		var record ingressRecord
		if json.Unmarshal(data, &record) != nil || (record.State != IngressReceived && record.State != IngressAccepted) || len(record.Payload) == 0 {
			continue
		}
		items = append(items, item{at: record.Received, payload: append(json.RawMessage(nil), record.Payload...)})
	}
	slices.SortFunc(items, func(a, b item) int { return a.at.Compare(b.at) })
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		out = append(out, item.payload)
	}
	return out, nil
}

func (j *IngressJournal) Complete(eventID string) error {
	if j == nil {
		return nil
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}
	path := j.path(eventID)
	j.mu.Lock()
	defer j.mu.Unlock()
	record := ingressRecord{EventID: eventID, State: IngressCompleted, Received: time.Now().UTC(), Completed: time.Now().UTC()}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &record)
		record.State = IngressCompleted
		record.Completed = time.Now().UTC()
	}
	delete(j.inflight, eventID)
	return j.writeLocked(path, record)
}

// Abandon releases the in-process claim while keeping the durable received
// record replayable. Hosts call it when admission fails before a session inbox
// owns the turn.
func (j *IngressJournal) Abandon(eventID string) {
	if j == nil || strings.TrimSpace(eventID) == "" {
		return
	}
	j.mu.Lock()
	delete(j.inflight, strings.TrimSpace(eventID))
	j.mu.Unlock()
}

func (j *IngressJournal) update(eventID string, mutate func(*ingressRecord)) error {
	if j == nil || strings.TrimSpace(eventID) == "" {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	path := j.path(strings.TrimSpace(eventID))
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("ingress event %q is not recorded", eventID)
	}
	if err != nil {
		return err
	}
	var record ingressRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	mutate(&record)
	return j.writeLocked(path, record)
}

func (j *IngressJournal) path(eventID string) string {
	sum := sha256.Sum256([]byte(eventID))
	return filepath.Join(j.dir, hex.EncodeToString(sum[:])[:32]+".json")
}

func (j *IngressJournal) writeLocked(path string, record ingressRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(j.dir, ".ingress-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
