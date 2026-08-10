package agent

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// Crash-consistency model suite: a crash is injected at every durable
// boundary and recovery must never lose a durable descendant, pick sides
// silently, fabricate a chimera, or change on a second recovery.

type crashSentinel struct{ n int }

type durabilityRun struct {
	t    *testing.T
	dir  string
	path string
}

func newDurabilityRun(t *testing.T) *durabilityRun {
	t.Helper()
	dir := t.TempDir()
	return &durabilityRun{t: t, dir: dir, path: filepath.Join(dir, "session.jsonl")}
}

func (d *durabilityRun) turn(i int) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: fmt.Sprintf("ask %d", i)},
		{Role: provider.RoleAssistant, Content: fmt.Sprintf("answer %d", i)},
	}
}

// countBoundaries dry-runs fn with a counting hook and returns the ordered op
// names of every durable boundary it crossed.
func (d *durabilityRun) countBoundaries(fn func()) []string {
	var ops []string
	fileutil.CrashPoint = func(op, path string) {
		if strings.HasPrefix(path, d.dir) {
			ops = append(ops, op)
		}
	}
	defer func() { fileutil.CrashPoint = nil }()
	fn()
	return ops
}

// crashAt injects a panic at the nth durable boundary under the run's dir and
// reports whether fn actually crashed there.
func (d *durabilityRun) crashAt(n int, fn func()) (crashed bool) {
	count := 0
	fileutil.CrashPoint = func(op, path string) {
		if !strings.HasPrefix(path, d.dir) {
			return
		}
		count++
		if count == n {
			panic(crashSentinel{n})
		}
	}
	defer func() { fileutil.CrashPoint = nil }()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(crashSentinel); !ok {
				panic(r)
			}
			crashed = true
		}
	}()
	fn()
	return false
}

func (d *durabilityRun) countRecoveryFiles() int {
	entries, _ := os.ReadDir(d.dir)
	n := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), "recovery") && strings.HasSuffix(e.Name(), ".jsonl") &&
			!strings.HasSuffix(e.Name(), ".events.jsonl") {
			n++
		}
	}
	return n
}

func mustDigest(t *testing.T, msgs []provider.Message) string {
	t.Helper()
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	return digestString(digest)
}

// recoverAndCheck loads the session twice (I4) and verifies the recovered
// transcript sits between lastSaved and pending in prefix order for appends
// (I1: no rollback below the durable floor; I3: never a chimera beyond
// pending), or equals one of the two endpoints for rewrites (I2/I3).
func (d *durabilityRun) recoverAndCheck(lastSaved, pending []provider.Message, rewrite bool, label string) []provider.Message {
	d.t.Helper()
	branchesBefore := d.countRecoveryFiles()
	s1, err := LoadSession(d.path)
	if err != nil {
		// A crash before anything ever became durable legitimately leaves no
		// session file; the empty floor lost nothing.
		if len(lastSaved) == 0 && os.IsNotExist(err) {
			return nil
		}
		d.t.Fatalf("%s: recovery load failed: %v", label, err)
	}
	s2, err := LoadSession(d.path)
	if err != nil {
		d.t.Fatalf("%s: second recovery load failed: %v", label, err)
	}
	if mustDigest(d.t, s1.Messages) != mustDigest(d.t, s2.Messages) {
		d.t.Fatalf("%s: recovery not idempotent — two loads disagree", label)
	}
	if after := d.countRecoveryFiles(); after != branchesBefore {
		d.t.Fatalf("%s: pure loads changed recovery-branch count %d→%d", label, branchesBefore, after)
	}
	got := s1.Messages
	if rewrite {
		if !messagesEqualForStorageList(got, lastSaved) && !messagesEqualForStorageList(got, pending) {
			d.t.Fatalf("%s: rewrite recovery produced a state that is neither endpoint (got %d msgs, endpoints %d/%d)",
				label, len(got), len(lastSaved), len(pending))
		}
		return got
	}
	if !messagesHavePrefixWithCompatibleSystem(got, lastSaved) {
		d.t.Fatalf("%s: recovery rolled back below the durable floor (got %d msgs, floor %d) — invariant 1 violated",
			label, len(got), len(lastSaved))
	}
	if !messagesHavePrefixWithCompatibleSystem(pending, got) {
		d.t.Fatalf("%s: recovery produced a chimera beyond the pending save (got %d msgs, pending %d) — invariant 3 violated",
			label, len(got), len(pending))
	}
	return got
}

