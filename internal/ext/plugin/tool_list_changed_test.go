package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

// listChangedServer is a Streamable HTTP MCP server whose catalog can change
// between calls. It announces a change in the SSE stream of the response it is
// already answering, which is where a client that opens no standalone stream
// can hear one.
type listChangedServer struct {
	mu      sync.Mutex
	tools   []string
	notices int
	declare bool
	// selfNotify stands for a server that announces on every listing, which is
	// what bounded catch-up exists for.
	selfNotify bool
	// destructive names the tools the server annotates as destructive, which is
	// one of the facts the host's gate reads before a call runs.
	destructive map[string]bool
	lists       atomic.Int64
	calls       atomic.Int64
}

func newListChangedServer(t *testing.T, declare bool, tools ...string) (*listChangedServer, *httptest.Server) {
	t.Helper()
	s := &listChangedServer{tools: tools, declare: declare}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	return s, srv
}

// announce swaps the catalog and arms n notifications for the next response.
func (s *listChangedServer) announce(n int, tools ...string) {
	s.mu.Lock()
	s.tools = tools
	s.notices = n
	s.mu.Unlock()
}

func (s *listChangedServer) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     *int   `json:"id"`
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch req.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo":      map[string]any{"name": "live", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": s.declare}},
		}
	case "tools/list":
		s.lists.Add(1)
		result = map[string]any{"tools": s.catalog()}
		if s.selfNotify {
			s.mu.Lock()
			s.notices++
			s.mu.Unlock()
		}
	case "tools/call":
		s.calls.Add(1)
		result = map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}}
	default:
		result = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})

	s.mu.Lock()
	notices := s.notices
	s.notices = 0
	s.mu.Unlock()
	if notices == 0 {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for range notices {
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":%q}\n\n", toolListChangedMethod)
	}
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
}

func (s *listChangedServer) catalog() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.tools))
	for _, name := range s.tools {
		entry := map[string]any{
			"name":        name,
			"description": "tool " + name,
			"inputSchema": map[string]any{"type": "object"},
		}
		if s.destructive[name] {
			entry["annotations"] = map[string]any{"destructiveHint": true}
		}
		out = append(out, entry)
	}
	return out
}

// connectListChanged connects one server and returns the host plus the catalogs
// its subscriber receives.
func connectListChanged(t *testing.T, srv *httptest.Server, spec Spec) (*Host, <-chan []string) {
	t.Helper()
	spec.Name = "live"
	spec.Type = "http"
	spec.URL = srv.URL
	h := NewHost()
	t.Cleanup(h.Close)
	published := make(chan []string, 16)
	h.SubscribeToolListChanges(t.Context(), func(_ Spec, tools []tool.Tool) {
		published <- names(tools)
	})
	if _, err := h.Add(t.Context(), spec); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return h, published
}

func waitForCatalog(t *testing.T, published <-chan []string) []string {
	t.Helper()
	select {
	case got := <-published:
		return got
	case <-time.After(10 * time.Second):
		t.Fatal("no refreshed catalog was published after notifications/tools/list_changed")
		return nil
	}
}

// A server that declared tools.listChanged may add and drop tools after
// startup. Ignoring the notice leaves the added one unreachable and the dropped
// one still offered.
func TestToolListChangedRepublishesTheCatalog(t *testing.T) {
	server, srv := newListChangedServer(t, true, "alpha", "beta")
	h, published := connectListChanged(t, srv, Spec{})

	server.announce(1, "alpha", "gamma")
	if _, err := h.ToolsFor(t.Context(), "live"); err != nil {
		t.Fatalf("ToolsFor: %v", err)
	}
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}

	got := waitForCatalog(t, published)
	want := []string{"mcp__live__alpha", "mcp__live__gamma"}
	if !slices.Equal(got, want) {
		t.Fatalf("published catalog = %v, want %v", got, want)
	}
	cached, err := h.ToolsFor(t.Context(), "live")
	if err != nil {
		t.Fatalf("ToolsFor after refresh: %v", err)
	}
	if !slices.Equal(names(cached), want) {
		t.Fatalf("host catalog = %v, want %v", names(cached), want)
	}
	if status := h.Servers(); len(status) != 1 || status[0].Tools != 2 {
		t.Fatalf("status = %+v, want one server reporting 2 tools", status)
	}
}

