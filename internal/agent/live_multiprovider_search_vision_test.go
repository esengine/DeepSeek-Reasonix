//go:build live

package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveMultiProviderNativeSearch(t *testing.T) {
	if os.Getenv("REASONIX_LIVE_WEB_SEARCH") != "1" {
		t.Skip("live search not enabled")
	}
	for _, tc := range multiProviderCases() {
		if os.Getenv(tc.keyEnv) == "" || tc.protocol == "chat" || !strings.HasPrefix(tc.model, "deepseek-v4-") {
			continue
		}
		t.Run(tc.vendor+"/"+tc.model+"/"+tc.protocol, func(t *testing.T) {
			proxy := &officialRecoveryProxy{protocol: tc.protocol, scenario: "search", upstreamURL: tc.upstream()}
			srv := httptest.NewServer(proxy)
			defer srv.Close()
			p := tc.new(t, srv.URL, "search")
			reg := tool.NewRegistry()
			var calls atomic.Int32
			reg.Add(liveRecoveryEchoTool{executions: &calls})
			sess := NewSession("Use real web search when asked. After searching, call echo once, then cite a source and report the marker.")
			sink := &recordSink{}
			a := New(p, reg, sess, Options{MaxSteps: 4, MaxOutputTokens: 4096, MissingReasoningWarnStateDir: t.TempDir()}, sink)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			err := a.Run(ctx, "Search the web for OpenCode Go documentation. Then call echo exactly once. Report one source URL and the marker.")
			searches, hits := 0, 0
			for _, m := range sess.Snapshot() {
				searches += len(m.ServerSearch)
				for _, s := range m.ServerSearch {
					hits += len(s.Results)
					if provider.HasUsableSearchSources(s.Results) && s.SourcesStatus != provider.SourcesAvailable {
						t.Fatal("sources availability missing")
					}
					if !provider.HasUsableSearchSources(s.Results) && s.SourcesStatus != provider.SourcesNotProvided {
						t.Fatal("completed source-free search has no availability state")
					}
					if len(s.Raw) > 0 {
						var raw any
						if json.Unmarshal(s.Raw, &raw) == nil {
							shape, _ := json.Marshal(liveJSONShape(raw))
							t.Logf("search_item_shape=%s", shape)
						}
					}
				}
			}
			proxy.mu.Lock()
			replies := append([][]byte(nil), proxy.searchResponses...)
			requestsOnWire := append([][]byte(nil), proxy.bodies...)
			proxy.mu.Unlock()
			if tc.protocol == "responses" {
				for _, m := range sess.Snapshot() {
					for _, s := range m.ServerSearch {
						var item map[string]any
						if json.Unmarshal(s.Raw, &item) != nil || item["status"] != "completed" {
							t.Fatal("completed raw search item missing")
						}
						replayed := false
						for _, body := range requestsOnWire[1:] {
							var request struct {
								Input []map[string]any `json:"input"`
							}
							if json.Unmarshal(body, &request) != nil {
								t.Fatal("invalid replay request")
							}
							for _, candidate := range request.Input {
								replayed = replayed || reflect.DeepEqual(candidate, item)
							}
						}
						if !replayed {
							t.Fatal("search item did not replay unchanged on the next model request")
						}
					}
				}
				for _, reply := range replies {
					for _, line := range bytes.Split(reply, []byte("\n")) {
						if !bytes.HasPrefix(line, []byte("data:")) {
							continue
						}
						var frame map[string]any
						if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &frame) != nil {
							continue
						}
						if frame["type"] == "response.completed" {
							shape, _ := json.Marshal(liveJSONShape(frame["response"]))
							t.Logf("terminal_response_shape=%s", shape)
						}
					}
				}
			}
			requests, prompt, output := 0, 0, 0
			unknown := false
			for _, e := range sink.kinds(event.Usage) {
				if u := e.Usage; u != nil {
					requests += u.RequestCount
					prompt += u.PromptTokens
					output += u.CompletionTokens
					unknown = unknown || u.Unknown
				}
			}
			t.Logf("provider=%s model=%s protocol=%s requests=%d searches=%d sources=%d tools=%d prompt=%d completion=%d unknown_usage=%t", tc.vendor, tc.model, tc.protocol, requests, searches, hits, calls.Load(), prompt, output, unknown)
			if err != nil {
				t.Fatal(err)
			}
			if searches == 0 || calls.Load() != 1 {
				t.Fatal("native search metadata or exactly-once tool continuation missing")
			}
			t.Run("structured_sources", func(t *testing.T) {
				if hits == 0 {
					// A completed Responses search can omit action.sources and all
					// message annotations. That proves search/replay, not citations.
					t.Log("search completed without structured sources; availability explicitly not_provided")
				}
			})
		})
	}
}

// Preserve JSON field/array structure for diagnostics, never content, opaque
// reasoning, identifiers or URLs. Replies themselves remain in process memory.
func liveJSONShape(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, child := range x {
			out[k] = liveJSONShape(child)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, child := range x {
			out[i] = liveJSONShape(child)
		}
		return out
	case string:
		return "string"
	case nil:
		return nil
	default:
		return "scalar"
	}
}

// A generated, non-sensitive image tests actual multimodal wire input. No user
// image, file reader or external image URL is sent to any provider.
func TestLiveMultiProviderVision(t *testing.T) {
	fixture := image.NewRGBA(image.Rect(0, 0, 256, 128))
	for y := range 128 {
		for x := range 256 {
			c := color.RGBA{R: 255, A: 255}
			if x >= 128 {
				c = color.RGBA{B: 255, A: 255}
			}
			fixture.SetRGBA(x, y, c)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, fixture); err != nil {
		t.Fatal(err)
	}
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	for _, tc := range multiProviderCases() {
		if os.Getenv(tc.keyEnv) == "" || tc.model != "deepseek-v4-flash-vision-exp" {
			continue
		}
		t.Run(tc.vendor+"/"+tc.protocol, func(t *testing.T) {
			p := tc.new(t, "", "vision")
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			ctx = provider.WithManagedRecovery(provider.WithRequestAttemptCounter(ctx))
			ch, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "Identify the two solid colors, left then right. Reply exactly two lowercase English words separated by a comma.", Images: []string{url}}}, MaxTokens: 4096})
			if err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			prompt, output := 0, 0
			for c := range ch {
				if c.Err != nil {
					t.Fatal(c.Err)
				}
				if c.Type == provider.ChunkText {
					text.WriteString(c.Text)
				}
				if c.Usage != nil {
					prompt = c.Usage.PromptTokens
					output = c.Usage.CompletionTokens
				}
			}
			answer := strings.ToLower(strings.TrimSpace(text.String()))
			answer = strings.ReplaceAll(answer, " ", "")
			t.Logf("provider=%s protocol=%s requests=%d prompt=%d completion=%d correct=%t", tc.vendor, tc.protocol, provider.RequestAttemptCount(ctx), prompt, output, answer == "red,blue")
			if answer != "red,blue" {
				t.Fatalf("color answer mismatch: %q", text.String())
			}
		})
	}
}
