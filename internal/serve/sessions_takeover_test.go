package serve

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
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

func TestForeignWriterOwnershipExplainsUnavailableReclaim(t *testing.T) {
	f := newOwnershipFixture(t)
	other := filepath.Join(f.dir, "ordinary-tui.jsonl")
	saveServeTestSession(t, other)
	var held atomic.Bool
	held.Store(true)
	withForeignWriterLease(t, other, &held)
	status, body := f.get(t, "/ownership?session="+url.QueryEscape(other))
	var owner ownershipView
	if err := json.Unmarshal([]byte(body), &owner); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || owner.Holder != "other" || !owner.TakenOver || owner.Mirrored || owner.Reclaimable {
		t.Fatalf("foreign ownership = %d %+v", status, owner)
	}
	status, body = f.get(t, "/status?session="+url.QueryEscape(other))
	var view struct {
		TakenOver   bool  `json:"takenOver"`
		Reclaimable *bool `json:"reclaimable"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || !view.TakenOver || view.Reclaimable == nil || *view.Reclaimable {
		t.Fatalf("foreign status = %d %s", status, body)
	}
	status, body = f.post(t, "/reclaim", map[string]any{"sessionPath": other})
	if status != http.StatusConflict || !strings.Contains(body, "retry with force") {
		t.Fatalf("reclaim = %d %s", status, body)
	}
	if !held.Load() {
		t.Fatal("reclaim interfered with foreign writer")
	}
}
