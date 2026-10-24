package session

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
	"reasonix/internal/provider"
)

var sessionAuditProbes sync.Map

func init() {
	// SQLite evaluates CHECK expressions during integrity_check. This observes
	// real full audits through public query APIs without timing assertions.
	sqlite.MustRegisterDeterministicScalarFunction("reasonix_test_session_audit", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if counter, ok := sessionAuditProbes.Load(args[0]); ok {
				counter.(*atomic.Int64).Add(1)
			}
			return int64(1), nil
		})
}

func TestSessionReadPathsReuseIntegrityAndRecheckChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "audit-reads"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"message": provider.Message{ID: "needle", Role: provider.RoleUser, Content: strings.Repeat("needle ", 20000)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "message", []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	query, ref := service.Query(), runtime.Ref()
	windowReady(t, query, ref, HistoryWindowRequest{Limit: 1})
	searchHistoryReady(t, query, ref, "needle", "", 1)
	for _, scenario := range []struct {
		name, path string
		read       func() error
	}{
		{"window", historyIndexPath(root, ref.SessionID), func() error {
			_, err := query.ReadHistoryWindow(t.Context(), ref, HistoryWindowRequest{Limit: 1})
			return err
		}},
		{"field", historyIndexPath(root, ref.SessionID), func() error {
			page, err := query.ReadMessageField(t.Context(), ref, "needle", 0, "content", 0, 64)
			if err == nil && page.Status != "ready" {
				t.Fatalf("field status %s", page.Status)
			}
			return err
		}},
		{"attachment-authorization", historyIndexPath(root, ref.SessionID), func() error {
			_, _, err := query.lookupHistoryAttachment(t.Context(), query.persistence.(*FilesystemPersistence), ref, "absent")
			return err
		}},
		{"export", historyIndexPath(root, ref.SessionID), func() error {
			snapshot, err := query.CaptureExportSnapshot(t.Context(), ref)
			if err != nil {
				return err
			}
			return query.VisitExportMessages(t.Context(), snapshot, func(PersistentMessage) error { return nil })
		}},
		{"search", searchIndexPath(root, ref.SessionID), func() error { _, err := query.SearchHistory(t.Context(), ref, "needle", "", 1); return err }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls atomic.Int64
			token := t.Name()
			sessionAuditProbes.Store(token, &calls)
			defer sessionAuditProbes.Delete(token)
			write := func(statement string, args ...any) {
				t.Helper()
				db, err := sql.Open("sqlite", scenario.path)
				if err != nil {
					t.Fatal(err)
				}
				_, execErr := db.ExecContext(t.Context(), statement, args...)
				closeErr := db.Close()
				if execErr != nil {
					t.Fatal(execErr)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			write("CREATE TABLE IF NOT EXISTS audit_probe (token TEXT CHECK(reasonix_test_session_audit(token)=1), revision INTEGER)")
			write("DELETE FROM audit_probe")
			write("INSERT INTO audit_probe VALUES (?,0)", token)
			calls.Store(0)
			if err := scenario.read(); err != nil {
				t.Fatal(err)
			}
			if calls.Load() == 0 {
				t.Fatal("changed projection was not audited")
			}
			calls.Store(0)
			for range 3 {
				if err := scenario.read(); err != nil {
					t.Fatal(err)
				}
			}
			if count := calls.Load(); count != 0 {
				t.Fatalf("unchanged projection re-audited %d times", count)
			}
			write("UPDATE audit_probe SET revision=revision+1")
			calls.Store(0)
			if err := scenario.read(); err != nil {
				t.Fatal(err)
			}
			if calls.Load() == 0 {
				t.Fatal("external write did not revoke integrity proof")
			}
		})
	}
}
