package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writePendingUpdateRaw drops arbitrary bytes where the transaction lives.
func writePendingUpdateRaw(t *testing.T, body string) string {
	t.Helper()
	path := PendingUpdatePath()
	if path == "" {
		t.Fatal("pending update path unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func quarantinedCopies(t *testing.T) []string {
	t.Helper()
	path := PendingUpdatePath()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	prefix := filepath.Base(path) + ".unusable-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			out = append(out, entry.Name())
		}
	}
	return out
}

// A marker that cannot describe a recoverable transaction used to block every
// future update forever: preparation refused over it and reconciliation could
// not act on it, so neither retrying nor reinstalling helped (#7342).
func TestPrepareUpdateRecoversFromUnusablePendingTransaction(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"truncated write", `{"schema_version":1,"to_version":"v2"`},
		{"not json at all", "\x00\x00garbage"},
		{"wrong types", `{"schema_version":"one"}`},
		{"empty file", ""},
		{"no target release", `{"schema_version":1,"to_version":""}`},
		{"missing identity", `{"schema_version":1,"to_version":"v2"}`},
		{"unparseable creation time", `{"schema_version":1,"to_version":"v2","platform":"test","created_at":"yesterday"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("REASONIX_HOME", home)
			target := filepath.Join(t.TempDir(), "reasonix-desktop")
			originalExecutable := repairExecutable
			repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
			t.Cleanup(func() { repairExecutable = originalExecutable })
			if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
				t.Fatal(err)
			}
			writePendingUpdateRaw(t, tc.body)

			if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
				t.Fatalf("PrepareFileUpdate over an unusable transaction: %v", err)
			}
			if got := quarantinedCopies(t); len(got) != 1 {
				t.Fatalf("quarantined copies = %v, want the unusable marker preserved exactly once", got)
			}
			tx, err := ReadPendingUpdate()
			if err != nil {
				t.Fatalf("ReadPendingUpdate after recovery: %v", err)
			}
			if tx.ToVersion != "v2" {
				t.Fatalf("pending transaction = %+v, want the newly prepared one", tx)
			}
		})
	}
}

// The guard still has to hold for a transaction that can actually be recovered:
// preparing over one would overwrite the fixed backup paths it still owns.
func TestPrepareUpdateStillRefusesOverRecoverableTransaction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(filepath.Dir(target), "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}

	if _, err := PrepareFileUpdate("v2", "v3", target); err == nil {
		t.Fatal("PrepareFileUpdate over a recoverable transaction = nil error, want refusal")
	} else if !strings.Contains(err.Error(), "a pending update already exists") {
		t.Fatalf("error = %v, want the pending-update refusal", err)
	}
	if got := quarantinedCopies(t); len(got) != 0 {
		t.Fatalf("quarantined copies = %v, want a recoverable transaction left alone", got)
	}
}

// Reconciliation must clear debris too, otherwise startup keeps reporting a
// recovery failure that nothing can resolve.
func TestReconcilePendingUpdateQuarantinesDebris(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	writePendingUpdateRaw(t, `{"schema_version":1,"to_version":"v2"`)

	result, err := ReconcilePendingUpdate("v1")
	if err != nil {
		t.Fatalf("ReconcilePendingUpdate over debris: %v", err)
	}
	if !result.Cleared {
		t.Fatalf("result = %+v, want the debris reported as cleared", result)
	}
	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("pending marker still present after reconciliation: %v", err)
	}
	if got := quarantinedCopies(t); len(got) != 1 {
		t.Fatalf("quarantined copies = %v, want the debris preserved for diagnosis", got)
	}
}

// Quarantine preserves evidence rather than deleting it, and repeated recovery
// never overwrites an earlier copy.
func TestQuarantinePendingUpdatePreservesEveryCopy(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	for i := range 3 {
		writePendingUpdateRaw(t, `{"broken":`)
		aside, err := quarantinePendingUpdate("test")
		if err != nil {
			t.Fatalf("quarantine %d: %v", i, err)
		}
		body, err := os.ReadFile(aside)
		if err != nil {
			t.Fatalf("read quarantined copy %d: %v", i, err)
		}
		if string(body) != `{"broken":` {
			t.Fatalf("quarantined body = %q, want the original bytes", body)
		}
	}
	if got := quarantinedCopies(t); len(got) != 3 {
		t.Fatalf("quarantined copies = %v, want all three preserved", got)
	}
}

// A transaction that describes itself but does not validate for this
// installation is not debris: it may own real rollback material and simply be
// observed from the wrong install, so it must survive classification.
func TestSelfDescribingTransactionIsNotTreatedAsDebris(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	body, err := json.Marshal(UpdateTransaction{
		SchemaVersion: updateTransactionVersion,
		ToVersion:     "v2",
		FromVersion:   "v1",
		Platform:      "test",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		TargetKind:    "file",
		TargetPath:    filepath.Join("elsewhere", "reasonix-desktop"),
	})
	if err != nil {
		t.Fatal(err)
	}
	writePendingUpdateRaw(t, string(body))

	disposition, tx, classifyErr := classifyPendingUpdate()
	if classifyErr != nil {
		t.Fatalf("classifyPendingUpdate: %v", classifyErr)
	}
	if disposition != pendingUpdateActionable {
		t.Fatalf("disposition = %v, want a self-describing transaction treated as actionable", disposition)
	}
	if tx != nil {
		t.Fatal("a transaction that fails installation validation must not be handed back as usable")
	}
	if got := quarantinedCopies(t); len(got) != 0 {
		t.Fatalf("quarantined copies = %v, want none", got)
	}
}
