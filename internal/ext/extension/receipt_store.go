package extension

import (
	"sync"
	"time"
)

const (
	// ReceiptStore deliberately retains only recent in-process recovery evidence.
	// A generation can record many file/provider effects, so both dimensions are
	// bounded independently. Retention is not persistence: crash recovery remains
	// outside this PR's contract.
	defaultReceiptGenerationLimit    = 32
	defaultReceiptPerGenerationLimit = 256
)

type receiptGeneration struct {
	ids       []string
	truncated bool
}

// ReceiptStore is a bounded, process-local ledger of irreversible /
// compensatable effects used by recovery to decide what is safe to resume. It
// does not claim rollback success for irreversible work.
type ReceiptStore struct {
	mu                 sync.Mutex
	byID               map[string]EffectReceipt
	byGen              map[uint64]*receiptGeneration
	generationOrder    []uint64
	generationLimit    int
	perGenerationLimit int
	evictedThrough     uint64
	evictedZero        bool
	sequence           uint64
	onEvict            func(EffectReceipt)
}

// DefaultReceiptStore is the compatibility owner's ledger.
var DefaultReceiptStore = DefaultRuntimeOwner.Receipts

// NewReceiptStore returns an empty store.
func NewReceiptStore() *ReceiptStore {
	return newReceiptStore(defaultReceiptGenerationLimit, defaultReceiptPerGenerationLimit, nil)
}

func newReceiptStore(generationLimit, perGenerationLimit int, onEvict func(EffectReceipt)) *ReceiptStore {
	if generationLimit < 1 {
		generationLimit = 1
	}
	if perGenerationLimit < 1 {
		perGenerationLimit = 1
	}
	return &ReceiptStore{
		byID:               make(map[string]EffectReceipt),
		byGen:              make(map[uint64]*receiptGeneration),
		generationLimit:    generationLimit,
		perGenerationLimit: perGenerationLimit,
		onEvict:            onEvict,
	}
}

// Record inserts or updates a receipt. Irreversible receipts never set
// CompensationStatus to a successful rollback.
func (s *ReceiptStore) Record(r EffectReceipt) {
	if s == nil {
		return
	}
	if r.Class == Irreversible {
		// Never claim external work was undone.
		if r.CompensationStatus == "applied" || r.CompensationStatus == "rolled_back" {
			r.CompensationStatus = "not_applicable"
		}
		if r.CompensationStatus == "" {
			r.CompensationStatus = "not_applicable"
		}
	}
	s.mu.Lock()
	if r.ID == "" {
		s.sequence++
		r.ID = "receipt-" + itoaU64(s.sequence)
	}
	if previous, exists := s.byID[r.ID]; exists && previous.Generation != r.Generation && r.Generation != 0 {
		// Receipt IDs are update keys only within one generation. Keep both
		// generations when a caller accidentally reuses an ID.
		r.ID += "#gen-" + itoaU64(r.Generation)
	}
	if previous, exists := s.byID[r.ID]; exists {
		if r.Generation == 0 {
			r.Generation = previous.Generation
		}
		if r.Owner == "" {
			r.Owner = previous.Owner
		}
		if r.Component == "" {
			r.Component = previous.Component
		}
		if r.StartedAt.IsZero() {
			r.StartedAt = previous.StartedAt
		}
		if r.CompletedAt.IsZero() {
			r.CompletedAt = previous.CompletedAt
		}
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = time.Now().UTC()
	}
	_, exists := s.byID[r.ID]
	s.byID[r.ID] = r
	if !exists {
		bucket := s.byGen[r.Generation]
		if bucket == nil {
			bucket = &receiptGeneration{}
			s.byGen[r.Generation] = bucket
			s.generationOrder = append(s.generationOrder, r.Generation)
		}
		bucket.ids = append(bucket.ids, r.ID)
	}
	evicted := s.trimLocked()
	s.mu.Unlock()
	for _, old := range evicted {
		if s.onEvict != nil {
			s.onEvict(old)
		}
	}
}

