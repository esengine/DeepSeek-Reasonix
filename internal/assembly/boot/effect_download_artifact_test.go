package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

func mp4Bytes() []byte {
	return append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 64)...)
}

// newVideoCDN serves one clip behind a signed redirect to a second host.
func newVideoCDN(t *testing.T) string {
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(mp4Bytes()) }))
	t.Cleanup(media.Close)
	mediaURL := strings.Replace(media.URL, "127.0.0.1", "localhost", 1)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mediaURL+"/clip.mp4?sig=private-cdn", http.StatusFound)
	}))
	t.Cleanup(front.Close)
	return front.URL + "/start?sig=private-front"
}

func newSyncVideoService(t *testing.T, videoURL string) string {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			result = map[string]any{"resultType": "complete", "content": []map[string]any{{"type": "text", "text": "video ready"}},
				"structuredContent": map[string]any{"video_url": videoURL}}
		default:
			result = map[string]any{"resultType": "complete"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}))
	t.Cleanup(s.Close)
	return s.URL
}

// downloadProvider generates through MCP, then downloads the retained result.
type downloadProvider struct {
	mu    sync.Mutex
	reqs  []provider.Request
	round int
}

func (p *downloadProvider) Name() string { return "boot-download-artifact" }

func (p *downloadProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	round := p.round
	p.round++
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	switch round {
	case 0:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "gen", Name: "use_capability", Arguments: capabilityCall("mcp-tool:live/job")}}
	case 1:
		ref := "missing"
		if m := regexp.MustCompile(`\[MCP result artifact: ([^\n\]]+)\]`).FindStringSubmatch(toolResults([]provider.Request{req})["gen"]); len(m) == 2 {
			ref = m[1]
		}
		args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:download_artifact", "arguments": map[string]string{
			"source": ref, "pointer": "/structuredContent/video_url", "path": "video/shot-1-v1.mp4"}})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "dl", Name: "use_capability", Arguments: string(args)}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *downloadProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func TestEffectDownloadArtifactSavesRetainedMCPResultWithPortableReceipt(t *testing.T) {
	rec := &downloadProvider{}
	provider.Register("boot-download-artifact", func(provider.Config) (provider.Provider, error) { return rec, nil })
	home, workspace := mediaTaskWorkspace(t, "boot-download-artifact", newSyncVideoService(t, newVideoCDN(t)))
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: home, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	// A downloaded file is a write the turn owes verification for; the
	// readiness gate asking for it proves the host tracked the mutation.
	var unready *agent.FinalReadinessError
	if err := ctrl.Run(t.Context(), "make a one-shot video"); !errors.As(err, &unready) {
		t.Fatalf("Run = %v, want the readiness gate to ask for verification of the download", err)
	}
	reqs := rec.requests()
	if len(reqs) < 3 {
		t.Fatalf("expected generate, download and answer rounds, got %d", len(reqs))
	}
	first, _ := json.Marshal(reqs[0].Tools)
	for i, req := range reqs[1:] {
		current, _ := json.Marshal(req.Tools)
		if !bytes.Equal(first, current) || req.Messages[0].Content != reqs[0].Messages[0].Content {
			t.Fatalf("request %d changed the cache-stable prefix", i+1)
		}
	}
	if strings.Contains(string(first), `"download_artifact"`) {
		t.Fatal("download_artifact reached the provider prefix instead of use_capability")
	}
	out := toolResults(reqs)["dl"]
	if !strings.Contains(out, `"sha256"`) || strings.Contains(out, "private-") {
		t.Fatalf("download receipt at provider: %s", out)
	}
	saved, err := os.ReadFile(filepath.Join(workspace, "video", "shot-1-v1.mp4"))
	if err != nil || !bytes.Equal(saved, mp4Bytes()) {
		t.Fatalf("downloaded media: %v", err)
	}
	receipt, err := os.ReadFile(filepath.Join(workspace, "video", "shot-1-v1.mp4.source.json"))
	if err != nil || strings.Contains(string(receipt), "private-") || !strings.Contains(string(receipt), "tool-result:") || !strings.Contains(string(receipt), "localhost") {
		t.Fatalf("portable receipt %s: %v", receipt, err)
	}
}

func TestEffectDownloadArtifactNeedsAnMCPServer(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, userModel)
	req := firstTokenProfileRequest(t, "")
	for _, schema := range req.Tools {
		if schema.Name == "download_artifact" {
			t.Fatal("download_artifact offered without any MCP server to retain a result")
		}
	}
}
