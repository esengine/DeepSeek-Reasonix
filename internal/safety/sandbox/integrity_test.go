package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRequiredIntegrityRejectsUnsupportedAndUnknownContracts(t *testing.T) {
	for _, spec := range []Spec{
		{RequiredIntegrity: CompleteFileIntegrity},
		{Mode: "enforce", RequiredIntegrity: "future_contract"},
	} {
		if !errors.Is(RequiredIntegrityFault(spec), ErrIncompleteIntegrity) || AvailableFor(spec) || UnavailableCodeFor(spec) != "sandbox.incomplete_integrity" {
			t.Fatalf("unsupported integrity contract accepted: %+v", spec)
		}
		if argv, wrapped := CommandArgs(spec, []string{"never-start"}); argv != nil || wrapped {
			t.Fatal("refused requirement returned runnable argv")
		}
		if got := PrepareArgs(spec, []string{"never-start"}, ""); !errors.Is(got.Err, ErrIncompleteIntegrity) || got.Argv != nil {
			t.Fatalf("lost refusal: %+v", got)
		}
	}
	if err := RequiredIntegrityFault(Spec{}); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteIntegrityPreparationCannotGrantIncompleteBackend(t *testing.T) {
	spec := Spec{Mode: "enforce", RequiredIntegrity: CompleteFileIntegrity, WriteRoots: []string{t.TempDir()}}
	state := filepath.Join(t.TempDir(), "state")
	got := GrantSpec(spec, state)
	if runtime.GOOS == "windows" {
		if got.GrantSID != "" || !errors.Is(got.GrantFault, ErrIncompleteIntegrity) || AvailableFor(got) || IntegrityEnforced(got) {
			t.Fatalf("incomplete backend claims a full boundary: %+v", got)
		}
		if _, err := os.Stat(state); !os.IsNotExist(err) {
			t.Fatalf("refusal edited grants: %v", err)
		}
	} else if (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && RequiredIntegrityFault(got) != nil {
		t.Fatalf("complete platform rejected: %+v", got)
	}
}

func TestRequiredIntegrityChecksFinalWritableSurface(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix writable surfaces")
	}
	source, candidate := t.TempDir(), t.TempDir()
	spec := Spec{Mode: "enforce", RequiredIntegrity: CompleteFileIntegrity, ProtectedWriteRoots: []string{source}, WriteRoots: []string{candidate}, MinimalWrites: true}
	if err := RequiredIntegrityFault(spec); err != nil {
		t.Fatal(err)
	}
	spec.SessionTemp = source
	if !errors.Is(RequiredIntegrityFault(spec), ErrIncompleteIntegrity) {
		t.Fatal("session temp reopened the protected source")
	}
	spec.SessionTemp = ""
	spec.WriteRoots = []string{filepath.Dir(source)}
	if !errors.Is(RequiredIntegrityFault(spec), ErrIncompleteIntegrity) {
		t.Fatal("ancestor write root reopened the protected source")
	}
}
