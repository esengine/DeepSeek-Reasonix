package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
)

type fakeACL struct {
	mu       sync.Mutex
	has      map[string]bool
	failNext string
	calls    []string
}

func newFakeACL() *fakeACL { return &fakeACL{has: map[string]bool{}} }

func (f *fakeACL) Grant(root, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "grant "+filepath.Base(root))
	if f.failNext == "grant" {
		f.failNext = ""
		return errors.New("access denied")
	}
	f.has[grantKey(root)+"|"+sid] = true
	return nil
}

func (f *fakeACL) Revoke(root, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "revoke "+filepath.Base(root))
	if f.failNext == "revoke" {
		f.failNext = ""
		return errors.New("access denied")
	}
	delete(f.has, grantKey(root)+"|"+sid)
	return nil
}

func (f *fakeACL) Granted(root, sid string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.has[grantKey(root)+"|"+sid], nil
}

func openBook(t *testing.T, ops grantOps) (*GrantBook, string) {
	t.Helper()
	dir := testenv.TempDir(t)
	b, err := OpenGrantBook(dir, ops)
	if err != nil {
		t.Fatal(err)
	}
	return b, dir
}

func TestEnsureGrantsOncePerRootAndTheRecordSurvivesAReopen(t *testing.T) {
	acl := newFakeACL()
	b, dir := openBook(t, acl)
	root := testenv.TempDir(t)
	for range 3 {
		if err := b.Ensure(root); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(acl.calls); n != 1 {
		t.Fatalf("calls = %v, want one grant for three Ensure", acl.calls)
	}
	again, err := OpenGrantBook(dir, acl)
	if err != nil || again.SID() != b.SID() || len(again.Roots()) != 1 {
		t.Fatalf("reopened: sid %q vs %q, roots %v, %v", again.SID(), b.SID(), again.Roots(), err)
	}
	if !strings.HasPrefix(b.SID(), "S-1-5-21-") {
		t.Fatalf("sid = %q", b.SID())
	}
	if info, err := os.Stat(filepath.Join(dir, "windows-grants.json")); err != nil || info.Mode().Perm()&0o077 != 0 && !isWindowsLike() {
		t.Fatalf("record mode: %v %v", info, err)
	}
}

// The intent is on disk before the ACL is touched; a crash after the edit and
// before the result leaves "granting", which Repair completes.
func TestACrashBetweenIntentAndResultIsFinishedByRepair(t *testing.T) {
	acl := newFakeACL()
	b, dir := openBook(t, acl)
	root := testenv.TempDir(t)
	acl.failNext = "grant"
	if err := b.Ensure(root); err == nil {
		t.Fatal("a failed grant reported success")
	}
	if len(b.Roots()) != 0 {
		t.Fatalf("a root whose grant failed counts as granted: %v", b.Roots())
	}
	reopened, _ := OpenGrantBook(dir, acl)
	rep := reopened.Repair()
	if len(rep.Regranted) != 1 || len(rep.Failed) != 0 || len(reopened.Roots()) != 1 {
		t.Fatalf("repair = %+v roots %v", rep, reopened.Roots())
	}
	if ok, _ := acl.Granted(root, reopened.SID()); !ok {
		t.Fatal("repair did not grant")
	}
}

func TestRepairGrantsAgainWhereTheSIDWasLostAndDropsWhatNoLongerExists(t *testing.T) {
	acl := newFakeACL()
	b, _ := openBook(t, acl)
	keep, gone := testenv.TempDir(t), filepath.Join(testenv.TempDir(t), "gone")
	if err := os.Mkdir(gone, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{keep, gone} {
		if err := b.Ensure(r); err != nil {
			t.Fatal(err)
		}
	}
	delete(acl.has, grantKey(keep)+"|"+b.SID())
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	rep := b.Repair()
	if len(rep.Regranted) != 1 || len(rep.Dropped) != 1 || len(b.Roots()) != 1 {
		t.Fatalf("repair = %+v roots %v", rep, b.Roots())
	}
}

func TestRevokeRemovesTheEntryAndAnInterruptedRevokeIsFinished(t *testing.T) {
	acl := newFakeACL()
	b, dir := openBook(t, acl)
	root := testenv.TempDir(t)
	if err := b.Ensure(root); err != nil {
		t.Fatal(err)
	}
	acl.failNext = "revoke"
	if err := b.Revoke(root); err == nil {
		t.Fatal("a failed revoke reported success")
	}
	reopened, _ := OpenGrantBook(dir, acl)
	if len(reopened.Roots()) != 0 {
		t.Fatalf("a root being revoked still counts as granted: %v", reopened.Roots())
	}
	rep := reopened.Repair()
	if len(rep.Revoked) != 1 {
		t.Fatalf("repair = %+v", rep)
	}
	if ok, _ := acl.Granted(root, reopened.SID()); ok {
		t.Fatal("the SID is still on the root after repair")
	}
	if err := reopened.Revoke(root); err != nil {
		t.Fatalf("revoking an unrecorded root = %v, want a no-op", err)
	}
}

// A record that exists and cannot be read is never the same as no record: the
// sandbox must not start over with a new SID and call that safe.
func TestAnUnreadableRecordIsAnErrorNotAFreshBook(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "windows-grants.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenGrantBook(dir, newFakeACL()); !errors.Is(err, ErrGrantBookUnreadable) {
		t.Fatalf("err = %v, want ErrGrantBookUnreadable", err)
	}
	for _, sid := range []string{"S-1-5-32-544", "S-1-5-21-1-2-3-1001", "S-1-5-21-1-2-3-500", "S-1-5-21-1-2-3-20000-513", "S-1-5-21-1-2-3-20000x"} {
		body := `{"version":1,"sid":"` + sid + `","roots":[]}`
		if err := os.WriteFile(filepath.Join(dir, "windows-grants.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenGrantBook(dir, newFakeACL()); !errors.Is(err, ErrGrantBookUnreadable) {
			t.Fatalf("a record naming %s was accepted: %v", sid, err)
		}
	}
}

func TestTwoInstallsDoNotShareASID(t *testing.T) {
	a, _ := openBook(t, newFakeACL())
	b, _ := openBook(t, newFakeACL())
	if a.SID() == b.SID() {
		t.Fatalf("both installs got %s", a.SID())
	}
}

func isWindowsLike() bool { return filepath.Separator == '\\' }