// buildSaved replays i committed turns into a fresh session file and returns
// the live session plus its durable transcript.
func (d *durabilityRun) buildSaved(turns int) (*Session, []provider.Message) {
	d.t.Helper()
	s := NewSession("system prompt")
	for i := 1; i <= turns; i++ {
		for _, m := range d.turn(i) {
			s.Add(m)
		}
		if err := s.SaveSnapshot(d.path); err != nil {
			d.t.Fatalf("seed save %d: %v", i, err)
		}
	}
	return s, append([]provider.Message(nil), s.Messages...)
}

func TestDurabilityCrashSweepAppendSave(t *testing.T) {
	probe := newDurabilityRun(t)
	s, _ := probe.buildSaved(1)
	for _, m := range probe.turn(2) {
		s.Add(m)
	}
	ops := probe.countBoundaries(func() {
		if err := s.SaveSnapshot(probe.path); err != nil {
			t.Fatalf("probe save: %v", err)
		}
	})
	if len(ops) == 0 {
		t.Fatal("save crossed no durable boundaries — seam broken")
	}
	walIdx := -1
	for i, op := range ops {
		if op == "wal-append" {
			walIdx = i
		}
	}
	t.Logf("append-save boundaries: %v (wal at %d)", ops, walIdx)

	for n := 1; n <= len(ops); n++ {
		d := newDurabilityRun(t)
		live, saved := d.buildSaved(1)
		for _, m := range d.turn(2) {
			live.Add(m)
		}
		pending := append([]provider.Message(nil), live.Messages...)
		if !d.crashAt(n, func() { _ = live.SaveSnapshot(d.path) }) {
			t.Fatalf("boundary %d: crash did not fire", n)
		}
		got := d.recoverAndCheck(saved, pending, false, fmt.Sprintf("boundary %d/%d (%s)", n, len(ops), ops[n-1]))
		// The WAL is authoritative: once the append event is durable, recovery
		// must yield the pending transcript even if the checkpoint never landed.
		if walIdx >= 0 && n > walIdx+1 && !messagesEqualForStorageList(got, pending) {
			t.Fatalf("boundary %d (%s): WAL was durable but recovery returned %d msgs instead of pending %d",
				n, ops[n-1], len(got), len(pending))
		}
	}
}

func TestDurabilityCheckpointWithoutLedgerHeals(t *testing.T) {
	probe := newDurabilityRun(t)
	s, _ := probe.buildSaved(1)
	for _, m := range probe.turn(2) {
		s.Add(m)
	}
	ops := probe.countBoundaries(func() { _ = s.SaveSnapshot(probe.path) })
	// Crash on the LAST boundary: everything before it (WAL + checkpoint) is
	// durable, the trailing ledger/index write is not.
	n := len(ops)
	d := newDurabilityRun(t)
	live, _ := d.buildSaved(1)
	for _, m := range d.turn(2) {
		live.Add(m)
	}
	pending := append([]provider.Message(nil), live.Messages...)
	if !d.crashAt(n, func() { _ = live.SaveSnapshot(d.path) }) {
		t.Fatalf("crash at final boundary did not fire (ops=%v)", ops)
	}
	branches := d.countRecoveryFiles()
	loaded, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("recovery load: %v", err)
	}
	if !messagesEqualForStorageList(loaded.Messages, pending) {
		t.Fatalf("recovery after ledger-less checkpoint returned %d msgs, want pending %d", len(loaded.Messages), len(pending))
	}
	// Healing save: continue on the recovered session without forking a branch.
	for _, m := range d.turn(3) {
		loaded.Add(m)
	}
	if err := loaded.SaveSnapshot(d.path); err != nil {
		t.Fatalf("post-recovery save must heal, got: %v", err)
	}
	if got := d.countRecoveryFiles(); got != branches {
		t.Fatalf("post-recovery save forked a recovery branch (%d→%d) instead of healing", branches, got)
	}
}

