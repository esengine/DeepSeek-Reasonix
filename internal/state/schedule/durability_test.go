package schedule

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/fileutil"
)

func TestNewerSchemaLoadsReadOnly(t *testing.T) {
	st, clk, dir := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	path := filepath.Join(dir, manifestName)
	data, _ := os.ReadFile(path)
	future := strings.Replace(string(data), `"schemaVersion":1`, `"schemaVersion":99`, 1)
	if err := os.WriteFile(path, []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	m, ro, err := st.Snapshot(t.Context())
	if err != nil || !ro || len(m.Schedules) != 1 {
		t.Fatalf("snapshot: ro=%v err=%v", ro, err)
	}
	clk.Advance(time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); !errors.Is(err, ErrSchemaReadonly) {
		t.Fatalf("claim on newer schema: %v", err)
	}
	if _, err := st.Create(t.Context(), p, everyReq()); !errors.Is(err, ErrSchemaReadonly) {
		t.Fatalf("create on newer schema: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != future {
		t.Fatal("a newer manifest must be left byte-for-byte alone")
	}
}

func TestCorruptManifestIsQuarantinedAndPausesAll(t *testing.T) {
	for name, content := range map[string]string{
		"garbage":       "{not json",
		"no version":    `{"schedules":[]}`,
		"truncated":     `{"schemaVersion":1,"schedules":[{"id":`,
		"oversize body": strings.Repeat(" ", MaxManifest+1),
	} {
		t.Run(name, func(t *testing.T) {
			st, _, dir := newTestStore(t)
			if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			m, ro, err := st.Snapshot(t.Context())
			if err != nil || ro || !m.PausedAll || m.PausedReason != PauseCorrupt {
				t.Fatalf("m=%+v ro=%v err=%v", m, ro, err)
			}
			q, _ := os.ReadDir(filepath.Join(dir, quarantineName))
			if len(q) != 1 {
				t.Fatalf("corrupt file must be kept aside, got %d entries", len(q))
			}
			if _, err := st.Claim(t.Context(), DefaultPolicy(), "sch_x", epoch); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrPausedAll) {
				t.Fatalf("got %v", err)
			}
			if err := st.SetPausedAll(t.Context(), false); err != nil {
				t.Fatal(err)
			}
			m, _, _ = st.Snapshot(t.Context())
			if m.PausedAll {
				t.Fatal("a person can lift the corruption pause")
			}
		})
	}
}

type crashSentinel struct{}

func TestCrashDuringWriteLeavesPreviousManifest(t *testing.T) {
	st, clk, dir := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	before, _ := os.ReadFile(filepath.Join(dir, manifestName))

	fileutil.CrashPoint = func(op, path string) {
		if strings.HasPrefix(path, dir) {
			panic(crashSentinel{})
		}
	}
	func() {
		defer func() {
			fileutil.CrashPoint = nil
			if r := recover(); r == nil {
				t.Fatal("expected the injected crash")
			}
		}()
		_, _ = st.Claim(t.Context(), p, sc.ID, slotN(sc, 1))
	}()

	after, _ := os.ReadFile(filepath.Join(dir, manifestName))
	if string(after) != string(before) {
		t.Fatal("an interrupted write must leave the previous manifest intact")
	}
	// A torn temp file next to the manifest must not confuse the next load.
	if err := os.WriteFile(filepath.Join(dir, manifestName+".tmp-torn"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	st = reopen(t, dir, clk)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); err != nil {
		t.Fatalf("slot never committed, so it must still be claimable: %v", err)
	}
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("committed claim must not repeat: %v", err)
	}
}

func TestManifestFileIsPrivateAndRevisionsAdvance(t *testing.T) {
	st, _, dir := newTestStore(t)
	mustCreate(t, st, DefaultPolicy(), everyReq())
	mustCreate(t, st, DefaultPolicy(), everyReq())
	info, err := os.Stat(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("manifest mode %v", info.Mode())
	}
	m, _, _ := st.Snapshot(t.Context())
	if m.Revision != 2 || m.SchemaVersion != SchemaVersion {
		t.Fatalf("revision=%d schema=%d", m.Revision, m.SchemaVersion)
	}
}
