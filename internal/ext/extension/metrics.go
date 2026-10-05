package extension

import (
	"sync/atomic"
	"time"
)

// LifecycleMetrics is a process-wide counter set for activation, drain,
// publish, and stale drops. Values are anonymous tallies only — no content.
type LifecycleMetrics struct {
	Publishes         atomic.Uint64
	Drains            atomic.Uint64
	StaleDrops        atomic.Uint64
	AdmissionRejected atomic.Uint64
	NoOpRebuilds      atomic.Uint64
	FullRebuilds      atomic.Uint64
	SubgraphRebuilds  atomic.Uint64
	SidecarAdopts     atomic.Uint64
	SidecarStarts     atomic.Uint64
	ActivateNanos     atomic.Uint64 // cumulative activation duration
	ActivateCount     atomic.Uint64
	DrainNanos        atomic.Uint64
	DrainCount        atomic.Uint64
	GraphBuildNanos   atomic.Uint64
	GraphBuildCount   atomic.Uint64
	PlanDiffNanos     atomic.Uint64
	PlanDiffCount     atomic.Uint64
}

// DefaultLifecycleMetrics is the process-wide metrics instance.
var DefaultLifecycleMetrics = &LifecycleMetrics{}

// Snapshot returns a plain copy for doctor/diagnostics.
type LifecycleMetricsSnapshot struct {
	Publishes         uint64 `json:"publishes"`
	Drains            uint64 `json:"drains"`
	StaleDrops        uint64 `json:"staleDrops"`
	AdmissionRejected uint64 `json:"admissionRejected"`
	NoOpRebuilds      uint64 `json:"noOpRebuilds"`
	FullRebuilds      uint64 `json:"fullRebuilds"`
	SubgraphRebuilds  uint64 `json:"subgraphRebuilds"`
	SidecarAdopts     uint64 `json:"sidecarAdopts"`
	SidecarStarts     uint64 `json:"sidecarStarts"`
	ActivateAvgNanos  uint64 `json:"activateAvgNanos"`
	DrainAvgNanos     uint64 `json:"drainAvgNanos"`
	GraphBuildAvgNs   uint64 `json:"graphBuildAvgNanos"`
	PlanDiffAvgNanos  uint64 `json:"planDiffAvgNanos"`
}

// Snapshot returns current counters with averaged timings.
func (m *LifecycleMetrics) Snapshot() LifecycleMetricsSnapshot {
	if m == nil {
		return LifecycleMetricsSnapshot{}
	}
	avg := func(total, count uint64) uint64 {
		if count == 0 {
			return 0
		}
		return total / count
	}
	return LifecycleMetricsSnapshot{
		Publishes:         m.Publishes.Load(),
		Drains:            m.Drains.Load(),
		StaleDrops:        m.StaleDrops.Load(),
		AdmissionRejected: m.AdmissionRejected.Load(),
		NoOpRebuilds:      m.NoOpRebuilds.Load(),
		FullRebuilds:      m.FullRebuilds.Load(),
		SubgraphRebuilds:  m.SubgraphRebuilds.Load(),
		SidecarAdopts:     m.SidecarAdopts.Load(),
		SidecarStarts:     m.SidecarStarts.Load(),
		ActivateAvgNanos:  avg(m.ActivateNanos.Load(), m.ActivateCount.Load()),
		DrainAvgNanos:     avg(m.DrainNanos.Load(), m.DrainCount.Load()),
		GraphBuildAvgNs:   avg(m.GraphBuildNanos.Load(), m.GraphBuildCount.Load()),
		PlanDiffAvgNanos:  avg(m.PlanDiffNanos.Load(), m.PlanDiffCount.Load()),
	}
}

// Observe records an elapsed duration into the named cumulative timer.
func (m *LifecycleMetrics) ObserveActivate(d time.Duration) {
	if m == nil {
		return
	}
	m.ActivateNanos.Add(uint64(d.Nanoseconds()))
	m.ActivateCount.Add(1)
}

// ObserveDrain records drain duration.
func (m *LifecycleMetrics) ObserveDrain(d time.Duration) {
	if m == nil {
		return
	}
	m.DrainNanos.Add(uint64(d.Nanoseconds()))
	m.DrainCount.Add(1)
}

// ObserveGraphBuild records dependency graph build duration.
func (m *LifecycleMetrics) ObserveGraphBuild(d time.Duration) {
	if m == nil {
		return
	}
	m.GraphBuildNanos.Add(uint64(d.Nanoseconds()))
	m.GraphBuildCount.Add(1)
}

// ObservePlanDiff records plan diff duration.
func (m *LifecycleMetrics) ObservePlanDiff(d time.Duration) {
	if m == nil {
		return
	}
	m.PlanDiffNanos.Add(uint64(d.Nanoseconds()))
	m.PlanDiffCount.Add(1)
}
