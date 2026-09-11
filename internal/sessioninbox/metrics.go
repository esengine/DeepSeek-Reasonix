package sessioninbox

import "sync/atomic"

// Lightweight process-local counters (no message bodies). Telemetry sinks may
// scrape these; zero values mean the feature was unused.
var (
	metricEnqueue       atomic.Int64
	metricEnqueueBytes  atomic.Int64
	metricRecovered     atomic.Int64
	metricPaused        atomic.Int64
	metricSteerAccepted atomic.Int64
	metricSteerRejected atomic.Int64
	metricCapacityRej   atomic.Int64
	metricUncertain     atomic.Int64
	metricTxFail        atomic.Int64
)

// MetricsSnapshot is a body-free counters view for diagnostics.
type MetricsSnapshot struct {
	Enqueue        int64 `json:"enqueue"`
	EnqueueBytes   int64 `json:"enqueueBytes"`
	Recovered      int64 `json:"recovered"`
	Paused         int64 `json:"paused"`
	SteerAccepted  int64 `json:"steerAccepted"`
	SteerRejected  int64 `json:"steerRejected"`
	CapacityReject int64 `json:"capacityReject"`
	Uncertain      int64 `json:"uncertain"`
	TxFail         int64 `json:"txFail"`
}

// Metrics returns current process-local inbox counters.
func Metrics() MetricsSnapshot {
	return MetricsSnapshot{
		Enqueue:        metricEnqueue.Load(),
		EnqueueBytes:   metricEnqueueBytes.Load(),
		Recovered:      metricRecovered.Load(),
		Paused:         metricPaused.Load(),
		SteerAccepted:  metricSteerAccepted.Load(),
		SteerRejected:  metricSteerRejected.Load(),
		CapacityReject: metricCapacityRej.Load(),
		Uncertain:      metricUncertain.Load(),
		TxFail:         metricTxFail.Load(),
	}
}

// NoteEnqueue increments durable-enqueue counters (body length only, no text).
func NoteEnqueue(bytes int64) {
	metricEnqueue.Add(1)
	if bytes > 0 {
		metricEnqueueBytes.Add(bytes)
	}
}

// NoteRecovered records crash-recovery item counts.
func NoteRecovered(n int) {
	if n > 0 {
		metricRecovered.Add(int64(n))
	}
}

func NotePaused()         { metricPaused.Add(1) }
func NoteSteerAccepted()  { metricSteerAccepted.Add(1) }
func NoteSteerRejected()  { metricSteerRejected.Add(1) }
func NoteCapacityReject() { metricCapacityRej.Add(1) }
func NoteUncertain()      { metricUncertain.Add(1) }
func NoteTxFail()         { metricTxFail.Add(1) }
