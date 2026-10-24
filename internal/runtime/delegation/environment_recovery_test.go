package delegation

import (
	"testing"
	"time"

	"reasonix/internal/state/sessionstore"
)

func TestInterruptedTranscriptRequiresNewEnvironmentGeneration(t *testing.T) {
	spec := testSubagentSpec(t, "task")
	spec.EnvironmentID, spec.EnvironmentGeneration, spec.OriginWorkspace = "iso_candidate", "previous", "source"
	for _, status := range []sessionstore.SubagentStatus{sessionstore.SubagentRunning, sessionstore.SubagentInterrupted, sessionstore.SubagentFailed} {
		meta := metaFromSpec("sa_record", status, time.Now(), time.Now(), spec)
		if err := validateMeta(meta, spec); err == nil {
			t.Fatalf("same-generation %s accepted", status)
		}
		next := spec
		next.EnvironmentGeneration = "current"
		if err := validateMeta(meta, next); err != nil {
			t.Fatalf("new-generation %s rejected: %v", status, err)
		}
		next.EnvironmentID = "iso_other"
		if err := validateMeta(meta, next); err == nil {
			t.Fatalf("different environment %s accepted", status)
		}
	}
}
