package memory

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionFollowsTheCurve(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	m := Memory{Type: TypeProject, CreatedAt: now.Add(-30 * 24 * time.Hour)}
	if got := m.Retention(now); math.Abs(got-math.Exp(-1)) > 0.001 {
		t.Fatalf("30 days at strength 30 = %v, want e^-1", got)
	}
	m.LastUsedAt = now.Add(-30 * 24 * time.Hour)
	m.Strength = 54
	if got := m.Retention(now); math.Abs(got-math.Exp(-30.0/54)) > 0.001 {
		t.Fatalf("reinforced retention = %v", got)
	}
	if got := (Memory{Type: TypeConstraint, CreatedAt: now.Add(-900 * 24 * time.Hour)}).Retention(now); got != 1 {
		t.Fatalf("a constraint faded to %v", got)
	}
	if (Memory{Type: TypeProject, CreatedAt: now.Add(-200 * 24 * time.Hour)}).RetentionState(now) != RetentionFaded {
		t.Fatal("a long unused normal fact must read as faded")
	}
}

func TestAFadedFactIsDemotedNotDropped(t *testing.T) {
	if retentionWeight(0.9) != 1 || retentionWeight(0.6) != 1 {
		t.Fatal("a clear fact must keep its full score")
	}
	if retentionWeight(0.01) != fadedWeightFloor {
		t.Fatalf("a faded fact weight = %v, want the floor", retentionWeight(0.01))
	}
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{
		ID: "mem-old", Name: "warehouse-schema", Title: "Warehouse schema", Description: "the warehouse schema lives in schema.sql",
		Type: TypeProject, Scope: FactScopeProject, CreatedAt: time.Now().Add(-400 * 24 * time.Hour),
		Body: "The warehouse schema lives in schema.sql at the repository root.",
	})
	result := AutoRecall(store, "where does the warehouse schema live", RecallOptions{})
	if len(result.Hits) != 1 || result.Hits[0].Memory.ID != "mem-old" {
		t.Fatalf("a long unused but strongly matching fact must still come back: %+v", result)
	}
}

