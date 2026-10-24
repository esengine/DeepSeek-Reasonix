package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/session/control"
)

const (
	resourceDeclaredURI = "doc://handbook"
	resourceSecretURI   = "secret://internal/keys"
	resourceBody        = "HANDBOOK-BODY"
)

// resourceMCPServer lists one resource and will also read an address it never
// listed, so a test can tell a refusal at the host from a refusal by the server.
type resourceMCPServer struct {
	*httptest.Server
	mu                sync.Mutex
	reads             []string
	initializeGate    <-chan struct{}
	initializeStarted chan<- struct{}
}

func newResourceMCPServer(t *testing.T, declares bool) *resourceMCPServer {
	t.Helper()
	s := &resourceMCPServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		caps := map[string]any{"tools": map[string]any{}}
		if declares {
			caps["resources"] = map[string]any{}
		}
		var result any
		switch req.Method {
		case "initialize":
			s.mu.Lock()
			wait, started := s.initializeGate, s.initializeStarted
			s.mu.Unlock()
			if started != nil {
				select {
				case started <- struct{}{}:
				default:
				}
			}
			if wait != nil {
				select {
				case <-wait:
				case <-r.Context().Done():
					return
				}
			}
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "res", "version": "1"}, "capabilities": caps}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "ping", "description": "Ping.", "inputSchema": map[string]any{"type": "object"}}}}
		case "resources/list":
			result = map[string]any{"resources": []map[string]any{{"uri": resourceDeclaredURI, "name": "handbook"}}}
		case "resources/templates/list":
			result = map[string]any{"resourceTemplates": []map[string]any{{"uriTemplate": "doc://item/{id}", "name": "item"}}}
		case "resources/read":
			var p struct {
				URI string `json:"uri"`
			}
			_ = json.Unmarshal(req.Params, &p)
			s.mu.Lock()
			s.reads = append(s.reads, p.URI)
			s.mu.Unlock()
			text := resourceBody
			if p.URI == resourceSecretURI {
				text = "SECRET-KEYS"
			}
			result = map[string]any{"contents": []map[string]any{{"uri": p.URI, "mimeType": "text/plain", "text": text}}}
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *resourceMCPServer) readURIs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reads...)
}

type resourceToolProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
	uri  string
}

func (p *resourceToolProvider) Name() string { return "boot-mcp-resource" }

func (p *resourceToolProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	first := len(p.reqs) == 1
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	offered := false
	for _, t := range req.Tools {
		offered = offered || t.Name == "read_mcp_resource"
	}
	if first && offered && p.uri != "" {
		args, _ := json.Marshal(map[string]string{"server": "res", "uri": p.uri})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-res", Name: "read_mcp_resource", Arguments: string(args)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *resourceToolProvider) offered() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) == 0 {
		return false
	}
	for _, t := range p.reqs[0].Tools {
		if t.Name == "read_mcp_resource" {
			return true
		}
	}
	return false
}

func (p *resourceToolProvider) firstTools() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) == 0 {
		return []string{"(no request reached the provider)"}
	}
	var names []string
	for _, t := range p.reqs[0].Tools {
		names = append(names, t.Name)
	}
	return names
}

func (p *resourceToolProvider) lastToolResult() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	results := effectToolResults(p.reqs[len(p.reqs)-1])
	if len(results) == 0 {
		return ""
	}
	return results[len(results)-1]
}

type resourceSession struct {
	rec       *resourceToolProvider
	ctrl      *control.Controller
	server    *resourceMCPServer
	mu        sync.Mutex
	approvals []event.Approval
	registry  *tool.Registry
}

func buildResourceSession(t *testing.T, userOn, declares bool, projectOn bool, uri string, beforeBuild ...func(*resourceMCPServer)) *resourceSession {
	return buildResourceSessionConfigured(t, userOn, declares, projectOn, uri, nil, nil, beforeBuild...)
}

func buildResourceSessionWithReadiness(t *testing.T, userOn, declares bool, projectOn bool, uri string, wait <-chan struct{}, started chan<- struct{}) *resourceSession {
	return buildResourceSessionConfigured(t, userOn, declares, projectOn, uri, wait, started)
}

