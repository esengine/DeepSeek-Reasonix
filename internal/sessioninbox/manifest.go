package sessioninbox

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxIdempotencyReceipts = 512
	idempotencyReceiptTTL  = 7 * 24 * time.Hour
	maxIdempotencyKeyBytes = 1024
)

type idempotencyReceipt struct {
	ItemID      string      `json:"itemId"`
	RequestHash string      `json:"requestHash"`
	Disposition Disposition `json:"disposition"`
	CompletedAt time.Time   `json:"completedAt"`
}

// manifest is the on-disk revisioned metadata file (no bodies).
type manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Revision      int64             `json:"revision"`
	RunID         string            `json:"runId,omitempty"`
	Paused        bool              `json:"paused"`
	Recovered     bool              `json:"recovered"`
	RecoveredN    int               `json:"recoveredCount,omitempty"`
	Items         []InboxItemMeta   `json:"items"`
	Idempotency   map[string]string `json:"idempotency,omitempty"` // key -> itemID
	// IdempotencyHashes fingerprints the original client request, excluding
	// enqueue-time reference materialization. It covers both live items and
	// aliases created by collect mode.
	IdempotencyHashes map[string]string             `json:"idempotencyHashes,omitempty"`
	Receipts          map[string]idempotencyReceipt `json:"receipts,omitempty"`
	UpdatedAt         time.Time                     `json:"updatedAt"`
}

func emptyManifest(runID string) *manifest {
	return &manifest{
		SchemaVersion:     SchemaVersion,
		RunID:             runID,
		Items:             []InboxItemMeta{},
		Idempotency:       map[string]string{},
		IdempotencyHashes: map[string]string{},
		Receipts:          map[string]idempotencyReceipt{},
		UpdatedAt:         time.Now().UTC(),
	}
}

func (m *manifest) clone() *manifest {
	if m == nil {
		return emptyManifest("")
	}
	out := *m
	out.Items = append([]InboxItemMeta(nil), m.Items...)
	if m.Idempotency != nil {
		out.Idempotency = make(map[string]string, len(m.Idempotency))
		maps.Copy(out.Idempotency, m.Idempotency)
	} else {
		out.Idempotency = map[string]string{}
	}
	if m.IdempotencyHashes != nil {
		out.IdempotencyHashes = make(map[string]string, len(m.IdempotencyHashes))
		maps.Copy(out.IdempotencyHashes, m.IdempotencyHashes)
	} else {
		out.IdempotencyHashes = map[string]string{}
	}
	if m.Receipts != nil {
		out.Receipts = make(map[string]idempotencyReceipt, len(m.Receipts))
		maps.Copy(out.Receipts, m.Receipts)
	} else {
		out.Receipts = map[string]idempotencyReceipt{}
	}
	return &out
}

func (m *manifest) totalBytes() int64 {
	var n int64
	for _, it := range m.Items {
		n += it.ByteSize
	}
	return n
}

func (m *manifest) indexOf(id string) int {
	for i, it := range m.Items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

func (m *manifest) item(id string) (InboxItemMeta, bool) {
	i := m.indexOf(id)
	if i < 0 {
		return InboxItemMeta{}, false
	}
	return m.Items[i], true
}

func (m *manifest) removeItem(id string) (InboxItemMeta, bool) {
	i := m.indexOf(id)
	if i < 0 {
		return InboxItemMeta{}, false
	}
	it := m.Items[i]
	m.Items = append(m.Items[:i], m.Items[i+1:]...)
	for key, itemID := range m.Idempotency {
		if itemID == id {
			delete(m.Idempotency, key)
			delete(m.IdempotencyHashes, key)
		}
	}
	return it, true
}

func (m *manifest) idempotencyKeysFor(id string) []string {
	keys := make([]string, 0, 1)
	for key, itemID := range m.Idempotency {
		if itemID == id {
			keys = append(keys, key)
		}
	}
	return keys
}

func (m *manifest) rememberReceipt(keys []string, itemID string, disposition Disposition, now time.Time) {
	if m.Receipts == nil {
		m.Receipts = map[string]idempotencyReceipt{}
	}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		m.Receipts[key] = idempotencyReceipt{
			ItemID:      itemID,
			RequestHash: m.IdempotencyHashes[key],
			Disposition: disposition,
			CompletedAt: now,
		}
	}
	m.pruneReceipts(now)
}

