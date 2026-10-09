package environment

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestProbeSnapshotServesAcrossRestartsWithoutReprobing(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(1000, 0))
	dir := testenv.TempDir(t)
	snapDir := testenv.TempDir(t)
	toolPath := writeProbeTool(t, filepath.Join(dir, "snaptool"), "snaptool 1.0")
	opts := ProbeOptions{Overrides: map[string]string{"snaptool": toolPath}, SnapshotDir: snapDir}
	commands := []string{"snaptool --version"}

	first := RunProbesWithOptions(context.Background(), commands, opts)
	if len(first) != 1 || !first[0].Found || first[0].Output != "snaptool 1.0" {
		t.Fatalf("first run = %+v, want found snaptool 1.0", first)
	}
	sectionBefore := FormatSection(first, "test/os", "/bin/sh", nil)

	// Simulate a process restart: in-memory cache gone, and the tool binary
	// gone too — a live probe would now say "not found". The persisted
	// snapshot must answer instead, byte-identically.
	resetProbeCacheForTest(t, time.Unix(2000, 0))
	if err := os.Remove(toolPath); err != nil {
		t.Fatalf("remove tool: %v", err)
	}
	second := RunProbesWithOptions(context.Background(), commands, opts)
	if len(second) != 1 || !second[0].Found || second[0].Output != "snaptool 1.0" {
		t.Fatalf("post-restart run = %+v, want snapshot-served snaptool 1.0", second)
	}
	if sectionAfter := FormatSection(second, "test/os", "/bin/sh", nil); sectionAfter != sectionBefore {
		t.Fatalf("environment section changed across restart:\nbefore: %s\nafter:  %s", sectionBefore, sectionAfter)
	}
}

func TestProbeSnapshotExpiresAndAdoptsDefinitiveChanges(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(1000, 0))
	dir := testenv.TempDir(t)
	snapDir := testenv.TempDir(t)
	toolPath := writeProbeTool(t, filepath.Join(dir, "exptool"), "exptool 1.0")
	opts := ProbeOptions{Overrides: map[string]string{"exptool": toolPath}, SnapshotDir: snapDir}
	commands := []string{"exptool --version"}

	if first := RunProbesWithOptions(context.Background(), commands, opts); !first[0].Found {
		t.Fatalf("first run = %+v, want found", first)
	}

	// Past the snapshot TTL a live probe runs again; a definitive change (the
	// tool now reports a new version) is adopted and re-persisted.
	resetProbeCacheForTest(t, time.Unix(1000, 0).Add(probeSnapshotTTL+time.Hour))
	writeProbeTool(t, filepath.Join(dir, "exptool"), "exptool 2.0")
	second := RunProbesWithOptions(context.Background(), commands, opts)
	if !second[0].Found || second[0].Output != "exptool 2.0" {
		t.Fatalf("post-TTL run = %+v, want refreshed exptool 2.0", second)
	}

	// The refreshed snapshot serves the new bytes across the next restart.
	resetProbeCacheForTest(t, time.Unix(1000, 0).Add(probeSnapshotTTL+2*time.Hour))
	if err := os.Remove(toolPath); err != nil {
		t.Fatalf("remove tool: %v", err)
	}
	third := RunProbesWithOptions(context.Background(), commands, opts)
	if !third[0].Found || third[0].Output != "exptool 2.0" {
		t.Fatalf("post-refresh restart = %+v, want snapshot-served exptool 2.0", third)
	}
}

func TestProbeSnapshotKeepsFoundOverTransientFailure(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(1000, 0))
	dir := testenv.TempDir(t)
	snapDir := testenv.TempDir(t)
	toolPath := writeProbeTool(t, filepath.Join(dir, "flaptool"), "flaptool 1.0")
	opts := ProbeOptions{Overrides: map[string]string{"flaptool": toolPath}, SnapshotDir: snapDir}
	commands := []string{"flaptool --version"}

	if first := RunProbesWithOptions(context.Background(), commands, opts); !first[0].Found {
		t.Fatalf("first run = %+v, want found", first)
	}

	// Past the TTL the live probe fails transiently (nonzero exit): the merge
	// keeps the previous successful observation so the rendered section — and
	// with it the cached prompt prefix — does not flap. Overwrite the tool in
	// place so the fingerprint (command + override path) stays identical to
	// the snapshot's.
	resetProbeCacheForTest(t, time.Unix(1000, 0).Add(probeSnapshotTTL+time.Hour))
	if err := os.Remove(toolPath); err != nil {
		t.Fatalf("remove tool: %v", err)
	}
	body := "#!/bin/sh\nexit 1\n"
	if runtime.GOOS == "windows" {
		body = "@exit /b 1\r\n"
	}
	if err := os.WriteFile(toolPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write failing tool: %v", err)
	}
	second := RunProbesWithOptions(context.Background(), commands, opts)
	if !second[0].Found || second[0].Output != "flaptool 1.0" {
		t.Fatalf("transient failure run = %+v, want previous found result kept", second)
	}

	// A definitive "not found" (override pointing at a missing binary) is
	// adopted — genuinely uninstalled tools must not be resurrected forever.
	resetProbeCacheForTest(t, time.Unix(1000, 0).Add(2*probeSnapshotTTL+2*time.Hour))
	if err := os.Remove(toolPath); err != nil {
		t.Fatalf("remove tool: %v", err)
	}
	third := RunProbesWithOptions(context.Background(), commands, opts)
	if third[0].Found || third[0].Error != "not found" {
		t.Fatalf("definitive removal run = %+v, want not found adopted", third)
	}
}

