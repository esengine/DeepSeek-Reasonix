package serve

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/agent"
)

func TestSessionsReportsForeignWriterAsTakenOver(t *testing.T) {
	f := newOwnershipFixture(t)
	other := filepath.Join(f.dir, "other.jsonl")
	saveServeTestSession(t, other)

	var held atomic.Bool
	held.Store(true)
	withForeignWriterLease(t, other, &held)

	status, body := f.get(t, "/sessions")
	if status != http.StatusOK {
		t.Fatalf("sessions status = %d (body %q)", status, body)
	}
	var rows []sessionListEntry
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode sessions: %v (body %q)", err, body)
	}
	for _, row := range rows {
		if agent.CanonicalSessionPath(row.Path) == agent.CanonicalSessionPath(other) {
			if !row.TakenOver {
				t.Fatalf("foreign-held session row = %+v, want takenOver", row)
			}
			return
		}
	}
	t.Fatalf("foreign-held session %q missing from %+v", other, rows)
}
