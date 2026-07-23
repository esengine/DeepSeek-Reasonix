//go:build windows
// +build windows

package edge

import "testing"

func TestWebView2OwnsMonitorScaleDetection(t *testing.T) {
	if !shouldDetectMonitorScaleChanges {
		t.Fatal("monitor-scale detection must stay enabled for mixed-DPI minimise/restore")
	}
}
