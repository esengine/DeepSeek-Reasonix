package environment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestSnapshotOwnerKeepsItsAnswerAfterAnotherHomeRefreshes(t *testing.T) {
	now := time.Unix(1000, 0)
	resetProbeCacheForTest(t, now)
	tool := filepath.Join(testenv.TempDir(t), "ownertool")
	toolPath := writeProbeTool(t, tool, "version one")
	commands := []string{"ownertool --version"}
	firstHome := ProbeOptions{Overrides: map[string]string{"ownertool": toolPath}, SnapshotDir: testenv.TempDir(t)}
	otherHome := firstHome
	otherHome.SnapshotDir = testenv.TempDir(t)
	first := RunProbesWithOptions(context.Background(), commands, firstHome)
	if len(first) != 1 || !first[0].Found || first[0].Output != "version one" {
		t.Fatalf("initial probe = %+v", first)
	}
	setProbeNowForTest(now.Add(probeCacheTTL + time.Second))
	writeProbeTool(t, tool, "version two")
	other := RunProbesWithOptions(context.Background(), commands, otherHome)
	if len(other) != 1 || other[0].Output != "version two" {
		t.Fatalf("new home must refresh the expired shared cache: %+v", other)
	}
	rebuilt := RunProbesWithOptions(context.Background(), commands, firstHome)
	if FormatSection(rebuilt, "test/os", "/bin/sh", nil) != FormatSection(first, "test/os", "/bin/sh", nil) {
		t.Fatalf("another home's cache replaced a fresh persisted answer: first=%+v rebuilt=%+v", first, rebuilt)
	}
}

func TestSharedRefreshKeepsTheExpiredOwnersSuccessfulObservation(t *testing.T) {
	now := time.Unix(1000, 0)
	resetProbeCacheForTest(t, now)
	tool := filepath.Join(testenv.TempDir(t), "refreshowner")
	toolPath := writeProbeTool(t, tool, "version one")
	commands := []string{"refreshowner --version"}
	owner := ProbeOptions{Overrides: map[string]string{"refreshowner": toolPath}, SnapshotDir: testenv.TempDir(t)}
	first := RunProbesWithOptions(context.Background(), commands, owner)
	if len(first) != 1 || !first[0].Found {
		t.Fatalf("first observation = %+v", first)
	}
	setProbeNowForTest(now.Add(probeSnapshotTTL + time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	other := owner
	other.SnapshotDir = testenv.TempDir(t)
	failed := RunProbesWithOptions(ctx, commands, other)
	if len(failed) != 1 || !transientProbeFailure(failed[0]) {
		t.Fatalf("canceled probe must be transient: %+v", failed)
	}
	got := RunProbesWithOptions(context.Background(), commands, owner)
	if len(got) != 1 || !got[0].Found || got[0].Output != first[0].Output {
		t.Fatalf("shared transient result erased the owner's successful observation: %+v", got)
	}
	snapshot, ok := loadProbeSnapshot(owner.SnapshotDir, probeFingerprint(commands, owner))
	if !ok || snapshot.Results[0].Output != first[0].Output || !snapshot.StoredAt.Equal(probeNow()) {
		t.Fatalf("refreshed snapshot = %+v", snapshot)
	}
	resetProbeCacheForTest(t, probeNow().Add(time.Second))
	if err := os.Remove(toolPath); err != nil {
		t.Fatal(err)
	}
	restarted := RunProbesWithOptions(context.Background(), commands, owner)
	if len(restarted) != 1 || restarted[0].Output != first[0].Output {
		t.Fatalf("restart lost the merged answer: %+v", restarted)
	}
}
