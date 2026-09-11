package main

// uiPerfSignalAllowlist is the closed set of frontend latency signals. Values
// arrive pre-bucketed from lib/uiPerf.ts; dropping anything else keeps webview
// input from widening telemetry cardinality.
var uiPerfSignalAllowlist = map[string]bool{
	"ui_bridge_events_rate": true,
	"ui_state_commit_rate":  true,
	"ui_stream_paint_p95":   true,
	"ui_frame_p95":          true,
	"ui_slow_frames":        true,
	"ui_input_latency_p95":  true,
	"ui_markdown_p95":       true,
	"ui_long_tasks":         true,
	"ui_dom_nodes":          true,
	"ui_js_heap":            true,
}

func filterUIPerfSignals(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for signal, bucket := range in {
		if uiPerfSignalAllowlist[signal] && bucket != "" {
			out[signal] = bucket
		}
	}
	return out
}

// RecordUIPerf records one turn's frontend latency summary as anonymous
// (signal, bucket) counters — same privacy posture as the event-stream
// metrics: bounded enums only, never content.
func (a *App) RecordUIPerf(signals map[string]string) {
	for signal, bucket := range filterUIPerfSignals(signals) {
		a.recordDiagnosticMetric(signal, bucket)
	}
}
