package recovery

import (
	"testing"

	"reasonix/internal/extension"
)

func TestAssessRuntimeResumeIrreversible(t *testing.T) {
	extension.RecordMessageSent(99, "t1", "test")
	d := AssessRuntimeResume(99)
	if d.CleanRollback {
		t.Fatal("expected unclean rollback")
	}
	if !d.AllowResume {
		t.Fatal("expected allow resume")
	}
	notes := ResumeNotesForGeneration(99)
	if len(notes) == 0 {
		t.Fatal("expected notes")
	}
}
