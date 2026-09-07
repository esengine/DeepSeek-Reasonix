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
		`REASONIX_FINISH_WHEEL_TICKS`,
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