// Servers announce once per tool while they register, so a burst is the normal
// case. Each notice costs a round trip to the same server, so a burst must
// settle into one re-list.
func TestToolListChangedCoalescesABurst(t *testing.T) {
	server, srv := newListChangedServer(t, true, "alpha")
	h, published := connectListChanged(t, srv, Spec{})
	listsAfterConnect := server.lists.Load()

	server.announce(10, "alpha", "beta")
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}

	waitForCatalog(t, published)
	if got := server.lists.Load() - listsAfterConnect; got != 1 {
		t.Fatalf("ten notifications caused %d tools/list calls, want 1", got)
	}
}

// Only a server that said it would announce changes is followed: the spec lets
// a client assume a catalog is fixed until it hears otherwise.
func TestToolListChangedIgnoredWhenNotDeclared(t *testing.T) {
	server, srv := newListChangedServer(t, false, "alpha")
	h, published := connectListChanged(t, srv, Spec{})
	listsAfterConnect := server.lists.Load()

	server.announce(1, "alpha", "beta")
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}

	select {
	case got := <-published:
		t.Fatalf("an undeclared server's notice refreshed the catalog to %v", got)
	case <-time.After(2 * time.Second):
	}
	if got := server.lists.Load() - listsAfterConnect; got != 0 {
		t.Fatalf("tools/list ran %d times for an undeclared server, want 0", got)
	}
}

// A tool the configuration disabled stays disabled when the server adds it
// back: the refresh re-applies the same policy the first listing did.
func TestToolListChangedKeepsDisabledToolsOut(t *testing.T) {
	server, srv := newListChangedServer(t, true, "alpha")
	h, published := connectListChanged(t, srv, Spec{DisabledTools: []string{"gamma"}})

	server.announce(1, "alpha", "beta", "gamma")
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}

	got := waitForCatalog(t, published)
	want := []string{"mcp__live__alpha", "mcp__live__beta"}
	if !slices.Equal(got, want) {
		t.Fatalf("published catalog = %v, want %v without the disabled tool", got, want)
	}
}

