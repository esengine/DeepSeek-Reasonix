//go:build !windows

package worktree

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestSnapshotMetadataWithGroupWritableUmask(t *testing.T) {
	if os.Getenv("REASONIX_METADATA_UMASK_HELPER") == "1" {
		syscall.Umask(0002)
		TestSnapshotMetadataPreservesCapturedModesAndSurvivesGC(t)
		return
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSnapshotMetadataWithGroupWritableUmask$", "-test.v")
	cmd.Env = append(os.Environ(), "REASONIX_METADATA_UMASK_HELPER=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("group-writable umask snapshot lost exact modes: %v\n%s", err, output)
	}
}