func buildResourceSessionConfigured(t *testing.T, userOn, declares bool, projectOn bool, uri string, wait <-chan struct{}, started chan<- struct{}, beforeBuild ...func(*resourceMCPServer)) *resourceSession {
	t.Helper()
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	s := &resourceSession{rec: &resourceToolProvider{uri: uri}, server: newResourceMCPServer(t, declares)}
	kind := uniqueKind("boot-mcp-resource-" + strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()))
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return s.rec, nil })
	extra := ""
	load := "always"
	if wait != nil {
		load = "deferred"
	}
	if projectOn {
		extra = "[tools]\nmcp_resources = true\n"
	}
	writeFile(t, workspace, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
%s
[agent]
system_prompt = "BASE"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = %q
model = "x"
[[plugins]]
name = "res"
type = "http"
url = %q
load = %q
`, extra, kind, s.server.URL, load))
	if userOn {
		writeFile(t, reasonixHome, "config.toml", "[tools]\nmcp_resources = true\n")
	}
	approveWorkspace(t, workspace)
	approveProjectServer(t, workspace, "res")
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			s.mu.Lock()
			s.approvals = append(s.approvals, e.Approval)
			s.mu.Unlock()
		}
	})
	// The capability is read from the schema cache of the server's last
	// handshake, so the first session only teaches the host what the server is.
	warm, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := warm.ConnectMCPServer(warm.ConfiguredMCPServers()[0].Entry); err != nil {
		t.Fatal(err)
	}
	warm.Close()
	s.server.mu.Lock()
	s.server.initializeGate, s.server.initializeStarted = wait, started
	s.server.mu.Unlock()
	for _, prepare := range beforeBuild {
		prepare(s.server)
	}
	result, err := BuildRuntime(t.Context(), Options{Sink: sink, Home: reasonixHome, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	ctrl := result.Controller
	s.registry = result.Assembly.Registry
	t.Cleanup(ctrl.Close)
	s.ctrl = ctrl
	ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	ctrl.EnableInteractiveApproval()
	return s
}

func TestEffectMCPResourceReadWaitsForTheAuthorizedConnection(t *testing.T) {
	resume, started := make(chan struct{}), make(chan struct{}, 1)
	var release sync.Once
	defer release.Do(func() { close(resume) })
	s := buildResourceSessionWithReadiness(t, true, true, false, resourceDeclaredURI, resume, started)
	done := make(chan error, 1)
	go func() { done <- s.ctrl.Run(t.Context(), "read the handbook") }()
	var request event.Approval
	waitFor(t, "the resource approval", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.approvals) == 0 {
			return false
		}
		request = s.approvals[0]
		return true
	})
	s.ctrl.Approve(request.ID, true, false, false)
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("resource call returned before connection readiness: %v / %s", err, s.rec.lastToolResult())
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	select {
	case err := <-done:
		t.Fatalf("resource call did not wait for the in-flight handshake: %v / %s", err, s.rec.lastToolResult())
	default:
	}
	release.Do(func() { close(resume) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.rec.lastToolResult(), resourceBody) {
		t.Fatalf("authorized resource was not read after readiness: %s", s.rec.lastToolResult())
	}
}

// The tool exists only where the user's own file turned it on and a connected
// server advertised resources; a project file cannot turn it on.
func TestEffectMCPResourceToolIsOffUnlessTheUserTurnedItOnAndAServerDeclaresResources(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		userOn, declares, projectOnly bool
		want                          bool
	}{
		{"user on, server declares", true, true, false, true},
		{"user on, server declares nothing", true, false, false, false},
		{"off, server declares", false, true, false, false},
		{"only the project file turned it on", false, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := buildResourceSession(t, tc.userOn, tc.declares, tc.projectOnly, "")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := s.ctrl.Run(ctx, "hi"); err != nil {
				t.Fatal(err)
			}
			if got := s.rec.offered(); got != tc.want {
				t.Fatalf("read_mcp_resource offered = %v, want %v", got, tc.want)
			}
		})
	}
}

// Reading a resource asks first and shows the whole address; an address the
// server never listed is refused by the host with a code and never sent to it.
func TestEffectMCPResourceReadAsksWithTheFullAddressAndRefusesWhatTheServerDidNotList(t *testing.T) {
	for _, tc := range []struct {
		name     string
		uri      string
		allow    bool
		wantIn   string
		wantRead bool
	}{
		{"declared, approved", resourceDeclaredURI, true, resourceBody, true},
		{"declared, declined", resourceDeclaredURI, false, "", false},
		{"built by a declared template, approved", "doc://item/9", true, resourceBody, true},
		{"not listed, approved", resourceSecretURI, true, "mcp_resource.not_declared", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := buildResourceSession(t, true, true, false, tc.uri)
			done := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				done <- s.ctrl.Run(ctx, "read the handbook")
			}()
			var request event.Approval
			waitFor(t, "the approval request", func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				if len(s.approvals) == 0 {
					select {
					case err := <-done:
						t.Fatalf("the run ended without an approval card (err=%v); the first request offered %v; the server's cache entry: %s", err, s.rec.firstTools(), resourceCacheEntry())
					default:
					}
					return false
				}
				request = s.approvals[0]
				return true
			})
			if request.Tool != "read_mcp_resource" || !strings.Contains(request.Subject, "res:"+tc.uri) {
				t.Fatalf("the card does not show server and full address: %+v", request)
			}
			if got := s.server.readURIs(); len(got) != 0 {
				t.Fatalf("the server was asked before the user answered: %v", got)
			}
			s.ctrl.Approve(request.ID, tc.allow, false, false)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			read := false
			for _, u := range s.server.readURIs() {
				read = read || u == tc.uri
			}
			if read != tc.wantRead {
				t.Fatalf("server reads = %v, want read=%v", s.server.readURIs(), tc.wantRead)
			}
			if tc.wantIn != "" && !strings.Contains(s.rec.lastToolResult(), tc.wantIn) {
				t.Fatalf("the model saw %q, want it to contain %q", s.rec.lastToolResult(), tc.wantIn)
			}
		})
	}
}

// resourceCacheEntry is what the schema cache holds for the test server, for the
// failure message: whether the second Build could have seen the capability.
func resourceCacheEntry() string {
	raw, err := os.ReadFile(filepath.Join(config.CacheDir(), "mcp", "res.json"))
	if err != nil {
		return "unreadable: " + err.Error()
	}
	var entry struct {
		Capabilities map[string]bool `json:"capabilities"`
		Tools        []any           `json:"tools"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return fmt.Sprintf("not JSON (%d bytes): %v", len(raw), err)
	}
	return fmt.Sprintf("capabilities %v, %d tools", entry.Capabilities, len(entry.Tools))
}