func TestMergeProbeSnapshotOnlyOverlaysTransientFailures(t *testing.T) {
	previous := []ProbeResult{
		{Command: "go version", Binary: "go", Found: true, Output: "go1.24"},
		{Command: "docker --version", Binary: "docker", Found: true, Output: "Docker 27"},
		{Command: "node --version", Binary: "node", Found: true, Output: "v22"},
	}
	fresh := []ProbeResult{
		{Command: "go version", Binary: "go", Error: "timeout"},
		{Command: "docker --version", Binary: "docker", Error: "exit exit status 1"},
		{Command: "node --version", Binary: "node", Error: "not found"},
	}
	merged := mergeProbeSnapshot(previous, fresh)
	byCommand := map[string]ProbeResult{}
	for _, r := range merged {
		byCommand[r.Command] = r
	}
	if r := byCommand["go version"]; !r.Found || r.Output != "go1.24" {
		t.Fatalf("timeout overlay = %+v, want previous found kept", r)
	}
	if r := byCommand["docker --version"]; !r.Found || r.Output != "Docker 27" {
		t.Fatalf("exit overlay = %+v, want previous found kept", r)
	}
	if r := byCommand["node --version"]; r.Found || r.Error != "not found" {
		t.Fatalf("not-found overlay = %+v, want definitive result adopted", r)
	}
}

func TestProbeCacheHitPersistsSnapshotSoExpiryKeepsTheAnswer(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(1000, 0))
	dir := testenv.TempDir(t)
	toolPath := writeProbeTool(t, filepath.Join(dir, "hittool"), "hittool 1.0")
	opts := ProbeOptions{Overrides: map[string]string{"hittool": toolPath}}
	commands := []string{"hittool --version"}

	opts.SnapshotDir = testenv.TempDir(t)
	RunProbesWithOptions(context.Background(), commands, opts)

	opts.SnapshotDir = testenv.TempDir(t)
	first := RunProbesWithOptions(context.Background(), commands, opts)
	sectionBefore := FormatSection(first, "test/os", "/bin/sh", nil)

	// Past the in-memory TTL the answer must come from the snapshot this
	// directory was served from, not from a fresh run that may differ.
	resetProbeCacheForTest(t, time.Unix(1000, 0).Add(probeCacheTTL+time.Second))
	if err := os.Remove(toolPath); err != nil {
		t.Fatalf("remove tool: %v", err)
	}
	second := RunProbesWithOptions(context.Background(), commands, opts)
	if sectionAfter := FormatSection(second, "test/os", "/bin/sh", nil); sectionAfter != sectionBefore {
		t.Fatalf("environment section moved when the in-memory cache expired:\nbefore: %s\nafter:  %s", sectionBefore, sectionAfter)
	}
}

func TestProbeCacheHitLeavesAnExistingSnapshotAlone(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(1000, 0))
	dir := testenv.TempDir(t)
	toolPath := writeProbeTool(t, filepath.Join(dir, "keeptool"), "keeptool 1.0")
	opts := ProbeOptions{Overrides: map[string]string{"keeptool": toolPath}, SnapshotDir: testenv.TempDir(t)}
	commands := []string{"keeptool --version"}
	key := probeFingerprint(commands, opts)

	saveProbeSnapshot(opts.SnapshotDir, key, []ProbeResult{{Command: commands[0], Binary: "keeptool", Found: true, Output: "keeptool 0.9", Path: toolPath}}, time.Unix(900, 0))
	finishProbe(key, []ProbeResult{{Command: commands[0], Binary: "keeptool", Found: true, Output: "keeptool 1.0", Path: toolPath}}, time.Unix(1000, 0))

	RunProbesWithOptions(context.Background(), commands, opts)
	snap, ok := loadProbeSnapshot(opts.SnapshotDir, key)
	if !ok || len(snap.Results) != 1 || snap.Results[0].Output != "keeptool 0.9" || !snap.StoredAt.Equal(time.Unix(900, 0)) {
		t.Fatalf("a cache hit rewrote the snapshot it found: %+v", snap)
	}
}

func TestProbeSharedWaitPersistsSnapshotForTheWaiter(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(1000, 0))
	dir := testenv.TempDir(t)
	toolPath := writeProbeTool(t, filepath.Join(dir, "waittool"), "waittool 1.0")
	opts := ProbeOptions{Overrides: map[string]string{"waittool": toolPath}, SnapshotDir: testenv.TempDir(t)}
	commands := []string{"waittool --version"}
	key := probeFingerprint(commands, opts)

	if _, running := beginProbe(key); running {
		t.Fatal("key already in flight")
	}
	done := make(chan []ProbeResult)
	go func() { done <- RunProbesWithOptions(context.Background(), commands, opts) }()
	// The waiter cannot be observed blocking; a pause long enough for the goroutine to reach <-call.done.
	time.Sleep(100 * time.Millisecond)
	finishProbe(key, []ProbeResult{{Command: commands[0], Binary: "waittool", Found: true, Output: "waittool 1.0", Path: toolPath}}, time.Unix(1000, 0))
	got := <-done
	if len(got) != 1 || got[0].Output != "waittool 1.0" {
		t.Fatalf("waiter got %+v", got)
	}
	if _, ok := loadProbeSnapshot(opts.SnapshotDir, key); !ok {
		t.Fatal("the waiter's directory holds no snapshot of the answer it was served")
	}
}
