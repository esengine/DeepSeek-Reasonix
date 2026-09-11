package recovery

import "reasonix/internal/extension"

// AssessRuntimeResume returns whether checkpoint/session resume can claim a
// clean rollback for the published (or given) runtime generation.
//
// Policy is owned by extension.DecideResume: irreversible work never yields
// CleanRollback=true; resume itself remains allowed with awareness.
func AssessRuntimeResume(gen uint64) extension.ResumeDecision {
	if gen == 0 {
		gen = extension.DefaultPublishGate().Published()
	}
	return extension.DecideResumeDefault(gen)
}

// ResumeNotesForGeneration formats doctor-facing notes for a generation.
func ResumeNotesForGeneration(gen uint64) []string {
	d := AssessRuntimeResume(gen)
	out := make([]string, 0, len(d.Notes)+2)
	if d.HasIrreversible {
		out = append(out, "irreversible external effects present; clean rollback is not claimed")
	}
	if !d.CleanRollback {
		out = append(out, "resume allowed with awareness; compensation incomplete or not applicable")
	}
	out = append(out, d.Notes...)
	return out
}