func TestDurabilityTornWALTailReplaysToLastCommit(t *testing.T) {
	d := newDurabilityRun(t)
	_, saved := d.buildSaved(2)
	wal := d.path[:len(d.path)-len(".jsonl")] + ".events.jsonl"
	if _, err := os.Stat(wal); err != nil {
		// Resolve the actual event-log path via the store layout if it differs.
		matches, _ := filepath.Glob(filepath.Join(d.dir, "*.events.jsonl"))
		if len(matches) != 1 {
			t.Fatalf("cannot locate WAL (stat %v, glob %v)", err, matches)
		}
		wal = matches[0]
	}
	f, err := os.OpenFile(wal, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	if _, err := f.WriteString(`{"schema_version":1,"type":"append","messages":[{"role":"u`); err != nil {
		t.Fatalf("tear WAL: %v", err)
	}
	f.Close()
	got := d.recoverAndCheck(saved, saved, false, "torn WAL tail")
	if !messagesEqualForStorageList(got, saved) {
		t.Fatalf("torn tail recovery returned %d msgs, want last clean commit %d", len(got), len(saved))
	}
}

func TestDurabilityStaleWriterCannotClobber(t *testing.T) {
	d := newDurabilityRun(t)
	_, _ = d.buildSaved(1)

	a, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("load A: %v", err)
	}
	b, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("load B: %v", err)
	}
	for _, m := range d.turn(2) {
		b.Add(m)
	}
	if err := b.SaveSnapshot(d.path); err != nil {
		t.Fatalf("B save: %v", err)
	}
	winner := append([]provider.Message(nil), b.Messages...)

	a.Add(provider.Message{Role: provider.RoleUser, Content: "diverged ask"})
	a.Add(provider.Message{Role: provider.RoleAssistant, Content: "diverged answer"})
	saveErr := a.SaveSnapshot(d.path)

	loaded, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if saveErr == nil {
		// A stale diverged writer may be redirected, never silently accepted
		// over B: the main path must still be B's descendant.
		if !messagesHavePrefixWithCompatibleSystem(loaded.Messages, winner) {
			t.Fatalf("stale writer clobbered the newer transcript: main path %d msgs no longer extends winner %d",
				len(loaded.Messages), len(winner))
		}
		return
	}
	if _, ok := SnapshotConflictKind(saveErr); !ok {
		t.Fatalf("stale save failed with a non-conflict error: %v", saveErr)
	}
	if !messagesEqualForStorageList(loaded.Messages, winner) {
		t.Fatalf("conflict was reported but main path changed anyway (%d msgs, want %d)", len(loaded.Messages), len(winner))
	}
}

func TestDurabilityBareSaveBootstrapsWAL(t *testing.T) {
	d := newDurabilityRun(t)
	s := NewSession("system prompt")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "bare save"})
	if err := s.Save(d.path); err != nil {
		t.Fatalf("bare Save: %v", err)
	}
	probe, err := probeSessionEventLog(d.path)
	if err != nil {
		t.Fatalf("probe WAL: %v", err)
	}
	if !probe.native || probe.size == 0 {
		t.Fatalf("bare Save did not bootstrap a native WAL: %+v", probe)
	}
	loaded, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("reload bare Save: %v", err)
	}
	if !messagesEqualForStorageList(loaded.Messages, s.Messages) {
		t.Fatalf("bare Save round trip changed transcript: got %d want %d messages", len(loaded.Messages), len(s.Messages))
	}
	if _, err := os.Stat(store.SessionEventLog(d.path)); err != nil {
		t.Fatalf("bare Save WAL missing: %v", err)
	}
}

