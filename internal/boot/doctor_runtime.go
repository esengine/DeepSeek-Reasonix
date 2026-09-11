package boot

import (
	"encoding/json"
	"fmt"

	"reasonix/internal/extension"
)

// RuntimeDoctorReport is the structured runtime diagnostics document for
// `reasonix doctor runtime` and desktop status panels.
type RuntimeDoctorReport struct {
	Status                *extension.RuntimeStatus           `json:"status,omitempty"`
	Metrics               extension.LifecycleMetricsSnapshot `json:"metrics"`
	Recoverability        extension.Recoverability           `json:"recoverability"`
	Resume                extension.ResumeDecision           `json:"resume"`
	PublishedGen          uint64                             `json:"publishedGeneration"`
	DrainingGens          []uint64                           `json:"drainingGenerations,omitempty"`
	RuntimeOwnerFallbacks uint64                             `json:"runtimeOwnerFallbacks"`
	Text                  string                             `json:"-"`
}

// CollectRuntimeDoctor builds a report from an optional live BuildResult and
// its session-lineage owner. Also sweeps expired drains so doctor is a natural
// product trigger for drain-timeout force-expire.
func CollectRuntimeDoctor(res *BuildResult) RuntimeDoctorReport {
	owner := extension.DefaultRuntimeOwner
	if res != nil && res.Owner != nil {
		owner = res.Owner
	}
	gate := owner.Gate
	_ = gate.SweepAndForceExpire()
	gen := gate.Published()
	report := RuntimeDoctorReport{
		Metrics:               extension.DefaultLifecycleMetrics.Snapshot(),
		PublishedGen:          gen,
		DrainingGens:          gate.DrainingGenerations(),
		RuntimeOwnerFallbacks: extension.RuntimeOwnerFallbackCount(),
		Recoverability:        owner.AssessRecoverability(gen),
		Resume:                owner.DecideResume(gen),
	}
	if res != nil {
		report.Status = res.Status
		if res.Status != nil {
			report.Text = FormatRuntimeStatus(res.Status)
		}
		if res.Snapshot != nil {
			g := res.Snapshot.Generation()
			report.Recoverability = owner.AssessRecoverability(g)
			report.Resume = owner.DecideResume(g)
		}
	}
	if report.Text == "" {
		report.Text = FormatRuntimeStatus(report.Status)
	}
	return report
}

// RenderRuntimeDoctorJSON encodes the report.
func RenderRuntimeDoctorJSON(report RuntimeDoctorReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

// RenderRuntimeDoctorText is the human-readable form.
func RenderRuntimeDoctorText(report RuntimeDoctorReport) string {
	body := report.Text
	if body == "" {
		body = "runtime status: unavailable\n"
	}
	body += fmt.Sprintf("runtime owner fallbacks: %d\n", report.RuntimeOwnerFallbacks)
	body += fmt.Sprintf("recoverability: clean=%v irreversible=%v\n", report.Recoverability.Clean, report.Recoverability.HasIrreversible)
	body += fmt.Sprintf("resume: allow=%v cleanRollback=%v\n", report.Resume.AllowResume, report.Resume.CleanRollback)
	for _, n := range report.Recoverability.Notes {
		body += "  note: " + n + "\n"
	}
	for _, n := range report.Resume.Notes {
		body += "  resume-note: " + n + "\n"
	}
	if len(report.DrainingGens) > 0 {
		body += fmt.Sprintf("draining generations: %v\n", report.DrainingGens)
	}
	return body
}
