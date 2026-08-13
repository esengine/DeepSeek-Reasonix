package sessioncatalog

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPreferredOrdinarySessionPathsHidesCoveredAndSiblingForks(t *testing.T) {
	parent := SessionRecord{Path: "/s/root.jsonl", Recovered: false, RecoveryRole: RecoveryRoleNormal, Turns: 4}
	covered := SessionRecord{
		Path: "/s/root-recovery-a.jsonl", Recovered: true, RecoveryCopy: true,
		RecoveryRole: RecoveryRoleCoveredCopy, ParentID: "root", RecoveryGroupID: "root", Turns: 2,
	}
	forkA := SessionRecord{
		Path: "/s/root-recovery-b.jsonl", Recovered: true, RecoveryRole: RecoveryRoleDiverged,
		ParentID: "root", RecoveryGroupID: "root", Turns: 5, LastActivityAt: 10,
	}
	forkB := SessionRecord{
		Path: "/s/root-recovery-c.jsonl", Recovered: true, RecoveryRole: RecoveryRoleDiverged,
		ParentID: "root", RecoveryGroupID: "root", Turns: 3, LastActivityAt: 20,
	}
	// Parent present → only the normal session is preferred.
	got := PreferredOrdinarySessionPaths([]SessionRecord{parent, covered, forkA, forkB})
	if _, ok := got[parent.Path]; !ok {
		t.Fatalf("parent missing from preferred: %+v", got)
	}
	for _, path := range []string{covered.Path, forkA.Path, forkB.Path} {
		if _, ok := got[path]; ok {
			t.Fatalf("recovery fork %s should stay hidden while parent exists", path)
		}
	}

	// Parent gone → keep the longest recovered leaf only.
	got = PreferredOrdinarySessionPaths([]SessionRecord{covered, forkA, forkB})
	if len(got) != 1 {
		t.Fatalf("preferred = %+v, want only the longest leaf", got)
	}
	if _, ok := got[forkA.Path]; !ok {
		t.Fatalf("preferred = %+v, want longest fork %s", got, forkA.Path)
	}
}

func TestPreferredOrdinarySessionPathsPrefersCanonical(t *testing.T) {
	short := SessionRecord{
		Path: "/s/a.jsonl", Recovered: true, RecoveryRole: RecoveryRoleDiverged,
		RecoveryGroupID: "g", Turns: 9, LastActivityAt: 50,
	}
	canonical := SessionRecord{
		Path: "/s/b.jsonl", Recovered: true, RecoveryRole: RecoveryRoleAdopted,
		RecoveryCanonical: true, RecoveryGroupID: "g", Turns: 2, LastActivityAt: 1,
	}
	got := PreferredOrdinarySessionPaths([]SessionRecord{short, canonical})
	if _, ok := got[canonical.Path]; !ok || len(got) != 1 {
		t.Fatalf("preferred = %+v, want canonical only", got)
	}
}

func TestOrdinaryTreeSessionForceShowsOpenFork(t *testing.T) {
	fork := SessionRecord{Path: "/s/fork.jsonl", Recovered: true, RecoveryRole: RecoveryRoleDiverged}
	preferred := map[string]struct{}{}
	if OrdinaryTreeSession(fork, false, false, preferred) {
		t.Fatal("idle non-preferred fork must stay hidden")
	}
	if !OrdinaryTreeSession(fork, true, false, preferred) {
		t.Fatal("open fork must stay reachable")
	}
}

func TestCatalogPreferredOrdinarySessionPaths(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "c.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	dir := t.TempDir()
	records := []SessionRecord{
		{Path: filepath.Join(dir, "root.jsonl"), Directory: dir, Scope: "global", TopicID: "t1",
			Turns: 2, TurnsState: TurnsValid, Health: HealthOK, LastActivityAt: 30},
		{Path: filepath.Join(dir, "fork.jsonl"), Directory: dir, Scope: "global", TopicID: "t2",
			Turns: 4, TurnsState: TurnsValid, Health: HealthOK, Recovered: true,
			ParentID: "root", RecoveryGroupID: "root", RecoveryRole: RecoveryRoleDiverged, LastActivityAt: 40},
	}
	for _, record := range records {
		if err := catalog.UpsertSession(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	got, err := catalog.PreferredOrdinarySessionPaths(ctx, "global", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[records[0].Path]; !ok {
		t.Fatalf("preferred = %+v, want parent", got)
	}
	if _, ok := got[records[1].Path]; ok {
		t.Fatalf("preferred = %+v, parent present so fork must hide", got)
	}
}