// The call that carries the announcement is the one whose tool is being
// dropped. It must land on its own answer: the adapter it was dispatched
// through outlives the catalog that produced it.
func TestToolListChangedDoesNotDisturbTheCallThatCarriedIt(t *testing.T) {
	server, srv := newListChangedServer(t, true, "alpha", "beta")
	h, published := connectListChanged(t, srv, Spec{})
	tools, err := h.ToolsFor(t.Context(), "live")
	if err != nil {
		t.Fatalf("ToolsFor: %v", err)
	}
	beta := tools[slices.IndexFunc(tools, func(x tool.Tool) bool { return x.Name() == "mcp__live__beta" })]

	server.announce(1, "alpha")
	if _, err := beta.Execute(t.Context(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("the call that carried the announcement failed: %v", err)
	}

	got := waitForCatalog(t, published)
	if !slices.Equal(got, []string{"mcp__live__alpha"}) {
		t.Fatalf("published catalog = %v, want the dropped tool gone", got)
	}
	left, err := h.ToolsFor(t.Context(), "live")
	if err != nil {
		t.Fatalf("ToolsFor after refresh: %v", err)
	}
	if slices.Contains(names(left), "mcp__live__beta") {
		t.Fatalf("host catalog = %v, still carries the dropped tool", names(left))
	}
}

// A backend rolled in behind the stable proxy is a new connection with no
// handlers of its own. It has to be watched too, or a server replaced mid
// session goes quiet for the rest of it.
func TestToolListChangedFollowsAReplacedBackend(t *testing.T) {
	h := NewHost()
	t.Cleanup(h.Close)
	published := make(chan []string, 4)
	h.SubscribeToolListChanges(t.Context(), func(_ Spec, tools []tool.Tool) {
		published <- names(tools)
	})

	backend := &recordingNotificationTransport{result: `{"tools":[{"name":"alpha","inputSchema":{"type":"object"}}]}`}
	next := &Client{name: "live", spec: Spec{Name: "live"}, t: backend}
	next.declareToolListChanged(true)
	if err := h.ReplaceServerBackend(t.Context(), "live", next, 1); err != nil {
		t.Fatalf("ReplaceServerBackend: %v", err)
	}

	backend.notices.dispatchNotification(toolListChangedMethod, nil)
	if got := waitForCatalog(t, published); !slices.Equal(got, []string{"mcp__live__alpha"}) {
		t.Fatalf("published catalog = %v, want the replaced backend's own tools", got)
	}
}

// A connection that dies takes its handlers with it. The replacement must be
// listening too, or a server that reconnects and then changes its tools is
// never heard again.
func TestNotificationRegistrationSurvivesAReconnect(t *testing.T) {
	first, second := &recordingNotificationTransport{diesOnce: true}, &recordingNotificationTransport{}
	rt := newReconnectingTransport(context.Background(), first, time.Second,
		func(context.Context, context.Context) (transport, error) { return second, nil },
		func(context.Context, transport) error { return nil })

	var heard atomic.Int64
	stop := rt.registerNotification(toolListChangedMethod, func(json.RawMessage) { heard.Add(1) })
	first.notices.dispatchNotification(toolListChangedMethod, nil)

	if _, err := rt.call(context.Background(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("call over a dead connection: %v", err)
	}
	second.notices.dispatchNotification(toolListChangedMethod, nil)
	if got := heard.Load(); got != 2 {
		t.Fatalf("handler heard %d notifications, want 2 (one per connection)", got)
	}

	stop()
	second.notices.dispatchNotification(toolListChangedMethod, nil)
	if got := heard.Load(); got != 2 {
		t.Fatalf("handler heard %d notifications after unregistering, want 2", got)
	}
}

// recordingNotificationTransport carries notifications and, when diesOnce is
// set, reports its first call as gone so the reconnecting transport dials the
// replacement.
type recordingNotificationTransport struct {
	notices  notificationRouter
	result   string
	diesOnce bool
	died     atomic.Bool
	calls    atomic.Int64
}

func (t *recordingNotificationTransport) call(context.Context, string, any) (json.RawMessage, error) {
	t.calls.Add(1)
	if t.diesOnce && t.died.CompareAndSwap(false, true) {
		return nil, markGone(fmt.Errorf("connection ended"))
	}
	if t.result == "" {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(t.result), nil
}

func (t *recordingNotificationTransport) notify(context.Context, string, any) error { return nil }
func (t *recordingNotificationTransport) close()                                    {}
func (t *recordingNotificationTransport) registerNotification(method string, handler notificationFunc) func() {
	return t.notices.registerNotification(method, handler)
}

// stubToolRefreshWait takes the debounce off the clock so a test can drive a
// whole catch-up cycle without waiting out its backoff.
func stubToolRefreshWait(c *Client) {
	c.refresh.mu.Lock()
	c.refresh.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	c.refresh.mu.Unlock()
}

// Some servers announce on every tools/list. Catch-up has to stop on its own:
// the alternative is a client and a server listing at each other for as long as
// the session lives.
func TestToolListChangedStopsCatchingUpWithASelfNotifyingServer(t *testing.T) {
	server, srv := newListChangedServer(t, true, "alpha")
	server.selfNotify = true
	h, published := connectListChanged(t, srv, Spec{})
	stubToolRefreshWait(h.lookupClient("live"))
	listsAfterConnect := server.lists.Load()

	server.announce(1, "alpha", "beta")
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	waitForCatalog(t, published)

	// Every re-list arms the next notice, so this settles only because the cycle
	// is bounded. Read it after it has had time to run away if it could.
	time.Sleep(500 * time.Millisecond)
	if got := server.lists.Load() - listsAfterConnect; got > toolListRefreshAttempts {
		t.Fatalf("a self-notifying server drew %d re-lists, want at most %d", got, toolListRefreshAttempts)
	}
}

// A notice whose re-list finds the same tools must publish nothing: every
// publication re-registers a server's whole prefix in every live session.
func TestToolListChangedPublishesNothingWhenTheCatalogIsUnchanged(t *testing.T) {
	server, srv := newListChangedServer(t, true, "alpha")
	h, published := connectListChanged(t, srv, Spec{})
	listsAfterConnect := server.lists.Load()

	server.announce(1, "alpha") // same catalog, announced anyway
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}

	select {
	case got := <-published:
		t.Fatalf("an unchanged catalog was published as %v", got)
	case <-time.After(2 * time.Second):
	}
	if got := server.lists.Load() - listsAfterConnect; got != 1 {
		t.Fatalf("tools/list ran %d times, want the one re-read the notice asked for", got)
	}
}

// A connection on its way out must not start a refresh. The transport here
// keeps answering after close on purpose: a test whose server simply stops
// replying would pass whether or not the watch was ever stopped.
func TestToolListChangedStopsWhenTheConnectionCloses(t *testing.T) {
	h := NewHost()
	t.Cleanup(h.Close)
	published := make(chan []string, 4)
	h.SubscribeToolListChanges(t.Context(), func(_ Spec, tools []tool.Tool) {
		published <- names(tools)
	})

	backend := &recordingNotificationTransport{result: `{"tools":[{"name":"alpha","inputSchema":{"type":"object"}}]}`}
	client := &Client{name: "live", spec: Spec{Name: "live"}, t: backend}
	client.declareToolListChanged(true)
	if err := h.ReplaceServerBackend(t.Context(), "live", client, 1); err != nil {
		t.Fatalf("ReplaceServerBackend: %v", err)
	}
	client.close()

	backend.notices.dispatchNotification(toolListChangedMethod, nil)
	select {
	case got := <-published:
		t.Fatalf("a closed connection published %v", got)
	case <-time.After(time.Second):
	}
	if got := backend.calls.Load(); got != 0 {
		t.Fatalf("a closed connection issued %d calls, want 0", got)
	}
}

func TestNextToolRefreshDelayClimbsToItsCeiling(t *testing.T) {
	for _, c := range []struct{ from, want time.Duration }{
		{0, toolListRefreshDebounce},
		{toolListRefreshDebounce, 2 * toolListRefreshDebounce},
		{toolListRefreshBackoff / 2, toolListRefreshBackoff},
		{toolListRefreshBackoff, toolListRefreshBackoff},
	} {
		if got := nextToolRefreshDelay(c.from); got != c.want {
			t.Errorf("nextToolRefreshDelay(%s) = %s, want %s", c.from, got, c.want)
		}
	}
}

// The next session pins its provider surface from the schema cache, so a
// catalog that moved during this one has to be what that file holds.
func TestToolListChangedRewritesTheSchemaCache(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
	server, srv := newListChangedServer(t, true, "alpha")
	h, published := connectListChanged(t, srv, Spec{})
	spec := h.lookupClient("live").spec

	server.announce(1, "alpha", "gamma")
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	waitForCatalog(t, published)

	cached, ok := LoadCachedSchemaForSpec(spec)
	if !ok {
		t.Fatal("no schema was cached for a server whose catalog changed")
	}
	var names []string
	for _, cachedTool := range cached.Tools {
		names = append(names, cachedTool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"alpha", "gamma"}) {
		t.Fatalf("cached catalog = %v, want the one the server moved to", names)
	}
}

// A connected server can add a tool after startup, which is the boundary this
// change moves. What arrives has to reach the host's gate carrying the same
// facts a startup tool does — the server's authorization and its own
// annotations — or a tool could get past a check by arriving late.
func TestToolListChangedCarriesTheStartupSecurityFacts(t *testing.T) {
	server, srv := newListChangedServer(t, true, "alpha")
	server.destructive = map[string]bool{"gamma": true}
	h, published := connectListChanged(t, srv, Spec{Authorized: true})

	startup, err := h.ToolsFor(t.Context(), "live")
	if err != nil {
		t.Fatalf("ToolsFor: %v", err)
	}
	if len(startup) != 1 || !tool.IsMCPServerAuthorized(startup[0]) {
		t.Fatalf("startup tool authorization = %v, want the spec's", names(startup))
	}

	server.announce(1, "alpha", "gamma")
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	waitForCatalog(t, published)

	refreshed, err := h.ToolsFor(t.Context(), "live")
	if err != nil {
		t.Fatalf("ToolsFor after refresh: %v", err)
	}
	byName := map[string]tool.Tool{}
	for _, tl := range refreshed {
		byName[tl.Name()] = tl
	}
	arrived, ok := byName["mcp__live__gamma"]
	if !ok {
		t.Fatalf("the added tool never reached the catalog: %v", names(refreshed))
	}
	if !tool.IsMCPServerAuthorized(arrived) {
		t.Fatal("a tool that arrived mid-session lost the server authorization its siblings carry")
	}
	if !tool.HasMCPDestructiveHint(arrived) {
		t.Fatal("a tool that arrived mid-session lost the destructive hint it declared")
	}
	if tool.HasMCPDestructiveHint(byName["mcp__live__alpha"]) {
		t.Fatal("the refresh marked an undeclared tool destructive")
	}
}

// An unauthorized server stays unauthorized for whatever it adds later: the
// approval is the user's answer about the server, and a server cannot answer
// it for them by announcing a new tool.
func TestToolListChangedCannotSelfAuthorize(t *testing.T) {
	server, srv := newListChangedServer(t, true, "alpha")
	h, published := connectListChanged(t, srv, Spec{})

	server.announce(1, "alpha", "gamma")
	if _, err := h.lookupClient("live").call(t.Context(), "tools/call",
		map[string]any{"name": "alpha", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	waitForCatalog(t, published)

	refreshed, err := h.ToolsFor(t.Context(), "live")
	if err != nil {
		t.Fatalf("ToolsFor after refresh: %v", err)
	}
	for _, tl := range refreshed {
		if tool.IsMCPServerAuthorized(tl) {
			t.Fatalf("%s reported an authorization the user never gave", tl.Name())
		}
	}
}