func TestDurabilityCrossWriterIDCannotClobber(t *testing.T) {
	originalWriterID := sessionWriterID
	t.Cleanup(func() { sessionWriterID = originalWriterID })

	d := newDurabilityRun(t)
	sessionWriterID = "writer-a"
	a := NewSession("system prompt")
	a.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
	if err := a.SaveSnapshot(d.path); err != nil {
		t.Fatalf("writer A seed save: %v", err)
	}
	a, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("writer A load: %v", err)
	}

	sessionWriterID = "writer-b"
	b, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("writer B load: %v", err)
	}
	b.Add(provider.Message{Role: provider.RoleAssistant, Content: "newer writer B"})
	if err := b.SaveSnapshot(d.path); err != nil {
		t.Fatalf("writer B save: %v", err)
	}
	winner := b.Snapshot()

	sessionWriterID = "writer-a"
	a.Add(provider.Message{Role: provider.RoleAssistant, Content: "stale writer A"})
	err = a.SaveSnapshot(d.path)
	if err == nil {
		t.Fatal("cross-writer stale save unexpectedly succeeded")
	}
	if _, ok := SnapshotConflictKind(err); !ok {
		t.Fatalf("cross-writer stale save error = %v, want snapshot conflict", err)
	}
	loaded, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("reload cross-writer winner: %v", err)
	}
	if !messagesEqualForStorageList(loaded.Messages, winner) {
		t.Fatalf("cross-writer stale save clobbered winner: got %d want %d messages", len(loaded.Messages), len(winner))
	}
}

func TestDurabilityStaleCompactRewriteCannotClobber(t *testing.T) {
	d := newDurabilityRun(t)
	_, _ = d.buildSaved(1)

	stale, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("load stale session: %v", err)
	}
	newer, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("load newer session: %v", err)
	}
	newer.Add(provider.Message{Role: provider.RoleUser, Content: "newer durable turn"})
	if err := newer.SaveSnapshot(d.path); err != nil {
		t.Fatalf("newer save: %v", err)
	}
	winner := append([]provider.Message(nil), newer.Messages...)

	stale.Replace(append([]provider.Message(nil), stale.Messages...))
	err = stale.SaveRewriteCompact(d.path)
	if err == nil {
		t.Fatal("stale compact rewrite unexpectedly succeeded")
	}
	if _, ok := SnapshotConflictKind(err); !ok {
		t.Fatalf("stale compact rewrite error = %v, want snapshot conflict", err)
	}
	loaded, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("reload winner: %v", err)
	}
	if !messagesEqualForStorageList(loaded.Messages, winner) {
		t.Fatalf("stale compact rewrite clobbered winner: got %d want %d messages", len(loaded.Messages), len(winner))
	}
}

func TestDurabilityRewindSuffixDoesNotResurrect(t *testing.T) {
	d := newDurabilityRun(t)
	_, _ = d.buildSaved(3)

	a, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("load A: %v", err)
	}
	b, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("load B: %v", err)
	}
	// B performs an intentional rewind to one turn and commits it.
	short := append([]provider.Message(nil), b.Messages[:3]...) // system + turn 1
	b.Rewrite(short, "rewind")
	if err := b.SaveRewrite(d.path); err != nil {
		t.Fatalf("B rewind save: %v", err)
	}
	// A, still holding the long pre-rewind transcript, keeps appending.
	a.Add(provider.Message{Role: provider.RoleUser, Content: "stale continuation"})
	_ = a.SaveSnapshot(d.path)

	loaded, err := LoadSession(d.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if messagesHavePrefixWithCompatibleSystem(loaded.Messages, a.Messages) && len(loaded.Messages) >= len(a.Messages) {
		t.Fatalf("rewound suffix resurrected on the main path (%d msgs)", len(loaded.Messages))
	}
}

