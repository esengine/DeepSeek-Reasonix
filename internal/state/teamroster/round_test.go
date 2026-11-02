package teamroster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestARoundStateRoundTrips(t *testing.T) {
	s, g := Store{Dir: t.TempDir()}, newGroupID()
	zero, err := s.Round(g)
	if err != nil || zero.Round != 0 || zero.Speaking != nil || zero.Seen == nil {
		t.Fatalf("a group that never ran a round = %+v, %v", zero, err)
	}
	ada, ben, cy := newAgentID(), newAgentID(), newAgentID()
	at := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	want := RoundState{
		Round: 3, Step: 7, Relays: 2,
		Queue:    []Slot{{Member: ben, Cause: CauseHand, By: ada, Ceiling: "readOnly"}},
		Speaking: &Speaking{Slot: Slot{Member: ada, Cause: CauseUser, By: ByUser}, Key: "group-x-7", StartedAt: at, LastEventAt: at},
		Working:  []string{cy},
		Parked:   []Slot{{Member: cy, Cause: CauseMention, By: ben}},
		Seen:     map[string]int64{ada: 12, ben: 9},
	}
	if err := s.SaveRound(context.Background(), g, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Round(g)
	if err != nil {
		t.Fatal(err)
	}
	if got.Round != 3 || got.Step != 7 || got.Relays != 2 || got.Queue[0] != want.Queue[0] || got.Speaking.Key != "group-x-7" ||
		got.Speaking.Member != ada || !got.Speaking.StartedAt.Equal(at) || got.Working[0] != cy || got.Parked[0] != want.Parked[0] || got.Seen[ada] != 12 {
		t.Fatalf("Round = %+v", got)
	}
}

func TestSaveRoundRefusesMalformedState(t *testing.T) {
	s, g := Store{Dir: t.TempDir()}, newGroupID()
	member := newAgentID()
	for name, r := range map[string]RoundState{
		"member":  {Queue: []Slot{{Member: "Ada", Cause: CauseUser}}},
		"cause":   {Queue: []Slot{{Member: member, Cause: "whim"}}},
		"by":      {Parked: []Slot{{Member: member, Cause: CauseHand, By: "Ada"}}},
		"working": {Working: []string{"../x"}},
		"seen":    {Seen: map[string]int64{"Ada": 1}},
		"relays":  {Relays: -1},
		"ceiling": {Queue: []Slot{{Member: member, Cause: CauseHand, Ceiling: "yolo!"}}},
	} {
		if err := s.SaveRound(context.Background(), g, r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: SaveRound = %v", name, err)
		}
	}
	if err := s.SaveRound(context.Background(), "g-../../x", RoundState{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad group id = %v", err)
	}
}

func TestACorruptRoundIsReported(t *testing.T) {
	s, g := Store{Dir: t.TempDir()}, newGroupID()
	dir := filepath.Join(s.Dir, groupsDir, g)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"round":`, `{"queue":[{"member":"Ada","cause":"user"}]}`} {
		if err := os.WriteFile(filepath.Join(dir, RoundFile), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Round(g); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("Round(%s) = %v, want ErrCorrupt", body, err)
		}
	}
}
