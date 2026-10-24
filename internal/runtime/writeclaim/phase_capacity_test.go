package writeclaim

import (
	"errors"
	"testing"
)

func TestResidentBoundIsIndependentAndReleasesExactlyOnce(t *testing.T) {
	s := NewSubagentScheduler(1, 1)
	var releases []func()
	for range maxResidentExecutions {
		release, err := s.ReserveResident()
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
		defer release()
	}
	_, err := s.ReserveResident()
	var limit *ResidentLimitError
	if !errors.As(err, &limit) || limit.Limit != maxResidentExecutions {
		t.Fatalf("unbounded resident admission: %v", err)
	}
	releases[0]()
	releases[0]()
	replacement, err := s.ReserveResident()
	if err != nil {
		t.Fatal(err)
	}
	defer replacement()
	if _, err := s.ReserveResident(); !errors.As(err, &limit) {
		t.Fatalf("old release removed a replacement's slot: %v", err)
	}
	end := s.TrackExecution(true)
	if s.ActiveWriters() != 1 {
		t.Fatal("live writer not tracked")
	}
	end()
	end()
	if s.ActiveWriters() != 0 {
		t.Fatal("live writer token not released")
	}
	pool := s.PhaseCapacity()
	pool.Model[0].Limit = 10
	if s.PhaseCapacity().Model[0].Limit != 1 || pool.Model[0].ID == pool.Mutation[0].ID {
		t.Fatal("phase identity or immutable limit was lost")
	}
}