func TestDurabilityStaleInFlightCompareAndClear(t *testing.T) {
	d := newDurabilityRun(t)
	_, _ = d.buildSaved(1)
	old, err := BeginSessionInFlightTurn(d.path, 1, false)
	if err != nil {
		t.Fatalf("begin old turn: %v", err)
	}
	fresh, err := BeginSessionInFlightTurn(d.path, 3, false)
	if err != nil {
		t.Fatalf("begin fresh turn: %v", err)
	}
	cleared, err := ClearSessionInFlightTurnIfMatch(d.path, old)
	if err != nil {
		t.Fatalf("compare-and-clear: %v", err)
	}
	if cleared {
		t.Fatal("stale turn cleared the fresh turn's marker — compare-and-clear broken")
	}
	cleared, err = ClearSessionInFlightTurnIfMatch(d.path, fresh)
	if err != nil || !cleared {
		t.Fatalf("owner clear failed: cleared=%v err=%v", cleared, err)
	}
}

func TestDurabilityFuzzCrashConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("fuzz sweep skipped in -short")
	}
	for seed := int64(1); seed <= 20; seed++ {
		t.Run(fmt.Sprintf("seed%02d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			steps := 2 + rng.Intn(5)
			crashStep := 1 + rng.Intn(steps)

			type stepKind int
			const (
				kindAppend stepKind = iota
				kindRewrite
			)
			kinds := make([]stepKind, steps)
			for i := range kinds {
				if rng.Intn(10) < 8 || i == 0 {
					kinds[i] = kindAppend
				} else {
					kinds[i] = kindRewrite
				}
			}

			apply := func(s *Session, i int) {
				switch kinds[i] {
				case kindAppend:
					s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("s%d ask %d", seed, i)})
					s.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("s%d answer %d", seed, i)})
				case kindRewrite:
					keep := 1 + len(s.Messages)/2
					s.Rewrite(append([]provider.Message(nil), s.Messages[:keep]...), "compact")
				}
			}
			save := func(s *Session, i int, path string) error {
				if kinds[i] == kindRewrite {
					return s.SaveRewrite(path)
				}
				return s.SaveSnapshot(path)
			}

			// Dry run to count the crash step's boundaries.
			probe := newDurabilityRun(t)
			ps := NewSession("system prompt")
			for i := range crashStep - 1 {
				apply(ps, i)
				if err := save(ps, i, probe.path); err != nil {
					t.Fatalf("probe step %d: %v", i, err)
				}
			}
			apply(ps, crashStep-1)
			ops := probe.countBoundaries(func() { _ = save(ps, crashStep-1, probe.path) })
			if len(ops) == 0 {
				t.Skip("crash step crossed no boundaries")
			}
			boundary := 1 + rng.Intn(len(ops))

			d := newDurabilityRun(t)
			s := NewSession("system prompt")
			for i := range crashStep - 1 {
				apply(s, i)
				if err := save(s, i, d.path); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
			}
			var lastSaved []provider.Message
			if crashStep > 1 {
				lastSaved = append(lastSaved, s.Messages...)
			}
			apply(s, crashStep-1)
			pending := append([]provider.Message(nil), s.Messages...)
			if !d.crashAt(boundary, func() { _ = save(s, crashStep-1, d.path) }) {
				t.Fatalf("crash at boundary %d/%d did not fire", boundary, len(ops))
			}
			d.recoverAndCheck(lastSaved, pending, kinds[crashStep-1] == kindRewrite,
				fmt.Sprintf("seed %d step %d boundary %d/%d (%s)", seed, crashStep, boundary, len(ops), ops[boundary-1]))
		})
	}
}
