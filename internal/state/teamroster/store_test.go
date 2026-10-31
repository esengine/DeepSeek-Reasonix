package teamroster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func addChief(r *Roster) error {
	r.Agents = append(r.Agents, agent("  Chief  ", true))
	return nil
}

func TestLoadOfAMissingRosterIsEmpty(t *testing.T) {
	r, err := Store{Dir: filepath.Join(t.TempDir(), "team")}.Load()
	if err != nil || r.Revision != 0 || len(r.Agents) != 0 || r.Groups == nil {
		t.Fatalf("Load = %+v, %v", r, err)
	}
}

func TestUpdateRoundTripsNormalizesAndBumpsRevision(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "team")}
	got, err := s.Update(context.Background(), addChief)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || got.Agents[0].Name != "Chief" {
		t.Fatalf("Update = %+v", got)
	}
	back, err := s.Load()
	if err != nil || back.Revision != 1 || back.Agents[0].ID != got.Agents[0].ID || back.Agents[0].Name != "Chief" {
		t.Fatalf("Load = %+v, %v", back, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(s.Dir, FileName))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("roster mode = %v, %v", info.Mode(), err)
		}
	}
}

func TestUpdateWritesNothingWhenTheResultIsRefused(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Update(context.Background(), addChief); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if _, err := s.Update(context.Background(), func(*Roster) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("mutate error = %v", err)
	}
	_, err := s.Update(context.Background(), func(r *Roster) error {
		r.Agents[0].Main = false
		return nil
	})
	if !errors.Is(err, ErrLastMain) {
		t.Fatalf("invalid result = %v", err)
	}
	if r, _ := s.Load(); r.Revision != 1 || !r.Agents[0].Main {
		t.Fatalf("refused updates reached the disk: %+v", r)
	}
}

func TestUpdateAtRefusesAStaleRevision(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	first, err := s.Update(context.Background(), addChief)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateAt(context.Background(), first.Revision, func(r *Roster) error {
		r.Agents[0].Pinned = true
		return nil
	}); err != nil {
		t.Fatalf("current revision refused: %v", err)
	}
	if _, err := s.UpdateAt(context.Background(), first.Revision, func(*Roster) error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision = %v, want ErrConflict", err)
	}
}

func TestACorruptRosterIsReportedAndNeverRewritten(t *testing.T) {
	for name, body := range map[string]string{
		"not json":         `{"agents": [`,
		"no schema":        `{"agents": [], "groups": []}`,
		"fails validation": `{"schemaVersion": 1, "agents": [{"id": "a-0000000000000000", "name": "x"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := Store{Dir: t.TempDir()}
			path := filepath.Join(s.Dir, FileName)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Load(); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Load = %v, want ErrCorrupt", err)
			}
			if _, err := s.Update(context.Background(), addChief); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Update = %v, want ErrCorrupt", err)
			}
			if data, _ := os.ReadFile(path); string(data) != body {
				t.Fatalf("corrupt roster was rewritten: %s", data)
			}
		})
	}
}

func TestANewerRosterReadsButRefusesWrites(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	body := `{"schemaVersion": 2, "revision": 7, "agents": [{"id": "a-0000000000000000", "name": "Chief", "main": true, "future": 1}], "groups": []}`
	if err := os.WriteFile(filepath.Join(s.Dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := s.Load()
	if !errors.Is(err, ErrNewerSchema) || r.Revision != 7 || len(r.Agents) != 1 || r.Agents[0].Name != "Chief" {
		t.Fatalf("Load = %+v, %v", r, err)
	}
	if _, err := s.Update(context.Background(), addChief); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("Update = %v, want ErrNewerSchema", err)
	}
}

func TestConcurrentUpdatesSerialize(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for range writers {
		wg.Go(func() {
			_, err := s.Update(context.Background(), func(r *Roster) error {
				r.Agents = append(r.Agents, agent(newAgentID(), true))
				return nil
			})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if r, err := s.Load(); err != nil || r.Revision != writers || len(r.Agents) != writers {
		t.Fatalf("after %d writers: revision %d, %d agents, %v", writers, r.Revision, len(r.Agents), err)
	}
}

func TestOnlyOneProcessHoldsTheWriterLease(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "team")}
	release, err := s.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireLease(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("second lease = %v, want ErrReadOnly", err)
	}
	release()
	again, err := s.AcquireLease()
	if err != nil {
		t.Fatalf("lease after release: %v", err)
	}
	again()
}

func TestTheZeroStoreIsRefused(t *testing.T) {
	if _, err := (Store{}).Load(); err == nil {
		t.Fatal("Load with no directory succeeded")
	}
	if _, err := (Store{}).Update(context.Background(), addChief); err == nil {
		t.Fatal("Update with no directory succeeded")
	}
	if _, err := (Store{}).AcquireLease(); err == nil {
		t.Fatal("AcquireLease with no directory succeeded")
	}
}
