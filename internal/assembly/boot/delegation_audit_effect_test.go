package boot

// Effect test for delegation accounting: whether the receipts a fan-out's
// children produce reach the same sink a benchmark builds its report from. The
// counters read "no delegation" for a run whose graph showed four members.

import (
	"reasonix/internal/contract/hostaudit"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
)

type delegationAuditSink struct {
	mu     sync.Mutex
	audits []hostaudit.DelegationAudit
}

func (s *delegationAuditSink) Emit(event.Event) {}

func (s *delegationAuditSink) RecordDelegationAudit(a hostaudit.DelegationAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, a)
}

func (s *delegationAuditSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.audits)
}

// TestEffectFleetChildrenReportDelegationReceipts pins what the delegation
// section is built from: one receipt per child that ran. A fan-out whose
// members report none is published as a task one agent solved by itself.
func TestEffectFleetChildrenReportDelegationReceipts(t *testing.T) {
	sink := &delegationAuditSink{}
	runProbeWith(t, "boot-delegation-audit", &capabilityProbeProvider{calls: []string{graphEffectCall}}, sink)

	if got := sink.count(); got != 2 {
		t.Fatalf("delegation receipts = %d, want one per fleet item; the report's delegation counters are folded from these", got)
	}
}
