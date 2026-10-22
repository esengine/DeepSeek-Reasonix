// Command fakevideomcp is a deterministic stand-in for a video-generation MCP
// service, for the offline video benchmark suite. It speaks MCP Tasks over HTTP:
// generate_video returns a task that completes after a fixed number of polls
// with a structured video_url served from this process. Every submission is
// appended to a JSON-lines log so a task's verifier can count duplicates.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7591", "listen address")
	polls := flag.Int("polls", 2, "tasks/get calls before a task completes")
	logPath := flag.String("log", "fakevideomcp.jsonl", "submission log (JSON lines)")
	flag.Parse()
	svc, err := newService(*polls, *logPath, renderClip)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fakevideomcp listening on http://%s/mcp", *addr)
	log.Fatal(http.ListenAndServe(*addr, svc))
}

type task struct {
	production, shot string
	duration         int
	polled           int
}

type service struct {
	mu     sync.Mutex
	polls  int
	log    *os.File
	tasks  map[string]*task
	seq    int
	render func(seconds int) []byte
	clips  map[int][]byte
}

func newService(polls int, logPath string, render func(int) []byte) (*service, error) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &service{polls: polls, log: f, tasks: map[string]*task{}, render: render, clips: map[int][]byte{}}, nil
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/clip.mp4" {
		seconds, _ := strconv.Atoi(r.URL.Query().Get("s"))
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(s.clip(max(1, min(seconds, 30))))
		return
	}
	var req struct {
		ID     *int            `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	result, rpcErr := s.answer(r, req.Method, req.Params)
	resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

var generateSchema = map[string]any{"type": "object", "required": []string{"production_id", "shot_id", "prompt"},
	"properties": map[string]any{"production_id": map[string]any{"type": "string"}, "shot_id": map[string]any{"type": "string"},
		"prompt": map[string]any{"type": "string"}, "duration": map[string]any{"type": "integer", "minimum": 1, "maximum": 30}}}

func (s *service) answer(r *http.Request, method string, params json.RawMessage) (any, map[string]any) {
	switch method {
	case "server/discover":
		return map[string]any{"supportedVersions": []string{"2026-07-28"}, "capabilities": map[string]any{"tools": map[string]any{}}}, nil
	case "tools/list":
		return map[string]any{"ttlMs": 0, "tools": []map[string]any{{"name": "generate_video",
			"description": "Generate one video shot from a prompt. Long-running: returns a task.", "inputSchema": generateSchema}}}, nil
	case "tools/call":
		var p struct {
			Arguments struct {
				Production string `json:"production_id"`
				Shot       string `json:"shot_id"`
				Prompt     string `json:"prompt"`
				Duration   int    `json:"duration"`
			} `json:"arguments"`
		}
		_ = json.Unmarshal(params, &p)
		a := p.Arguments
		s.mu.Lock()
		s.seq++
		id := fmt.Sprintf("task-%d", s.seq)
		s.tasks[id] = &task{production: a.Production, shot: a.Shot, duration: max(1, min(a.Duration, 30))}
		line, _ := json.Marshal(map[string]any{"task_id": id, "production_id": a.Production, "shot_id": a.Shot, "prompt": a.Prompt})
		_, _ = s.log.Write(append(line, '\n'))
		s.mu.Unlock()
		return map[string]any{"resultType": "task", "taskId": id, "status": "working", "pollIntervalMs": 200}, nil
	case "tasks/get", "tasks/cancel":
		var p struct {
			TaskID string `json:"taskId"`
		}
		_ = json.Unmarshal(params, &p)
		s.mu.Lock()
		defer s.mu.Unlock()
		t, ok := s.tasks[p.TaskID]
		if !ok {
			return nil, map[string]any{"code": -32602, "message": "unknown task"}
		}
		if method == "tasks/cancel" {
			delete(s.tasks, p.TaskID)
			return map[string]any{"resultType": "complete"}, nil
		}
		t.polled++
		if t.polled < s.polls {
			return map[string]any{"resultType": "complete", "taskId": p.TaskID, "status": "working", "pollIntervalMs": 200}, nil
		}
		url := fmt.Sprintf("http://%s/clip.mp4?s=%d&shot=%s&sig=fake-signature", r.Host, t.duration, t.shot)
		return map[string]any{"resultType": "complete", "taskId": p.TaskID, "status": "completed", "result": map[string]any{
			"content":           []map[string]any{{"type": "text", "text": "shot " + t.shot + " is ready"}},
			"structuredContent": map[string]any{"video_url": url, "duration": t.duration, "width": 320, "height": 240, "shot_id": t.shot}}}, nil
	}
	return map[string]any{"resultType": "complete"}, nil
}

func (s *service) clip(seconds int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clips[seconds]; ok {
		return c
	}
	c := s.render(seconds)
	s.clips[seconds] = c
	return c
}

// renderClip makes a real, decodable clip with the host's ffmpeg; without one
// it serves a container header, which downloads but will not pass check_video.
func renderClip(seconds int) []byte {
	dir, err := os.MkdirTemp("", "fakevideomcp-")
	if err == nil {
		defer os.RemoveAll(dir)
		out := dir + "/clip.mp4"
		cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i",
			fmt.Sprintf("testsrc=size=320x240:rate=10:duration=%d", seconds), "-pix_fmt", "yuv420p", "-movflags", "+faststart", out)
		if cmd.Run() == nil {
			if b, err := os.ReadFile(out); err == nil {
				return b
			}
		}
	}
	return append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{0}, 64)...)
}
