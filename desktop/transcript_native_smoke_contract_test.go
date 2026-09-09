package main

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxTranscriptNativeSmokeFinishesFromNativeGeometry(t *testing.T) {
	data, err := os.ReadFile("cmd/transcript-native-smoke/host_linux.c")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, contract := range []string{
		`window.__reasonixNativeTranscriptSmoke.reportTail()`,
		`\"type\":\"tail-status\"`,
		`REASONIX_FINISH_WHEEL_BATCH`,
		`host->tail_stable_checks >= 2`,
		`reasonix_transcript_start_finish_batch(host)`,
		`reasonix_transcript_capture_wheel_point(host, message)`,
		`host->wheel_point_ready`,
		`CLAMP(host->wheel_x`,
		`CLAMP(host->wheel_y`,
	} {
		if !strings.Contains(source, contract) {
			t.Errorf("Linux native Transcript smoke is missing geometry-driven finish contract %q", contract)
		}
	}
	if strings.Contains(source, "g_timeout_add(700, reasonix_transcript_request_result, host)") {
		t.Error("Linux native Transcript smoke still treats a fixed wheel count as proof of reaching the tail")
	}
}

// A count of delivered input events cannot prove that a measured transcript
// has been traversed. Keep the time bounds and real geometry gates on every host.
func TestNativeTranscriptTailFinishKeepsDeadlineAndGeometryGates(t *testing.T) {
	for _, tc := range []struct{ file, deadline, stable, obsolete string }{
		{"host_linux.c", "g_timeout_add_seconds(45, reasonix_transcript_timeout", "host->tail_stable_checks >= 2", "REASONIX_FINISH_WHEEL_TICKS"},
		{"host_darwin.m", "225 * NSEC_PER_SEC", "self.finishTailStableChecks >= 2", "self.finishWheelEvents >= 240"},
		{"host_windows.go", "time.Now().Before(deadline)", "state.tailStableChecks >= 2", "finishWheelTicks"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile("cmd/transcript-native-smoke/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			source := string(data)
			if !strings.Contains(source, tc.deadline) || !strings.Contains(source, tc.stable) {
				t.Fatal("native tail finish lost its bounded geometry contract")
			}
			if strings.Contains(source, tc.obsolete) {
				t.Fatal("fixed input count still terminates the tail phase before geometry is reached")
			}
		})
	}
}
