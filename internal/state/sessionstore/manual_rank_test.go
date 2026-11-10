package sessionstore

import (
	"testing"

	"reasonix/internal/base/testenv"
)

func TestManualRankIsKeptWithTheSessionAndListed(t *testing.T) {
	dir := testenv.TempDir(t)
	a := writeIdleSession(t, dir, "a")
	b := writeIdleSession(t, dir, "b")
	if _, err := ListSessionOrder(dir); err != nil {
		t.Fatal(err)
	}
	if err := SetSessionRank(a, 2048); err != nil {
		t.Fatal(err)
	}
	rank := map[string]float64{}
	listed, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range listed {
		rank[BranchID(s.Path)] = s.ManualRank
	}
	if rank["a"] != 2048 || rank["b"] != 0 {
		t.Fatalf("ranks = %v, want a placed and b not", rank)
	}
	ordered, err := ListSessionOrder(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ordered {
		if BranchID(s.Path) == "a" && s.ManualRank != 2048 {
			t.Fatalf("the order listing lost the rank: %v", s.ManualRank)
		}
	}
	if err := SetSessionRank(b, -1); err == nil {
		t.Fatal("a negative rank means nothing and must be refused")
	}
	if err := SetSessionRank(a, 0); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := LoadBranchMeta(a); m.ManualRank != 0 {
		t.Fatalf("0 returns a session to the activity order, got %v", m.ManualRank)
	}
}