func (m *manifest) pruneReceipts(now time.Time) {
	for key, receipt := range m.Receipts {
		if receipt.CompletedAt.IsZero() || now.Sub(receipt.CompletedAt) > idempotencyReceiptTTL {
			delete(m.Receipts, key)
		}
	}
	for len(m.Receipts) > maxIdempotencyReceipts {
		oldestKey := ""
		var oldest time.Time
		for key, receipt := range m.Receipts {
			if oldestKey == "" || receipt.CompletedAt.Before(oldest) {
				oldestKey = key
				oldest = receipt.CompletedAt
			}
		}
		delete(m.Receipts, oldestKey)
	}
}

func decodeManifest(data []byte) (*manifest, error) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Items == nil {
		m.Items = []InboxItemMeta{}
	}
	if m.Idempotency == nil {
		m.Idempotency = map[string]string{}
	}
	if m.IdempotencyHashes == nil {
		m.IdempotencyHashes = map[string]string{}
	}
	if m.Receipts == nil {
		m.Receipts = map[string]idempotencyReceipt{}
	}
	if err := validateManifest(&m, m.SchemaVersion > SchemaVersion); err != nil {
		return nil, err
	}
	return &m, nil
}

func validateManifest(m *manifest, allowUnknownEnums bool) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	if m.SchemaVersion < 0 || m.Revision < 0 {
		return fmt.Errorf("invalid manifest version or revision")
	}
	ids := make(map[string]struct{}, len(m.Items))
	for _, item := range m.Items {
		if !validBlobStem(item.ID) || !validBlobStem(blobNameFor(item)) {
			return fmt.Errorf("invalid inbox item path")
		}
		if _, exists := ids[item.ID]; exists {
			return fmt.Errorf("duplicate inbox item id")
		}
		ids[item.ID] = struct{}{}
		if item.ByteSize < 0 {
			return fmt.Errorf("negative inbox item size")
		}
		if !allowUnknownEnums && (!validInboxIntent(item.Intent) || !validInboxState(item.State)) {
			return fmt.Errorf("invalid inbox item intent or state")
		}
		if item.Checksum != "" && !validSHA256(item.Checksum) {
			return fmt.Errorf("invalid inbox item checksum")
		}
	}
	if allowUnknownEnums {
		return nil
	}
	for key, id := range m.Idempotency {
		if !validIdempotencyKey(key) {
			return fmt.Errorf("invalid idempotency key")
		}
		if _, exists := ids[id]; !exists {
			return fmt.Errorf("idempotency key references missing item")
		}
		hash := m.IdempotencyHashes[key]
		if hash == "" {
			if m.SchemaVersion >= 2 {
				return fmt.Errorf("missing idempotency request hash")
			}
		} else if !validSHA256(hash) {
			return fmt.Errorf("invalid idempotency request hash")
		}
	}
	for key, receipt := range m.Receipts {
		if !validIdempotencyKey(key) || !validBlobStem(receipt.ItemID) || !validSHA256(receipt.RequestHash) {
			return fmt.Errorf("invalid idempotency receipt")
		}
	}
	return nil
}

func validBlobStem(name string) bool {
	if name == "" || strings.TrimSpace(name) != name || len(name) > 200 || name == "." || name == ".." || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return false
	}
	return !strings.ContainsAny(name, "/\\\x00")
}

func validIdempotencyKey(key string) bool {
	return key != "" && strings.TrimSpace(key) == key && len(key) <= maxIdempotencyKeyBytes && !strings.ContainsRune(key, '\x00')
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validInboxIntent(intent InboxIntent) bool {
	return intent == IntentFollowup || intent == IntentSteer
}

func validInboxState(state InboxState) bool {
	switch state {
	case StateQueued, StateSteerAccepted, StateSteerConsumed, StateRunning, StateBlocked, StateUncertain:
		return true
	default:
		return false
	}
}
