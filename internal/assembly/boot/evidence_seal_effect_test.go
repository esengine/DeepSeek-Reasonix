package boot

import (
	"sync"

	"reasonix/internal/contract/event"
)

type bundleAuditSink struct {
	mu  sync.Mutex
	got []event.EvidenceBundleAudit
}

func (s *bundleAuditSink) Emit(event.Event) {}

func (s *bundleAuditSink) RecordEvidenceBundle(a event.EvidenceBundleAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, a)
}
