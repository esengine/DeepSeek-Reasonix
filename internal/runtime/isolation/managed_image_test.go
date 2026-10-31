package isolation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestManagedCandidateCorruptImageRemainsNamedAndRefusesSettlement(t *testing.T) {
	for _, which := range []string{"identity", "result"} {
		t.Run(which, func(t *testing.T) {
			root, managed := gitRepo(t), t.TempDir()
			s := NewStore(managed, root, opened(t, root))
			p, err := s.Prepare(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			run, err := p.BeginManaged(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := run.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			write(t, run.WorkspaceRoot(), "a.txt", "retained\n")
			if err := run.Complete(t.Context()); err != nil {
				t.Fatal(err)
			}
			r, err := s.candidateRecords(false)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			progress, err := readCandidateProgress(r, run.ID())
			if err != nil {
				t.Fatal(err)
			}
			revision := progress.ResultRevision
			if which == "identity" {
				revision = progress.IdentityRevision
			}
			name, err := candidateImageName(run.ID(), revision)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(r.Name(), name), []byte(`{"invalid":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			peer := NewStore(managed, root, opened(t, root))
			for _, apply := range []bool{false, true} {
				var err error
				if apply {
					_, err = peer.Apply(t.Context(), run.ID())
				} else {
					_, err = peer.Discard(t.Context(), run.ID())
				}
				if err == nil {
					t.Fatal("corrupt image admitted")
				}
			}
			peer.SweepExpired(t.Context(), -time.Hour)
			if !slices.Contains(peer.Pending(), run.ID()) {
				t.Fatal("corrupt result lost diagnostic ID")
			}
			if got := read(t, root, "a.txt"); got != "one\n" {
				t.Fatal(got)
			}
			if got := read(t, run.WorkspaceRoot(), "a.txt"); got != "retained\n" {
				t.Fatal(got)
			}
		})
	}
}

func TestManagedCandidateUnknownVersionPreservesProgressBytes(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.candidateRecords(false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	progress, err := readCandidateProgress(r, run.ID())
	if err != nil {
		t.Fatal(err)
	}
	progress.Kind = "candidate-run-v999"
	raw, err := json.Marshal(progress)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.Name(), candidateRecordName(run.ID()))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	peer := NewStore(managed, root, opened(t, root))
	if _, err := peer.Discard(t.Context(), run.ID()); err == nil {
		t.Fatal("future record admitted")
	}
	peer.SweepExpired(t.Context(), -time.Hour)
	if after, err := os.ReadFile(path); err != nil || string(after) != string(raw) {
		t.Fatalf("future progress changed: %s %v", after, err)
	}
	if !slices.Contains(peer.Pending(), run.ID()) {
		t.Fatal("future ID hidden")
	}
}

func TestManagedCandidateMaterializationFailureKeepsOriginalID(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	run, err := p.BeginManaged(t.Context())
	if err == nil || run == nil {
		t.Fatalf("materialization failure lost its handle: %v %v", run, err)
	}
	peer := NewStore(managed, root, s.repo)
	if !slices.Contains(peer.Pending(), run.ID()) {
		t.Fatal("pre-launch persisted ID lost")
	}
	if _, err := peer.Discard(t.Context(), run.ID()); !errors.Is(err, ErrCandidateUnsettled) {
		t.Fatal(err)
	}
}

func TestManagedCandidateOppositeDispositionCannotReportSuccess(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Discard(t.Context(), run.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(managed, root, s.repo).Apply(t.Context(), run.ID()); !errors.Is(err, ErrCandidateDisposition) {
		t.Fatalf("discarded ID reported applied: %v", err)
	}
}