// Get returns a receipt by id.
func (s *ReceiptStore) Get(id string) (EffectReceipt, bool) {
	if s == nil {
		return EffectReceipt{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[id]
	return r, ok
}

// ForGeneration returns all receipts for a generation.
func (s *ReceiptStore) ForGeneration(gen uint64) []EffectReceipt {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byGen[gen]
	if ids == nil {
		return nil
	}
	out := make([]EffectReceipt, 0, len(ids.ids))
	for _, id := range ids.ids {
		if r, ok := s.byID[id]; ok {
			out = append(out, r)
		}
	}
	return out
}

// trimLocked evicts whole old generations first, then the oldest receipts in
// an overfull generation. The caller must hold s.mu.
func (s *ReceiptStore) trimLocked() []EffectReceipt {
	var evicted []EffectReceipt
	for len(s.generationOrder) > s.generationLimit {
		gen := s.generationOrder[0]
		s.generationOrder = s.generationOrder[1:]
		bucket := s.byGen[gen]
		delete(s.byGen, gen)
		if gen == 0 {
			s.evictedZero = true
		} else if gen > s.evictedThrough {
			s.evictedThrough = gen
		}
		if bucket != nil {
			for _, id := range bucket.ids {
				if r, ok := s.byID[id]; ok {
					evicted = append(evicted, r)
					delete(s.byID, id)
				}
			}
		}
	}
	for gen, bucket := range s.byGen {
		if len(bucket.ids) <= s.perGenerationLimit {
			continue
		}
		extra := len(bucket.ids) - s.perGenerationLimit
		for _, id := range bucket.ids[:extra] {
			if r, ok := s.byID[id]; ok {
				evicted = append(evicted, r)
				delete(s.byID, id)
			}
		}
		bucket.ids = append([]string(nil), bucket.ids[extra:]...)
		bucket.truncated = true
		s.byGen[gen] = bucket
	}
	return evicted
}

func (s *ReceiptStore) generationSnapshot(gen uint64) ([]EffectReceipt, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.byGen[gen]
	if bucket == nil {
		truncated := (gen == 0 && s.evictedZero) || (gen != 0 && gen <= s.evictedThrough)
		return nil, truncated
	}
	ids := append([]string(nil), bucket.ids...)
	out := make([]EffectReceipt, 0, len(ids))
	for _, id := range ids {
		if r, ok := s.byID[id]; ok {
			out = append(out, r)
		}
	}
	truncated := bucket.truncated || (gen == 0 && s.evictedZero) || (gen != 0 && gen <= s.evictedThrough)
	return out, truncated
}

// Recoverability classifies whether a generation's external effects allow
// a clean resume. Irreversible completed work without compensation blocks
// claiming a clean rollback but still allows resume with awareness.
type Recoverability struct {
	Clean           bool     `json:"clean"`
	HasIrreversible bool     `json:"hasIrreversible"`
	Blocking        []string `json:"blocking,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

// AssessRecoverability reports whether checkpoint resume can claim a clean
// state for generation gen.
func (s *ReceiptStore) AssessRecoverability(gen uint64) Recoverability {
	out := Recoverability{Clean: true}
	receipts, truncated := s.generationSnapshot(gen)
	if truncated {
		out.Clean = false
		out.Blocking = append(out.Blocking, "receipt-history-truncated")
		out.Notes = append(out.Notes, "receipt history was evicted; clean rollback cannot be proven")
	}
	for _, r := range receipts {
		switch r.Class {
		case Irreversible:
			out.HasIrreversible = true
			out.Clean = false
			out.Notes = append(out.Notes, "irreversible effect "+r.ID+" cannot be rolled back")
		case Compensatable:
			if r.CompensationStatus == "failed" || r.CompensationStatus == "" || r.CompensationStatus == "prior_truncated" {
				out.Clean = false
				out.Blocking = append(out.Blocking, r.ID)
				out.Notes = append(out.Notes, "compensatable effect "+r.ID+" not fully compensated")
			}
		}
	}
	return out
}

// IngestScope copies completed receipts from a LiveScope/EffectScope into the store.
func (s *ReceiptStore) IngestScope(scope EffectScope) {
	if s == nil || scope == nil {
		return
	}
	for _, r := range scope.Receipts() {
		s.Record(r)
	}
}

func itoaU64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
