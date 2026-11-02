package boot

import (
	"encoding/json"
	"fmt"

	"reasonix/internal/ext/extension"
)

// RuntimeDoctorReport is the structured runtime diagnostics document for
// `reasonix doctor runtime` and desktop status panels.
type RuntimeDoctorReport struct {
	Status                *extension.RuntimeStatus           `json:"status"`
	Metrics               extension.LifecycleMetricsSnapshot `json:"metrics"`
	Recoverability        *extension.Recoverability          `json:"recoverability"`
	Resume                *extension.ResumeDecision          `json:"resume"`
	PublishedGen          uint64                             `json:"publishedGeneration"`
	DrainingGens          []uint64                           `json:"drainingGenerations,omitempty"`
	RuntimeOwnerFallbacks uint64                             `json:"runtimeOwnerFallbacks"`
	Text                  string                             `json:"-"`
}

// CollectRuntimeDoctor builds a report from an optional live BuildResult and
// its session-lineage owner. Also sweeps expired drains so doctor is a natural
// product trigger for drain-timeout force-expire. A nil BuildResult means no
// runtime was available to ask: verdicts stay nil (unknown), never clean.
func CollectRuntimeDoctor(res *BuildResult) RuntimeDoctorReport {
	report := RuntimeDoctorReport{
		Metrics:               extension.DefaultLifecycleMetrics.Snapshot(),
		RuntimeOwnerFallbacks: extension.RuntimeOwnerFallbackCount(),
	}
	if res == nil {
		return report
	}
	owner := res.Owner
	if owner == nil {
		owner = extension.DefaultRuntimeOwner
	}
	gate := owner.Gate
	_ = gate.SweepAndForceExpire()
	gen := gate.Published()
	report.PublishedGen = gen
	report.DrainingGens = gate.DrainingGenerations()
	rec := owner.AssessRecoverability(gen)
	decision := owner.DecideResume(gen)
	report.Recoverability = &rec
	report.Resume = &decision
	report.Status = res.Status
	if res.Status != nil {
		report.Text = FormatRuntimeStatus(res.Status)
	}
	if res.Snapshot != nil {
		g := res.Snapshot.Generation()
		rec := owner.AssessRecoverability(g)
		decision := owner.DecideResume(g)
		report.Recoverability = &rec
		report.Resume = &decision
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
	if report.Recoverability == nil {
		body += "recoverability: unknown\n"
	} else {
		body += fmt.Sprintf("recoverability: clean=%v irreversible=%v\n", report.Recoverability.Clean, report.Recoverability.HasIrreversible)
	}
	if report.Resume == nil {
		body += "resume: unknown\n"
	} else {
		body += fmt.Sprintf("resume: allow=%v cleanRollback=%v\n", report.Resume.AllowResume, report.Resume.CleanRollback)
	}
	if report.Recoverability == nil || report.Resume == nil {
		body += "  note: no runtime was available to ask; effect receipts live only in the process that owned the runtime\n"
	}
	if report.Recoverability != nil {
		for _, n := range report.Recoverability.Notes {
			body += "  note: " + n + "\n"
		}
	}
	if report.Resume != nil {
		for _, n := range report.Resume.Notes {
			body += "  resume-note: " + n + "\n"
		}
	}
	if len(report.DrainingGens) > 0 {
		body += fmt.Sprintf("draining generations: %v\n", report.DrainingGens)
	}
	return body
}
