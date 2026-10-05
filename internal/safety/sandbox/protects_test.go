package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWriteProtectsNeedsEnforcement(t *testing.T) {
	outside := filepath.Join(string(filepath.Separator), "reasonix-ths-probe-does-not-exist", "trusted")
	if WriteProtects(Spec{Mode: "off"}, outside) {
		t.Fatal("an unenforced spec protects nothing")
	}
	if got := WriteProtects(Spec{Mode: "enforce", WriteRoots: []string{t.TempDir()}}, outside); got != Available() {
		t.Fatalf("WriteProtects = %v with a backend available = %v", got, Available())
	}
}

func TestOutsideAll(t *testing.T) {
	outside := filepath.Join(string(filepath.Separator), "reasonix-ths-probe-does-not-exist", "trusted")
	ws := t.TempDir()
	cases := []struct {
		name string
		path string
		dirs []string
		want bool
	}{
		{"empty path", "", []string{ws}, false},
		{"under a write root", filepath.Join(ws, "state", "trusted"), []string{ws}, false},
		{"write root inside the path", outside, []string{filepath.Join(outside, "objects")}, false},
		{"the path is a write root", outside, []string{outside}, false},
		{"elsewhere", outside, []string{ws}, true},
		{"no write roots", outside, nil, true},
	}
	for _, tc := range cases {
		if got := outsideAll(tc.path, tc.dirs); got != tc.want {
			t.Errorf("%s: outsideAll = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOutsideAllFollowsSymlinkedAncestor(t *testing.T) {
	ws := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(ws, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if outsideAll(filepath.Join(link, "not-yet", "trusted"), []string{ws}) {
		t.Fatal("a path reached through a symlink into a write root is writable")
	}
}

func TestConfinedWriteDirsCoverCallerRootsAndTemp(t *testing.T) {
	ws := t.TempDir()
	session := t.TempDir()
	dirs := confinedWriteDirs(Spec{Mode: "enforce", WriteRoots: []string{ws}, SessionTemp: session})
	for _, want := range []string{ws, session, os.TempDir()} {
		w, _ := resolveForCompare(want)
		if !slices.ContainsFunc(dirs, func(d string) bool { r, _ := resolveForCompare(d); return r == w }) {
			t.Errorf("confinedWriteDirs omits %s: %v", want, dirs)
		}
	}
	minimal := confinedWriteDirs(Spec{Mode: "enforce", WriteRoots: []string{ws}, MinimalWrites: true})
	tmp, _ := resolveForCompare(os.TempDir())
	if slices.ContainsFunc(minimal, func(d string) bool { r, _ := resolveForCompare(d); return r == tmp }) {
		t.Errorf("minimal writes still grant host temp: %v", minimal)
	}
}

func TestIntegrityEnforcedNeedsBothTheSpecAndABackend(t *testing.T) {
	if IntegrityEnforced(Spec{Mode: "off"}) || IntegrityEnforced(Spec{}) {
		t.Fatal("a spec that does not enforce claims no integrity")
	}
	if got := IntegrityEnforced(Spec{Mode: "enforce"}); got != Available() {
		t.Fatalf("IntegrityEnforced(enforce) = %v with a backend available = %v", got, Available())
	}
}
