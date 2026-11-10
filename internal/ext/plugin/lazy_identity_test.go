package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestLazyCachedRawIdentityAcrossHTTPDiscovery(t *testing.T) {
	long := strings.Repeat("x", 150)
	for _, tc := range []struct{ name, raw, live, strip string }{
		{"normalized replacement", "a.b", normalizeName("a.b"), ""},
		{"shortened replacement", long, strings.TrimPrefix(toolName("srv", long), ToolPrefix("srv")), ""},
		{"stripped replacement", "srv_read", "read", "srv_"},
		{"unchanged normalized", "a.b", "a.b", ""},
		{"unchanged shortened", long, long, ""},
		{"unchanged stripped", "srv_read", "srv_read", "srv_"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectCache(t)
			var replacement atomic.Bool
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID     *int   `json:"id"`
					Method string `json:"method"`
					Params struct {
						Name string `json:"name"`
					} `json:"params"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == nil {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				var result any
				switch req.Method {
				case "initialize":
					result = map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}}
				case "tools/list":
					raw := tc.raw
					if replacement.Load() {
						raw = tc.live
					}
					result = map[string]any{"tools": []mcpTool{pinTool(raw, "Example")}}
				case "tools/call":
					calls.Add(1)
					result = map[string]any{"content": []map[string]string{{"type": "text", "text": req.Params.Name}}}
				}
				writeHTTPRPCResult(w, req.ID, result)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			spec := Spec{Name: "srv", Type: "http", URL: server.URL, StripRawPrefix: tc.strip, StateDir: filepath.Join(testenv.TempDir(t), "srv")}
			first := NewHost()
			defer first.Close()
			live, err := first.Add(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			saveLazyCachedSchema(spec, live, "", map[string]bool{"tools": len(live) > 0})
			first.Close()
			cs, ok := LoadCachedSchemaForSpec(spec)
			if !ok || len(cs.Tools) != 1 || cs.Tools[0].Name != tc.raw {
				t.Fatalf("persisted cache = %+v, loaded=%v", cs, ok)
			}
			replacement.Store(true)
			host := NewHost()
			defer host.Close()
			reg := tool.NewRegistry()
			lazy := LazyToolset(spec, cs, host, reg, ctx, false)
			reg.Add(lazy[0])
			before := registrySchemaBytes(t, reg)
			for range 2 {
				output, err := lazy[0].Execute(ctx, json.RawMessage(`{}`))
				if tc.raw != tc.live {
					if err == nil || output != "" || calls.Load() != 0 {
						t.Fatalf("cached raw %q dispatched live raw %q: output=%q err=%v calls=%d", tc.raw, tc.live, output, err, calls.Load())
					}
				} else if err != nil || output != tc.raw {
					t.Fatalf("unchanged raw dispatch = (%q, %v), want %q", output, err, tc.raw)
				}
			}
			if after := registrySchemaBytes(t, reg); after != before {
				t.Fatal("cached provider tool bytes changed during discovery")
			}
			live, err = host.ToolsForSpec(ctx, spec)
			if err != nil || len(live) != 1 || live[0].Name() != lazy[0].Name() {
				t.Fatalf("live catalog = %v, err=%v; want the same callable name", names(live), err)
			}
			if output, err := live[0].Execute(ctx, nil); err != nil || output != tc.live {
				t.Fatalf("direct live dispatch = (%q, %v), want %q", output, err, tc.live)
			}
			host.Close()
			cs, ok = LoadCachedSchemaForSpec(spec)
			if !ok || len(cs.Tools) != 1 || cs.Tools[0].Name != tc.live {
				t.Fatalf("refreshed cache = %+v, loaded=%v", cs, ok)
			}
			next := NewHost()
			defer next.Close()
			fresh := LazyToolset(spec, cs, next, tool.NewRegistry(), ctx, false)
			if output, err := fresh[0].Execute(ctx, nil); err != nil || output != tc.live {
				t.Fatalf("next-session dispatch = (%q, %v), want %q", output, err, tc.live)
			}
		})
	}
}

type lazyIdentityTarget struct {
	mutableLazyTarget
	server, raw string
}

func (t *lazyIdentityTarget) MCPServerName() string  { return t.server }
func (t *lazyIdentityTarget) MCPRawToolName() string { return t.raw }

func TestLazyCachedIdentityRejectsUnknownOrDifferentServer(t *testing.T) {
	const name = "mcp__srv__mutate"
	for _, target := range []tool.Tool{
		testTool{name: name},
		&lazyIdentityTarget{mutableLazyTarget: mutableLazyTarget{name: name}, server: "other", raw: "mutate"},
		&lazyIdentityTarget{mutableLazyTarget: mutableLazyTarget{name: name}, server: "srv", raw: ""},
	} {
		host := NewHost()
		defer host.Close()
		shared := &lazySpawn{spec: Spec{Name: "srv"}, host: host, state: spawnReady, swapped: true, real: map[string]tool.Tool{name: target}}
		lazy := &lazyTool{shared: shared, name: name, rawName: "mutate", hasCache: true}
		if output, err := lazy.Execute(t.Context(), nil); err == nil || output != "" {
			t.Fatalf("unknown or different identity dispatched %T: output=%q err=%v", target, output, err)
		}
		if target, ok := target.(*lazyIdentityTarget); ok && target.calls != 0 {
			t.Fatalf("identity mismatch made %d calls", target.calls)
		}
	}
}
