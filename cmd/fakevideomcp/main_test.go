package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rpc(t *testing.T, url, method string, params any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGenerateVideoCompletesAfterPollsAndLogsEachSubmission(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log.jsonl")
	svc, err := newService(2, logPath, func(int) []byte { return []byte("CLIP") })
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(svc)
	defer srv.Close()
	call := rpc(t, srv.URL, "tools/call", map[string]any{"name": "generate_video", "arguments": map[string]any{"production_id": "p", "shot_id": "s01", "prompt": "x", "duration": 2}})
	res := call["result"].(map[string]any)
	if res["resultType"] != "task" {
		t.Fatalf("tools/call = %v", call)
	}
	id := res["taskId"]
	if first := rpc(t, srv.URL, "tasks/get", map[string]any{"taskId": id})["result"].(map[string]any); first["status"] != "working" {
		t.Fatalf("first poll = %v", first)
	}
	done := rpc(t, srv.URL, "tasks/get", map[string]any{"taskId": id})["result"].(map[string]any)
	url := done["result"].(map[string]any)["structuredContent"].(map[string]any)["video_url"].(string)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	clip, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(clip) != "CLIP" {
		t.Fatalf("clip = %q", clip)
	}
	logged, _ := os.ReadFile(logPath)
	if strings.Count(string(logged), "\n") != 1 || !strings.Contains(string(logged), `"shot_id":"s01"`) {
		t.Fatalf("log = %s", logged)
	}
	if gone := rpc(t, srv.URL, "tasks/get", map[string]any{"taskId": "task-99"}); gone["error"] == nil {
		t.Fatal("unknown task did not answer an error")
	}
}
