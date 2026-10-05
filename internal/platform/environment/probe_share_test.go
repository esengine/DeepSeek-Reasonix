package environment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// The section this feeds is the same for every project on one machine, which is
// what prompt.go says it is for. Keying the cache on the workspace gave each one
// its own entry and re-ran every probe per project — measurably, half of one
// package's test time was eleven subprocesses per build.
func TestTwoWorkspacesShareOneProbeRun(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(1000, 0))
	dir := testenv.TempDir(t)
	writeProbeTool(t, filepath.Join(dir, "sharedtool"), "shared version")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Two workspaces, neither of which contains the tool.
	first := RunProbesWithOptions(context.Background(), []string{"sharedtool --version"}, ProbeOptions{
		DenyRoots: []string{testenv.TempDir(t)},
	})
	if len(first) != 1 || !first[0].Found {
		t.Fatalf("first workspace = %+v", first)
	}
	if first[0].Path == "" {
		t.Fatal("the result records no resolved path, so no other workspace can check it")
	}

	// A second workspace with the same PATH must not pay for the run again.
	if _, running := beginProbe(probeFingerprint([]string{"sharedtool --version"}, ProbeOptions{})); running {
		t.Fatal("the first run left no entry for the second to find")
	}
	second := RunProbesWithOptions(context.Background(), []string{"sharedtool --version"}, ProbeOptions{
		DenyRoots: []string{testenv.TempDir(t)},
	})
	if len(second) != 1 || second[0].Output != first[0].Output {
		t.Fatalf("second workspace got %+v, want the same answer as %+v", second, first)
	}
}

// Sharing must not carry a refusal past the workspace it belongs to, nor an
// answer past a workspace that would have refused it. Both directions, because
// each is a different way for the cache to speak for somebody else.
func TestASharedAnswerStopsAtAWorkspaceThatWouldRefuseIt(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(2000, 0))
	dir := testenv.TempDir(t)
	writeProbeTool(t, filepath.Join(dir, "localtool"), "should not run here")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd := []string{"localtool --version"}

	// A workspace elsewhere runs it and the answer is shareable.
	allowed := RunProbesWithOptions(context.Background(), cmd, ProbeOptions{DenyRoots: []string{testenv.TempDir(t)}})
	if len(allowed) != 1 || !allowed[0].Found {
		t.Fatalf("the allowing workspace did not run it: %+v", allowed)
	}

	// The workspace the tool lives in must still refuse, cache or no cache.
	denied := RunProbesWithOptions(context.Background(), cmd, ProbeOptions{DenyRoots: []string{dir}})
	if len(denied) != 1 {
		t.Fatalf("results len = %d, want 1", len(denied))
	}
	if denied[0].Found || denied[0].Error != "not trusted" {
		t.Fatalf("a cached run reached a workspace that denies it: %+v", denied[0])
	}

	// And the refusal is not what the next workspace inherits.
	after := RunProbesWithOptions(context.Background(), cmd, ProbeOptions{DenyRoots: []string{testenv.TempDir(t)}})
	if len(after) != 1 || !after[0].Found || after[0].Output != allowed[0].Output {
		t.Fatalf("one workspace's refusal became another's answer: %+v", after)
	}
}

// A result recorded before Path existed cannot be checked against a deny root,
// so it is not reused. Answering from it would be guessing that nothing it ran
// sits where this workspace refuses.
func TestAnAnswerWithNoResolvedPathIsNotReused(t *testing.T) {
	dir := testenv.TempDir(t)
	old := []ProbeResult{{Command: "tool --version", Binary: "tool", Found: true, Output: "v1"}}
	if reusableHere(old, []string{dir}) {
		t.Fatal("a result with no recorded path was reused against a deny root")
	}
	// With no deny roots there is nothing to check it against, so it stands.
	if !reusableHere(old, nil) {
		t.Fatal("a result was refused where nothing was denied")
	}
}
