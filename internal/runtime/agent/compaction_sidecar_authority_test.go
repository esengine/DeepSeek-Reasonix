package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func sidecarMessages() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("work line\n", 800)},
		{Role: provider.RoleUser, Content: "continue"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("more work\n", 800)},
		{Role: provider.RoleUser, Content: "tail"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}
}

func sidecarAgent(t *testing.T, prov provider.Provider, sess *sessionstore.Session, path string, load bool) *Agent {
	t.Helper()
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 20_000, CompactRatio: 0.5, WorkspaceID: "ws", ModelRef: "p/m",
	}, event.Discard)
	a.BindSessionPath(path, load)
	return a
}

// The transcript save fails closed when this controller holds no live write
// authority for the path. The context sidecar write had no such check, so a
// controller that may not write the session still installed a fold there — and,
// carrying a projection loaded before the owner's fold, it summarised the same
// span a second time and replaced the digest the owner published.
func TestCompactionSidecarIsOutsideTheSessionWriteAuthority(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")

	// The stale writer binds before anything is folded, so its in-memory
	// projection is empty while the owner's later fold lands on disk.
	staleSess := &sessionstore.Session{Messages: sidecarMessages()}
	staleSess.RequireWriteAuthority() // production write path: fail closed
	staleProv := &fakeProvider{reply: "digest written without authority"}
	stale := sidecarAgent(t, staleProv, staleSess, path, true)

	ownerProv := &fakeProvider{reply: "digest written by the owner"}
	owner := sidecarAgent(t, ownerProv, &sessionstore.Session{Messages: sidecarMessages()}, path, true)
	if _, err := owner.CompactNow(context.Background(), CompactRequest{}); err != nil {
		t.Fatalf("owner CompactNow: %v", err)
	}
	owned, ok, err := sessionstore.LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("load owned sidecar: ok=%v err=%v", ok, err)
	}
	if owned.Projection.SummaryHash == "" || owned.Projection.CoveredCount == 0 {
		t.Fatalf("owner fold not published: %+v", owned.Projection)
	}
	ownerVersion := owner.window().currentProjectionVersion()

	// This controller may not write the session at all.
	authErr := staleSess.Save(path)
	if authErr == nil {
		t.Fatal("setup: the stale session was allowed to write the transcript")
	}
	t.Logf("transcript save for the same session: %v", authErr)

	_, compactErr := stale.CompactNow(context.Background(), CompactRequest{})
	if compactErr == nil {
		t.Fatalf("a controller with no write authority for this session installed and published a fold; the transcript save for the same session was refused with %v", authErr)
	}
	t.Logf("stale writer refused the fold: %v", compactErr)

	after, ok, err := sessionstore.LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("reload sidecar: ok=%v err=%v", ok, err)
	}
	if after.Projection.SummaryHash != owned.Projection.SummaryHash ||
		after.Projection.ProjectionVersion != owned.Projection.ProjectionVersion ||
		after.Generation != owned.Generation {
		t.Fatalf("a controller with no write authority for this session replaced its installed fold: summary %q -> %q, projection version %d -> %d, generation %d -> %d, two paid summaries for one span (coverage %d/%d)",
			owned.Projection.SummaryHash, after.Projection.SummaryHash, ownerVersion, after.Projection.ProjectionVersion,
			owned.Generation, after.Generation, owned.Projection.CoveredCount, after.Projection.CoveredCount)
	}
}
