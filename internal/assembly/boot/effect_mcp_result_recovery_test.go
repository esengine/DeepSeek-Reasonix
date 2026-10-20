package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

type mediaTaskService struct {
	*httptest.Server
	complete    atomic.Bool
	submissions atomic.Int32
	cancels     atomic.Int32
	polled      chan struct{}
	once        sync.Once
}

func newMediaTaskService(t *testing.T, complete bool) *mediaTaskService {
	s := &mediaTaskService{polled: make(chan struct{})}
	s.complete.Store(complete)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{"2026-07-28"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"ttlMs": 0, "tools": []map[string]any{{"name": "job", "description": "Generate a video.", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			s.submissions.Add(1)
			result = map[string]any{"resultType": "task", "taskId": "remote-original", "status": "working", "pollIntervalMs": 1}
		case "tasks/get":
			s.once.Do(func() { close(s.polled) })
			result = map[string]any{"resultType": "complete", "taskId": "remote-original", "status": "working", "pollIntervalMs": 1}
			if s.complete.Load() {
				result = map[string]any{"resultType": "complete", "taskId": "remote-original", "status": "completed", "result": map[string]any{
					"content":           []map[string]any{{"type": "text", "text": "video ready"}, {"type": "resource_link", "uri": "https://example.test/v.mp4", "mimeType": "video/mp4", "future": "keep"}},
					"structuredContent": map[string]any{"media_id": "media-1", "seed": int64(9007199254740993)}, "future": map[string]any{"keep": true}}}
			}
		case "tasks/cancel":
			s.cancels.Add(1)
			result = map[string]any{"resultType": "complete"}
		default:
			result = map[string]any{"resultType": "complete"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}))
	t.Cleanup(s.Close)
	return s
}

func mediaTaskWorkspace(t *testing.T, kind, url string) (string, string) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	block := fmt.Sprintf("\n[[plugins]]\nname = \"live\"\ntype = \"http\"\nurl = %q\nload = \"always\"\n", url)
	writeFile(t, workspace, "reasonix.toml", listChangedConfig(kind, block))
	approveWorkspace(t, workspace)
	approveProjectServer(t, workspace, "live")
	return reasonixHome, workspace
}

type mediaResultReadProvider struct{ listChangedProvider }

func (p *mediaResultReadProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	if p.round != 1 {
		p.mu.Unlock()
		return p.listChangedProvider.Stream(ctx, req)
	}
	p.reqs = append(p.reqs, req)
	p.round++
	p.mu.Unlock()
	output := toolResults([]provider.Request{req})["probe-0"]
	match := regexp.MustCompile(`\[MCP result artifact: ([^\n]+)\]`).FindStringSubmatch(output)
	ref := "missing"
	if len(match) == 2 {
		ref = match[1]
	}
	args, _ := json.Marshal(map[string]string{"path": ref})
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "read-result", Name: "read_file", Arguments: string(args)}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectMCPStructuredResultPreservesPrefixAndDurableEnvelope(t *testing.T) {
	s := newMediaTaskService(t, true)
	rec := &mediaResultReadProvider{listChangedProvider: listChangedProvider{calls: map[int]string{0: capabilityCall("mcp-tool:live/job")}}}
	provider.Register("boot-media-result", func(provider.Config) (provider.Provider, error) { return rec, nil })
	home, workspace := mediaTaskWorkspace(t, "boot-media-result", s.URL)
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: home, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	if err := ctrl.Run(t.Context(), "make a video"); err != nil {
		t.Fatal(err)
	}
	reqs := rec.requests()
	if len(reqs) < 2 {
		t.Fatal("no post-tool request")
	}
	first, _ := json.Marshal(reqs[0].Tools)
	for i, req := range reqs[1:] {
		current, _ := json.Marshal(req.Tools)
		if !bytes.Equal(first, current) {
			t.Fatalf("request %d changed tool schema prefix", i+1)
		}
		if req.Messages[0].Content != reqs[0].Messages[0].Content {
			t.Fatal("result changed stable system prefix")
		}
	}
	output := toolResults(reqs)["probe-0"]
	if !strings.Contains(output, "[structuredContent fields: media_id, seed]") || strings.Contains(output, "media-1") {
		t.Fatalf("the provider should name the structured fields and leave their values in the retained result: %s", output)
	}
	readBack := toolResults(reqs)["read-result"]
	if !strings.Contains(readBack, `"media_id":"media-1"`) || !strings.Contains(readBack, `"future":"keep"`) || !strings.Contains(readBack, "[external content") {
		t.Fatalf("model could not inspect retained external result: %s", readBack)
	}
	match := regexp.MustCompile(`\[MCP result artifact: ([^\n]+)\]`).FindStringSubmatch(output)
	if len(match) != 2 {
		t.Fatalf("no durable result reference: %s", output)
	}
	f, err := ctrl.JobManager().OpenToolResultForSession(sessionstore.BranchID(ctrl.SessionPath()), match[1])
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"future":"keep"`, `"mimeType":"video/mp4"`, `9007199254740993`, `"keep":true`} {
		if !bytes.Contains(b, []byte(want)) {
			t.Fatalf("canonical envelope lost %s: %s", want, b)
		}
	}
}

func TestEffectMCPRemoteTaskSurvivesControllerCloseAndResume(t *testing.T) {
	s := newMediaTaskService(t, false)
	var rec *listChangedProvider
	provider.Register("boot-media-resume", func(provider.Config) (provider.Provider, error) { return rec, nil })
	home, workspace := mediaTaskWorkspace(t, "boot-media-resume", s.URL)
	rec = &listChangedProvider{calls: map[int]string{0: capabilityCall("mcp-tool:live/job")}}
	first, err := Build(t.Context(), Options{Sink: event.Discard, Home: home, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	path := filepath.Join(home, "sessions", "video.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	firstLease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer firstLease.Release()
	first.SetFreshSessionPath(path)
	runDone := make(chan error, 1)
	go func() { runDone <- first.Run(t.Context(), "make a video") }()
	select {
	case <-s.polled:
	case <-time.After(10 * time.Second):
		t.Fatal("remote task never started polling")
	}
	running := first.JobManager().Running()
	if len(running) != 1 {
		t.Fatalf("accepted task not owned: %+v", running)
	}
	first.Close()
	select {
	case <-runDone:
	case <-time.After(10 * time.Second):
		t.Fatal("foreground did not stop")
	}
	if s.cancels.Load() != 0 {
		t.Fatal("controller close cancelled the remote task")
	}
	firstLease.Release()
	s.complete.Store(true)
	rec = &listChangedProvider{calls: map[int]string{}}
	second, err := Build(t.Context(), Options{Sink: event.Discard, Home: home, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	sess, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Resume(sess, path); err != nil {
		t.Fatal(err)
	}
	results, _ := second.JobManager().Wait(t.Context(), []string{running[0].ID}, jobs.WaitOptions{Timeout: 5 * time.Second})
	if len(results) != 1 || results[0].Status != jobs.Done || !strings.Contains(results[0].Output, "[structuredContent fields: media_id, seed]") {
		t.Fatalf("recovered job = %+v", results)
	}
	if s.submissions.Load() != 1 {
		t.Fatalf("video submission repeated %d times", s.submissions.Load())
	}
}
