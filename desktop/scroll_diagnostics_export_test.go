package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func validScrollDiagnosticsPayload(t *testing.T) string {
	t.Helper()
	payload := map[string]any{
		"schemaVersion": 2,
		"manifest": map[string]any{
			"reportId":              "0123456789abcdef0123456789abcdef",
			"createdAt":             "2026-08-19T03:00:00.000Z",
			"buildCommit":           "5386a938",
			"buildChannel":          "test",
			"platform":              "windows",
			"userAgent":             "Mozilla/5.0 diagnostic-test",
			"devicePixelRatio":      1.25,
			"viewportWidth":         1440,
			"viewportHeight":        900,
			"reducedMotion":         false,
			"transcriptWidth":       1180,
			"contentWidth":          960,
			"fontSize":              14,
			"lineHeight":            23.52,
			"processFoldPreference": "auto",
			"reasoningDisplayMode":  "summary",
		},
		"summary": map[string]any{
			"durationMs":        1200,
			"eventCount":        5,
			"droppedEventCount": 0,
			"markerCount":       1,
		},
		"events": []any{
			map[string]any{"t": 0, "type": "start"},
			map[string]any{"t": 350, "type": "row-measure", "rowIndex": 44, "rowKind": "answer", "estimatedSize": 1800, "previousSize": 1800, "measuredSize": 420, "sizeDelta": -1380, "contentRevision": 3, "foldState": "closed", "disclosureCount": 1},
			map[string]any{"t": 500, "type": "mark"},
			map[string]any{"t": 700, "type": "scroll-state", "source": "jump-bottom", "previousMode": "manual", "mode": "tail-follow", "atBottom": true, "scrollable": true, "tailCommand": true},
			map[string]any{"t": 1200, "type": "stop"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBuildScrollDiagnosticsZip(t *testing.T) {
	t.Parallel()
	data, err := buildScrollDiagnosticsZip(validScrollDiagnosticsPayload(t))
	if err != nil {
		t.Fatalf("build diagnostic zip: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open diagnostic zip: %v", err)
	}
	wantNames := []string{"manifest.json", "summary.json", "scroll-events.jsonl", "sha256.txt"}
	if len(zr.File) != len(wantNames) {
		t.Fatalf("zip entries = %d, want %d", len(zr.File), len(wantNames))
	}
	for i, file := range zr.File {
		if file.Name != wantNames[i] {
			t.Fatalf("zip entry %d = %q, want %q", i, file.Name, wantNames[i])
		}
		if strings.Contains(file.Name, "..") || strings.HasPrefix(file.Name, "/") {
			t.Fatalf("unsafe zip entry %q", file.Name)
		}
	}

	eventsFile := zr.File[2]
	r, err := eventsFile.Open()
	if err != nil {
		t.Fatal(err)
	}
	events, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(events)), "\n")
	if len(lines) != 5 {
		t.Fatalf("event lines = %d, want 5", len(lines))
	}
	if !strings.Contains(lines[1], `"type":"row-measure"`) || strings.Contains(lines[1], "rowKey") {
		t.Fatalf("second event = %q, want privacy-safe row measurement", lines[1])
	}
	if !strings.Contains(lines[2], `"type":"mark"`) {
		t.Fatalf("third event = %q, want mark", lines[2])
	}

	checksumReader, err := zr.File[3].Open()
	if err != nil {
		t.Fatal(err)
	}
	checksum, err := io.ReadAll(checksumReader)
	_ = checksumReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(events)
	wantChecksum := fmt.Sprintf("%x  scroll-events.jsonl", digest)
	if strings.TrimSpace(string(checksum)) != wantChecksum {
		t.Fatalf("event checksum = %q, want %q", strings.TrimSpace(string(checksum)), wantChecksum)
	}
}

func TestBuildScrollDiagnosticsZipRejectsUnsafePayloads(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "old schema version",
			mutate: func(payload map[string]any) {
				payload["schemaVersion"] = 1
			},
		},
		{
			name: "unknown content field",
			mutate: func(payload map[string]any) {
				payload["transcriptText"] = "PRIVATE_TRANSCRIPT_CANARY"
			},
		},
		{
			name: "unknown event field",
			mutate: func(payload map[string]any) {
				events := payload["events"].([]any)
				events[0].(map[string]any)["rowKey"] = "raw-row-key"
			},
		},
		{
			name: "row measurement leaks row key",
			mutate: func(payload map[string]any) {
				events := payload["events"].([]any)
				events[1].(map[string]any)["rowKey"] = "raw-row-key"
			},
		},
		{
			name: "invalid report id",
			mutate: func(payload map[string]any) {
				payload["manifest"].(map[string]any)["reportId"] = "C:\\Users\\private"
			},
		},
		{
			name: "too many events",
			mutate: func(payload map[string]any) {
				events := make([]any, maxScrollDiagnosticEvents+1)
				for i := range events {
					events[i] = map[string]any{"t": i, "type": "scroll"}
				}
				payload["events"] = events
				payload["summary"].(map[string]any)["eventCount"] = len(events)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal([]byte(validScrollDiagnosticsPayload(t)), &payload); err != nil {
				t.Fatal(err)
			}
			test.mutate(payload)
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := buildScrollDiagnosticsZip(string(data)); err == nil {
				t.Fatal("expected unsafe payload to be rejected")
			} else if strings.Contains(err.Error(), "PRIVATE_TRANSCRIPT_CANARY") || strings.Contains(err.Error(), "private") {
				t.Fatalf("validation error exposed payload content: %q", err)
			}
		})
	}
}
