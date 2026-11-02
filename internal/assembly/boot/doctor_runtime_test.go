package boot

import (
	"strings"
	"testing"

	"reasonix/internal/ext/extension"
)

func TestDoctorRuntimeNoRuntimeLeavesVerdictsUnknown(t *testing.T) {
	report := CollectRuntimeDoctor(nil)
	if report.Recoverability != nil || report.Resume != nil {
		t.Fatalf("no-runtime report carries verdicts: rec=%+v resume=%+v", report.Recoverability, report.Resume)
	}
	text := RenderRuntimeDoctorText(report)
	for _, want := range []string{"recoverability: unknown", "resume: unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no-runtime text missing %q:\n%s", want, text)
		}
	}
	raw, err := RenderRuntimeDoctorJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"status": null`, `"recoverability": null`, `"resume": null`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("no-runtime JSON missing %q:\n%s", want, raw)
		}
	}
}

func TestDoctorRuntimeObservedVerdictsUnchanged(t *testing.T) {
	owner := extension.NewRuntimeOwner()
	report := CollectRuntimeDoctor(&BuildResult{
		Owner:  owner,
		Status: &extension.RuntimeStatus{PublishedGeneration: 0},
	})
	if report.Recoverability == nil || !report.Recoverability.Clean {
		t.Fatalf("observed recoverability = %+v", report.Recoverability)
	}
	if report.Resume == nil || !report.Resume.AllowResume || !report.Resume.CleanRollback {
		t.Fatalf("observed resume = %+v", report.Resume)
	}
	text := RenderRuntimeDoctorText(report)
	for _, want := range []string{
		"recoverability: clean=true irreversible=false",
		"resume: allow=true cleanRollback=true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("observed text missing %q:\n%s", want, text)
		}
	}
	raw, err := RenderRuntimeDoctorJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"clean": true`, `"cleanRollback": true`, `"status": {`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("observed JSON missing %q:\n%s", want, raw)
		}
	}
}

func TestDoctorRuntimeObservedIrreversibleDeniesCleanRollback(t *testing.T) {
	owner := extension.NewRuntimeOwner()
	owner.RecordMessageSent(0, "m1", "test")
	report := CollectRuntimeDoctor(&BuildResult{Owner: owner})
	if report.Recoverability == nil || report.Recoverability.Clean || !report.Recoverability.HasIrreversible {
		t.Fatalf("irreversible recoverability = %+v", report.Recoverability)
	}
	if report.Resume == nil || !report.Resume.AllowResume || report.Resume.CleanRollback {
		t.Fatalf("irreversible resume = %+v", report.Resume)
	}
	text := RenderRuntimeDoctorText(report)
	for _, want := range []string{
		"recoverability: clean=false irreversible=true",
		"resume: allow=true cleanRollback=false",
		"resume-note: irreversible effect message-sent:m1 cannot be rolled back",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("observed text missing %q:\n%s", want, text)
		}
	}
}
