package control

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func seedBranchProjection(t *testing.T, path string, canonical []provider.Message, covered int, version uint64) sessionstore.CompactionState {
	t.Helper()
	st := sessionstore.CompactionState{
		SchemaVersion: sessionstore.CompactionStateSchemaCurrent,
		Generation:    1,
		Projection: sessionstore.ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "sys"},
				{Role: provider.RoleUser, Content: "<compaction-summary>\nfolded branch summary\n</compaction-summary>"},
			},
			ProjectionVersion: version,
			CoveredCount:      covered,
			CoveredPrefixHash: sessionstore.CoveredPrefixHash(canonical, covered),
		},
		UpdatedAt: time.Now().UTC(),
	}
	if err := sessionstore.SaveCompactionState(path, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func branchProjectionVisibleText(msgs []provider.Message) string {
	var b strings.Builder
	for _, msg := range msgs {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestBranchAtWithoutCompactionDoesNotCreateProjection(t *testing.T) {
	c, _, _ := runTwoTurns(t)
	turns := orderedTurns(t, c)
	path, err := c.BranchAt(turns[len(turns)-1], "no compaction")
	if err != nil {
		t.Fatalf("BranchAt without compaction: %v", err)
	}
	if _, ok, err := sessionstore.LoadCompactionState(path); err != nil || ok {
		t.Fatalf("branch without a source fold has sidecar ok=%v err=%v", ok, err)
	}
}

func TestBranchAtInheritsCoveredProjectionAndUsesItOnOpen(t *testing.T) {
	c, ag, _ := runTwoTurns(t)
	parent := c.SessionPath()
	canonical := ag.Session().Snapshot()
	turns := orderedTurns(t, c)
	branchTurn := turns[0]
	branchEnd, ok := c.checkpoints.endOfTurn(branchTurn, len(canonical))
	if !ok || branchEnd < 2 {
		t.Fatalf("branch boundary %d ok=%v, want room for a folded prefix and a live tail", branchEnd, ok)
	}
	covered := branchEnd - 1
	seedBranchProjection(t, parent, canonical, covered, 3)

	path, err := c.BranchAt(branchTurn, "folded")
	if err != nil {
		t.Fatalf("BranchAt: %v", err)
	}
	st, ok, err := sessionstore.LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("branch projection ok=%v err=%v", ok, err)
	}
	if st.Projection.CoveredCount != covered || st.Projection.ProjectionVersion != 3 ||
		st.Projection.CoveredPrefixHash != sessionstore.CoveredPrefixHash(canonical[:branchEnd], covered) {
		t.Fatalf("inherited projection = %+v, want covered=%d version=3", st.Projection, covered)
	}

	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Resume(loaded, path)
	if got := ag.ProjectionVersion(); got != 3 {
		t.Fatalf("ProjectionVersion after opening branch = %d, want 3", got)
	}
	if dropped := ag.ProjectionDropped(); dropped != "" {
		t.Fatalf("inherited projection dropped: %s", dropped)
	}
	visible := branchProjectionVisibleText(ag.ModelVisibleMessages())
	if !strings.Contains(visible, "folded branch summary") {
		t.Fatalf("opened branch does not carry the fold:\n%s", visible)
	}
	if strings.Contains(visible, "first prompt") {
		t.Fatalf("folded user turn is still model-visible:\n%s", visible)
	}
	if !strings.Contains(visible, "first answer") {
		t.Fatalf("canonical tail after CoveredCount is missing:\n%s", visible)
	}
}

func TestBranchAtDoesNotInheritProjectionPastForkPoint(t *testing.T) {
	c, ag, _ := runTwoTurns(t)
	canonical := ag.Session().Snapshot()
	seedBranchProjection(t, c.SessionPath(), canonical, len(canonical), 4)
	turns := orderedTurns(t, c)

	path, err := c.BranchAt(turns[0], "before the fold")
	if err != nil {
		t.Fatalf("BranchAt before the fold: %v", err)
	}
	if _, ok, err := sessionstore.LoadCompactionState(path); err != nil || ok {
		t.Fatalf("projection covering turns after the fork was inherited: ok=%v err=%v", ok, err)
	}
}

func TestBranchLoadsInheritedProjectionInTheSamePane(t *testing.T) {
	c, ag, _ := runTwoTurns(t)
	parent := c.SessionPath()
	canonical := ag.Session().Snapshot()
	seedBranchProjection(t, parent, canonical, len(canonical), 5)
	sourceBefore, err := os.ReadFile(store.SessionContext(parent))
	if err != nil {
		t.Fatal(err)
	}

	path, err := c.Branch("same pane")
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if got := ag.ProjectionVersion(); got != 5 {
		t.Fatalf("ProjectionVersion after Branch = %d, want the inherited 5", got)
	}
	if dropped := ag.ProjectionDropped(); dropped != "" {
		t.Fatalf("inherited projection dropped in the same pane: %s", dropped)
	}
	visible := branchProjectionVisibleText(ag.ModelVisibleMessages())
	if !strings.Contains(visible, "folded branch summary") || strings.Contains(visible, "second prompt") {
		t.Fatalf("same-pane branch is not using the folded view:\n%s", visible)
	}
	sourceAfter, err := os.ReadFile(store.SessionContext(parent))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sourceBefore, sourceAfter) {
		t.Fatal("Branch changed the source session's projection sidecar")
	}
	if _, ok, err := sessionstore.LoadCompactionState(path); err != nil || !ok {
		t.Fatalf("branch projection missing after Branch: ok=%v err=%v", ok, err)
	}
}

func TestBranchAtUnreadableProjectionFallsBackWithoutFailing(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "corrupt json", body: "not json"},
		{name: "unsupported schema", body: `{"schema_version":999}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := runTwoTurns(t)
			if err := os.WriteFile(store.SessionContext(c.SessionPath()), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			turns := orderedTurns(t, c)
			path, err := c.BranchAt(turns[len(turns)-1], "fallback")
			if err != nil {
				t.Fatalf("BranchAt with unusable source projection: %v", err)
			}
			if _, ok, err := sessionstore.LoadCompactionState(path); err != nil || ok {
				t.Fatalf("fallback branch should have no projection: ok=%v err=%v", ok, err)
			}
		})
	}
}
