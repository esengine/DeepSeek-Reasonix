package boot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/plugin"
)

// screenshotProvider asks for a screenshot every round until it has seen
// enough of them, then answers.
type screenshotProvider struct {
	mu    sync.Mutex
	shots int
	reqs  []provider.Request
}

func (p *screenshotProvider) Name() string { return "boot-screenshot" }

func (p *screenshotProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	round := len(p.reqs)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if round <= p.shots {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("shot-%d", round), Name: "mcp__screen__shot", Arguments: `{}`,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *screenshotProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// screenshotMCPServer answers every call with one distinct screenshot, so a
// request's images say which rounds they came from.
func screenshotMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "screen", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "shot",
				"description": "Capture the screen.",
				"inputSchema": map[string]any{"type": "object"},
				"annotations": map[string]any{"readOnlyHint": true},
			}}}
		case "tools/call":
			mu.Lock()
			calls++
			width := 10 + calls
			mu.Unlock()
			var buf bytes.Buffer
			if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, 10))); err != nil {
				t.Error(err)
			}
			result = map[string]any{"content": []map[string]any{
				{"type": "text", "text": "captured"},
				{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(buf.Bytes())},
			}}
		default:
			http.Error(w, "unsupported method", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
}

func imageWidth(t *testing.T, dataURL string) int {
	t.Helper()
	_, payload, _ := strings.Cut(dataURL, ";base64,")
	cfg, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)))
	if err != nil {
		t.Fatalf("decode request image: %v", err)
	}
	return cfg.Width
}

// TestEffectScreenshotsAgeOutOfTheRequest holds the image budget where it is
// paid: however many screenshots a session takes, a request carries the newest
// few, and the model is told what left.
func TestEffectScreenshotsAgeOutOfTheRequest(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)

	const shots = 12
	rec := &screenshotProvider{shots: shots}
	provider.Register("boot-screenshot", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-screenshot"
model = "x"
vision = true
`)
	approveWorkspaceAt(t, home, dir)
	server := screenshotMCPServer(t)
	defer server.Close()

	ctrl, err := Build(context.Background(), Options{
		Home:          home,
		WorkspaceRoot: dir,
		Sink:          event.Discard,
		ExtraPlugins:  []plugin.Spec{{Name: "screen", Type: "http", URL: server.URL, Authorized: true}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "watch the screen"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := agentRequests(rec.requests())
	if len(reqs) != shots+1 {
		t.Fatalf("requests = %d, want %d", len(reqs), shots+1)
	}
	last := reqs[len(reqs)-1]
	var widths []int
	told := false
	for _, m := range last.Messages {
		if m.Role != provider.RoleTool {
			continue
		}
		for _, url := range m.Images {
			widths = append(widths, imageWidth(t, url))
		}
		told = told || strings.Contains(m.Content, "no longer attached")
	}
	if len(widths) == 0 || len(widths) > 5 {
		t.Fatalf("the last request carried %d screenshots, want between 1 and 5", len(widths))
	}
	if newest := 10 + shots; widths[len(widths)-1] != newest {
		t.Fatalf("the newest screenshot is not in the last request: widths %v", widths)
	}
	if !told {
		t.Fatal("the model was not told which screenshots left the request")
	}
}