func TestRecordUseWritesTheUsageFileAndLeavesTheFactAlone(t *testing.T) {
	store := recallTestStore(t)
	m := Memory{ID: "mem-a", Name: "a", Description: "a", Type: TypeProject, Scope: FactScopeProject, Body: "alpha fact"}
	recallTestWrite(t, store.Dir, m)
	before, err := os.ReadFile(filepath.Join(store.Dir, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if err := store.RecordUse([]Memory{m}, false, now); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUse([]Memory{m}, true, now); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(store.Dir, "a.md"))
	if string(before) != string(after) {
		t.Fatal("recording a use rewrote the fact file")
	}
	report, err := store.ListActive()
	if err != nil || len(report.Facts) != 1 {
		t.Fatalf("list: %+v %v", report, err)
	}
	got := report.Facts[0]
	if got.Uses != 2 || !got.LastUsedAt.Equal(now) || math.Abs(got.Strength-30*reinforceFull*reinforceWeak) > 0.001 || got.Revision != 1 {
		t.Fatalf("usage = uses %d last %v strength %v revision %d", got.Uses, got.LastUsedAt, got.Strength, got.Revision)
	}
}

func TestStrengthIsCappedAndConstraintsOnlyCountUses(t *testing.T) {
	store := recallTestStore(t)
	m := Memory{ID: "mem-h", Name: "h", Description: "h", Type: TypeProject, Importance: 3, Scope: FactScopeProject, Body: "h"}
	c := Memory{ID: "mem-c", Name: "c", Description: "c", Type: TypeConstraint, Scope: FactScopeProject, Body: "c"}
	recallTestWrite(t, store.Dir, m)
	recallTestWrite(t, store.Dir, c)
	for range 10 {
		if err := store.RecordUse([]Memory{m, c}, false, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	data := readUsage(store.Dir)
	if data.Facts["mem-h"].Strength != maxStrengthDays {
		t.Fatalf("strength = %v, want capped at %v", data.Facts["mem-h"].Strength, maxStrengthDays)
	}
	if data.Facts["mem-c"].Strength != 0 || data.Facts["mem-c"].Uses != 10 {
		t.Fatalf("constraint usage = %+v", data.Facts["mem-c"])
	}
	if data.Links[linkKey("mem-h", "mem-c")] != 10 {
		t.Fatalf("link weight = %v, want 10", data.Links[linkKey("mem-h", "mem-c")])
	}
}

func TestReadingAFactReinforcesItWithoutBreakingAnUpdateByRevision(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.Dir, Memory{ID: "mem-r", Name: "release-day", Description: "release day", Type: TypeProject, Scope: FactScopeProject, Body: "Releases go out on Thursday."})
	out, err := NewRecallTool(store).Execute(context.Background(), json.RawMessage(`{"operation":"read","name":"mem-r"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "state: ") || !strings.Contains(out, "revision: 1") {
		t.Fatalf("read output = %s", out)
	}
	if readUsage(store.Dir).Facts["mem-r"].Uses != 1 {
		t.Fatal("reading did not record a use")
	}
	if _, err := NewRememberTool(store).Execute(context.Background(), json.RawMessage(`{"id":"mem-r","expected_revision":1,"name":"release-day","description":"release day","body":"Releases go out on Wednesday."}`)); err != nil {
		t.Fatalf("an update by the revision just read was refused: %v", err)
	}
}

func TestOftenUsedTogetherBringsANeighbor(t *testing.T) {
	store := recallTestStore(t)
	a := Memory{ID: "mem-deploy", Name: "deploy-window", Title: "Deploy window", Description: "deploys run on thursday evenings", Type: TypeProject, Scope: FactScopeProject, Body: "Deploys run on Thursday evenings."}
	b := Memory{ID: "mem-oncall", Name: "oncall-pager", Title: "On-call pager", Description: "page the on-call before risky changes", Type: TypeProject, Scope: FactScopeProject, Body: "Page the on-call engineer first."}
	recallTestWrite(t, store.Dir, a)
	recallTestWrite(t, store.Dir, b)
	for range 2 {
		if err := store.RecordUse([]Memory{a, b}, true, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	result := AutoRecall(store, "when do deploys run on thursday evenings", RecallOptions{})
	found := map[string]bool{}
	for _, h := range result.Hits {
		found[h.Memory.ID] = true
	}
	if !found["mem-deploy"] || !found["mem-oncall"] {
		t.Fatalf("hits = %+v, want the matched fact and its frequent companion", result.Hits)
	}
}

func TestANewFactAwaitsReviewUntilMarked(t *testing.T) {
	store := recallTestStore(t)
	now := time.Now()
	m := Memory{ID: "mem-n", Name: "n", Description: "n", Type: TypeProject, Scope: FactScopeProject, Body: "new fact", CreatedAt: now.Add(-time.Hour)}
	recallTestWrite(t, store.Dir, m)
	if !m.AwaitingReview(now) {
		t.Fatal("a fact written an hour ago must await review")
	}
	if (Memory{CreatedAt: now.Add(-20 * 24 * time.Hour)}).AwaitingReview(now) {
		t.Fatal("a fact older than the review window is not new")
	}
	if err := store.MarkReviewed(m, now); err != nil {
		t.Fatal(err)
	}
	report, err := store.ListActive()
	if err != nil || len(report.Facts) != 1 {
		t.Fatalf("list = %+v, %v", report, err)
	}
	if report.Facts[0].AwaitingReview(now) {
		t.Fatal("a reviewed fact still reads as new")
	}
}

func TestClearForDaysCountsDownToTheFuzzyLine(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	full := int(math.Ceil(30 * math.Log(1/RetentionClearMin)))
	if got := (Memory{Type: TypeProject, CreatedAt: now}).ClearForDays(now); got != full {
		t.Fatalf("fresh normal fact = %d days, want %d", got, full)
	}
	if got := (Memory{Type: TypeProject, CreatedAt: now.Add(-10 * 24 * time.Hour)}).ClearForDays(now); got != full-10 {
		t.Fatalf("ten days on = %d, want %d", got, full-10)
	}
	if got := (Memory{Type: TypeProject, CreatedAt: now.Add(-60 * 24 * time.Hour)}).ClearForDays(now); got != 0 {
		t.Fatalf("a fuzzy fact = %d days, want 0", got)
	}
	if got := (Memory{Type: TypeConstraint}).ClearForDays(now); got != 0 {
		t.Fatalf("a constraint = %d days, want 0 since it never decays", got)
	}
}
