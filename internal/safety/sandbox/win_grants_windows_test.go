//go:build windows

package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// The real DACL edit, on a real directory: the SID appears on the root and on a
// file made after, and is gone after a revoke.
func TestWindowsGrantOpsEditARealDACL(t *testing.T) {
	ops := WindowsGrantOps()
	root := testenv.TempDir(t)
	sid := newSyntheticSID()
	if ok, err := ops.Granted(root, sid); err != nil || ok {
		t.Fatalf("before: granted=%v err=%v", ok, err)
	}
	if err := ops.Grant(root, sid); err != nil {
		t.Fatal(err)
	}
	if ok, err := ops.Granted(root, sid); err != nil || !ok {
		t.Fatalf("after grant: granted=%v err=%v", ok, err)
	}
	child := filepath.Join(root, "made-after.txt")
	if err := os.WriteFile(child, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := ops.Granted(child, sid); err != nil || !ok {
		t.Fatalf("a file created after the grant did not inherit it: granted=%v err=%v", ok, err)
	}
	if err := ops.Revoke(root, sid); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ops.Granted(root, sid); ok {
		t.Fatal("the SID is still on the root after a revoke")
	}
	if ok, _ := ops.Granted(child, sid); ok {
		t.Fatal("the SID is still on a child after a revoke")
	}
}

func TestAGrantBookOverRealDACLsRepairsAfterAnExternalRemoval(t *testing.T) {
	b, err := OpenGrantBook(testenv.TempDir(t), WindowsGrantOps())
	if err != nil {
		t.Fatal(err)
	}
	root := testenv.TempDir(t)
	if err := b.Ensure(root); err != nil {
		t.Fatal(err)
	}
	if err := WindowsGrantOps().Revoke(root, b.SID()); err != nil {
		t.Fatal(err)
	}
	if rep := b.Repair(); len(rep.Regranted) != 1 || len(rep.Failed) != 0 {
		t.Fatalf("repair = %+v", rep)
	}
	if ok, _ := WindowsGrantOps().Granted(root, b.SID()); !ok {
		t.Fatal("repair did not restore the grant")
	}
	_ = b.Revoke(root)
}
