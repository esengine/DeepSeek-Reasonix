package boot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
)

func TestRejectedMCPCacheDiscoversWithoutPinning(t *testing.T) {
	for _, tc := range []struct {
		name                string
		always, freezeEarly bool
	}{
		{"deferred", false, false},
		{"always_before_handshake", true, true},
		{"always_after_handshake", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			listed, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				case "initialize":
					result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}}
				case "tools/list":
					once.Do(func() { close(listed) })
					select {
					case <-release:
					case <-ctx.Done():
						return
					}
					result = map[string]any{"tools": []map[string]any{{"name": "read", "inputSchema": map[string]any{"type": "object"}}}}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
			}))
			defer server.Close()
			spec := plugin.Spec{Name: "srv", Type: "http", URL: server.URL, Authorized: true}
			cs := plugin.CachedSchema{CacheKey: plugin.SchemaCacheKey(spec)}
			for _, raw := range []string{"a.b", strings.TrimPrefix(plugin.ModelToolName(spec.Name, "a.b"), plugin.ToolPrefix(spec.Name))} {
				cs.Tools = append(cs.Tools, plugin.CachedTool{Name: raw, Schema: json.RawMessage(`{"type":"object"}`)})
			}
			if err := plugin.SaveCachedSchema(spec.Name, cs); err != nil {
				t.Fatal(err)
			}
			host := plugin.NewHost()
			defer host.Close()
			reg := tool.NewRegistry()
			plan := mcpSpecPlan{configured: []plugin.Spec{spec}, alwaysLoad: map[string]bool{}}
			if tc.always {
				plan.alwaysLoad[spec.Name] = true
			}
			_, known := registerMCPTools(ctx, host, reg, plan, event.Discard)
			if known[spec.Name] {
				t.Error("rejected cache was reported as a known schema")
			}
			select {
			case <-listed:
			case <-ctx.Done():
				t.Fatal("rejected cache did not start catalog discovery")
			}
			freeze := func() {
				applyUnifiedProviderToolSurface(reg, false, ablation.Set{}, pinnedMCPServers(plan.alwaysLoad, known))
			}
			if tc.freezeEarly {
				freeze()
			}
			close(release)
			for {
				if _, ok := reg.Get("mcp__srv__read"); ok {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("discovered tool was not registered")
				case <-time.After(time.Millisecond):
				}
			}
			if !tc.freezeEarly {
				freeze()
			}
			if len(reg.Schemas()) != 0 {
				t.Fatalf("newly discovered tools reached the pinned surface: %v", reg.Schemas())
			}
			host.Close()
			next := plugin.NewHost()
			defer next.Close()
			nextReg := tool.NewRegistry()
			_, known = registerMCPTools(ctx, next, nextReg, plan, event.Discard)
			if !known[spec.Name] {
				t.Fatal("valid discovered cache was not known in the next session")
			}
			applyUnifiedProviderToolSurface(nextReg, false, ablation.Set{}, pinnedMCPServers(plan.alwaysLoad, known))
			if visible := len(nextReg.Schemas()) != 0; visible != tc.always {
				t.Fatalf("next-session tool visibility = %v, want %v", visible, tc.always)
			}
		})
	}
}
