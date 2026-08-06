package cosplay

import (
	"context"
	"sync"
)

// Runner executes one candidate against one test. Implementations range from
// local subprocess runners (go test / node / python) to model-backed judges;
// the interface keeps the co-evolution engine host-agnostic.
type Runner interface {
	// Run returns pass, the actual output (for diagnostics), and any runner
	// error. A non-nil error is treated as a failed cell.
	Run(ctx context.Context, cand Candidate, test TestCase) (pass bool, got string, detail string, err error)
}

// ExecMatrix is the code×test execution result matrix: rows are candidates
// (original + repair rounds), columns are tests, cells are pass/fail.
type ExecMatrix struct {
	mu      sync.Mutex
	pass    map[string]map[string]bool   // candidateID -> testID -> pass
	detail  map[string]map[string]string // candidateID -> testID -> detail
	order   []string                     // candidate insertion order
}

// NewMatrix creates an empty execution matrix.
func NewMatrix() *ExecMatrix {
	return &ExecMatrix{
		pass:   make(map[string]map[string]bool),
		detail: make(map[string]map[string]string),
	}
}

// Record sets one cell to pass/fail.
func (m *ExecMatrix) Record(candID, testID string, pass bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pass[candID]; !ok {
		m.pass[candID] = make(map[string]bool)
		m.detail[candID] = make(map[string]string)
		m.order = append(m.order, candID)
	}
	m.pass[candID][testID] = pass
}

// RecordDetail attaches a diagnostic string to a cell (non-blocking).
func (m *ExecMatrix) RecordDetail(candID, testID, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.detail[candID]; ok {
		d[testID] = detail
	}
}

// Lookup returns the cell value and whether it has been executed.
func (m *ExecMatrix) Lookup(candID, testID string) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.pass[candID]; ok {
		v, ok := row[testID]
		return v, ok
	}
	return false, false
}

// PassFor reports whether a candidate passed a test (false when unexecuted).
func (m *ExecMatrix) PassFor(candID, testID string) bool {
	v, _ := m.Lookup(candID, testID)
	return v
}

// PassRate returns the fraction of executed tests a candidate passed.
// 0 for unknown candidates.
func (m *ExecMatrix) PassRate(candID string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.pass[candID]
	if !ok || len(row) == 0 {
		return 0
	}
	passed := 0
	for _, v := range row {
		if v {
			passed++
		}
	}
	return float64(passed) / float64(len(row))
}

// PassedByAny reports whether at least one candidate ever passed this test.
func (m *ExecMatrix) PassedByAny(testID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.pass {
		if row[testID] {
			return true
		}
	}
	return false
}

// ExecutedBy returns how many candidates have executed this test.
func (m *ExecMatrix) ExecutedBy(testID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, row := range m.pass {
		if _, ok := row[testID]; ok {
			n++
		}
	}
	return n
}

// CandidateIDs returns the candidate rows in insertion order.
func (m *ExecMatrix) CandidateIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.order))
	copy(out, m.order)
	return out
}
