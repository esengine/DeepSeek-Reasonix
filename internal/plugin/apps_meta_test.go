package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"reasonix/internal/tool"
)

// appsFixtureServer advertises the Apps extension and serves tools with
// visibility/_meta.ui metadata, capturing tools/list as the client sees it.
type appsFixtureServer struct {
}

func (f *appsFixtureServer) server(t *testing.T, advertiseApps bool) *mcpsdk.Server {
	t.Helper()
	capabilities := &mcpsdk.ServerCapabilities{}
	if advertiseApps {
		capabilities.AddExtension(AppsUIExtensionID, map[string]any{"mimeTypes": []any{AppsMimeType}})
	}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "apps-fixture", Version: "1"}, &mcpsdk.ServerOptions{
		Capabilities: capabilities,
	})
	addAppTool := func(name string, meta map[string]any) {
		t := &mcpsdk.Tool{
			Name:        name,
			Description: name + " tool",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Meta:        mcpsdk.Meta(meta),
		}
		server.AddTool(t, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: name + " done"}},
				StructuredContent: map[string]any{"ok": true, "tool": name},
			}, nil
		})
	}
	addAppTool("both_tool", map[string]any{})
	// Top-level visibility is retained for pre-stable servers.
	addAppTool("model_only", map[string]any{"visibility": []string{"model"}})
	addAppTool("app_only", map[string]any{
		"ui": map[string]any{
			"visibility":  []string{"app"},
			"resourceUri": "ui://stable/index.html",
		},
	})
	addAppTool("app_rich", map[string]any{
		"ui": map[string]any{
			"resourceUri": "ui://app/rich.html",
			"csp":         map[string]any{"connect-src": []string{"https://api.example.com"}},
			"visibility":  []string{"model", "app"},
		},
	})
	addAppTool("nested_wins", map[string]any{
		"visibility": []string{"model"},
		"ui": map[string]any{
			"visibility": []string{"app"},
		},
	})
	server.AddTool(&mcpsdk.Tool{
		Name:        "complete_result",
		Description: "complete CallToolResult fixture",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Meta:        mcpsdk.Meta{"ui": map[string]any{"visibility": []string{"app"}}},
	}, func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		size := int64(12)
		return &mcpsdk.CallToolResult{
			Meta: mcpsdk.Meta{"trace": "app-call"},
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: "complete result"},
				&mcpsdk.ResourceLink{URI: "https://example.test/result", Name: "result", MIMEType: "application/json", Size: &size, Meta: mcpsdk.Meta{"resource": "metadata"}},
			},
			StructuredContent: map[string]any{"ok": false, "reason": "fixture"},
			IsError:           true,
		}, nil
	})
	server.AddResource(
		&mcpsdk.Resource{URI: "ui://app/rich.html", Name: "rich-app", MIMEType: "text/html;profile=mcp-app"},
		func(context.Context, *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
			return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{
				URI: "ui://app/rich.html", MIMEType: "text/html;profile=mcp-app", Text: "<html>rich</html>",
				Meta: mcpsdk.Meta{"ui": map[string]any{"csp": map[string]any{
					"connectDomains":  []string{"https://api.resource.example"},
					"resourceDomains": []string{"https://cdn.resource.example"},
				}}},
			}}}, nil
		},
	)
	return server
}

// startAppsClient connects a desktop-profile client through the real build
// path and returns the started Client plus the fixture for assertions.
func startAppsClientWithAgreement(t *testing.T, advertiseApps bool) (*Host, *Client, toolCatalogSnapshot, *appsFixtureServer) {
	t.Helper()
	fixture := &appsFixtureServer{}
	host := NewHostWithProfile(HostProfileDesktopApps)
	lifeCtx, cancel := context.WithCancel(context.Background())
	transport := &sdkSessionTransport{
		name:            "apps-fixture",
		spec:            Spec{Name: "apps-fixture", Type: "http", StartupTimeout: 2 * time.Second},
		profile:         HostProfileDesktopApps,
		lifeCtx:         lifeCtx,
		cancel:          cancel,
		state:           SessionStateConnecting,
		reconnectDelays: []time.Duration{time.Millisecond},
	}
	transport.endpointFactory = func(ctx context.Context) (sdkEndpoint, error) {
		clientSide, serverSide := mcpsdk.NewInMemoryTransports()
		go func() { _ = fixture.server(t, advertiseApps).Run(ctx, serverSide) }()
		return sdkEndpoint{transport: clientSide}, nil
	}
	t.Cleanup(transport.close)
	client := &Client{name: "apps-fixture", t: transport, spec: Spec{Name: "apps-fixture"}, profile: HostProfileDesktopApps, transport: "http"}
	if err := client.initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := client.listTools(t.Context()); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	client.toolsMu.RLock()
	snapshot := client.toolCatalog
	snapshot.infos = append([]ToolInfo(nil), snapshot.infos...)
	snapshot.adapters = append([]tool.Tool(nil), snapshot.adapters...)
	snapshot.appAdapters = append([]tool.Tool(nil), snapshot.appAdapters...)
	client.toolsMu.RUnlock()
	return host, client, snapshot, fixture
}

func startAppsClient(t *testing.T) (*Host, *Client, toolCatalogSnapshot, *appsFixtureServer) {
	t.Helper()
	return startAppsClientWithAgreement(t, true)
}

func TestMetaVisibilitySplitsCatalogs(t *testing.T) {
	_, _, catalog, _ := startAppsClient(t)

	var modelNames, appNames []string
	for _, tl := range catalog.adapters {
		modelNames = append(modelNames, tl.Name())
	}
	for _, tl := range catalog.appAdapters {
		appNames = append(appNames, tl.Name())
	}
	joined := strings.Join(modelNames, ",")
	for _, banned := range []string{"app_only", "nested_wins", "complete_result"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("model catalog contains %s: %v", banned, modelNames)
		}
	}
	for _, want := range []string{"both_tool", "model_only", "app_rich"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("model catalog missing %s: %v", want, modelNames)
		}
	}
	appJoined := strings.Join(appNames, ",")
	for _, want := range []string{"both_tool", "app_only", "app_rich", "nested_wins", "complete_result"} {
		if !strings.Contains(appJoined, want) {
			t.Fatalf("app catalog missing %s: %v", want, appNames)
		}
	}
	if strings.Contains(appJoined, "model_only") {
		t.Fatalf("app catalog contains model-only tool: %v", appNames)
	}
	// ToolInfo (use_capability list source) must also exclude app-only.
	for _, info := range catalog.infos {
		if info.Name == "app_only" {
			t.Fatal("app-only tool visible in ToolInfo list")
		}
	}
}

func TestMetaUIResourceNestedAndFlat(t *testing.T) {
	_, _, catalog, _ := startAppsClient(t)
	byName := map[string]tool.Tool{}
	for _, tl := range catalog.appAdapters {
		byName[tl.Name()] = tl
	}
	rich, ok := byName[toolName("apps-fixture", "app_rich")].(*remoteTool)
	if !ok || rich.UIResourceURI() != "ui://app/rich.html" {
		t.Fatalf("nested ui.resourceUri not parsed: %+v", rich)
	}
	if len(rich.UICSP()["connect-src"]) != 1 || rich.UICSP()["connect-src"][0] != "https://api.example.com" {
		t.Fatalf("csp not parsed: %v", rich.UICSP())
	}
	legacy, ok := byName[toolName("apps-fixture", "app_only")].(*remoteTool)
	if !ok || legacy.UIResourceURI() != "ui://stable/index.html" {
		t.Fatalf("stable nested resourceUri not parsed: %+v", legacy)
	}
}

func TestAppsRequireTwoWayExtensionAgreement(t *testing.T) {
	host, client, catalog, _ := startAppsClientWithAgreement(t, false)
	if client.appsNegotiated() {
		t.Fatal("Apps negotiated without the server extension")
	}
	if len(catalog.appAdapters) != 0 {
		t.Fatalf("app catalog populated without agreement: %v", catalog.appAdapters)
	}
	for _, info := range catalog.infos {
		if info.Name == "app_only" || info.Name == "nested_wins" || info.Name == "complete_result" {
			t.Fatalf("stable app-only tool leaked to the model catalog: %q", info.Name)
		}
	}
	host.mu.Lock()
	host.clients = append(host.clients, client)
	host.mu.Unlock()
	inst := host.RegisterAppInstance("apps-fixture", "app_rich", catalog.generation, "call", "ui://app/rich.html")
	if _, ok := host.AppInstanceResourceDescriptor(inst.Token); ok {
		t.Fatal("App resource opened without two-way extension agreement")
	}
	var rich *remoteTool
	for _, candidate := range catalog.adapters {
		if rt, ok := candidate.(*remoteTool); ok && rt.rawName == "app_rich" {
			rich = rt
		}
	}
	if rich == nil {
		t.Fatal("model-visible app_rich tool not found")
	}
	ctx, collector := tool.WithMCPAppCollector(t.Context())
	if _, _, err := rich.ExecuteWithImages(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if collector.Server != "" {
		t.Fatal("rich presentation stamped without two-way extension agreement")
	}
}

func TestAppCallResultPreservesStandardFields(t *testing.T) {
	_, _, catalog, _ := startAppsClient(t)
	var complete *remoteTool
	for _, candidate := range catalog.appAdapters {
		if rt, ok := candidate.(*remoteTool); ok && rt.rawName == "complete_result" {
			complete = rt
		}
	}
	if complete == nil {
		t.Fatal("complete_result App tool not found")
	}
	raw, text, reportedError, err := complete.ExecuteForApp(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if text != "complete result" || !reportedError {
		t.Fatalf("host projection = %q, isError=%v", text, reportedError)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	content, _ := result["content"].([]any)
	resource, _ := content[1].(map[string]any)
	meta, _ := resource["_meta"].(map[string]any)
	if result["isError"] != true || result["structuredContent"] == nil || result["_meta"] == nil || meta["resource"] != "metadata" {
		t.Fatalf("complete CallToolResult was not preserved: %s", raw)
	}
}

func TestRichResultStampedOnCallContext(t *testing.T) {
	_, _, catalog, _ := startAppsClient(t)
	var rich *remoteTool
	for _, tl := range catalog.adapters {
		if rt, ok := tl.(*remoteTool); ok && rt.rawName == "app_rich" {
			rich = rt
			break
		}
	}
	if rich == nil {
		t.Fatal("app_rich not found")
	}
	ctx, collector := tool.WithMCPAppCollector(t.Context())
	out, _, err := rich.ExecuteWithImages(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "app_rich done") {
		t.Fatalf("text form lost: %q", out)
	}
	stamped := collector.Sanitized()
	if stamped == nil || stamped.Server == "" {
		t.Fatal("Apps presentation not collected")
	}
	if stamped.Server != "apps-fixture" || stamped.Tool != "app_rich" || stamped.ResourceURI != "ui://app/rich.html" {
		t.Fatalf("stamped identity = %+v", stamped)
	}
	if len(stamped.Structured) == 0 || !strings.Contains(string(stamped.Structured), `"tool":"app_rich"`) {
		t.Fatalf("structured content not captured: %s", stamped.Structured)
	}
}

func TestAppInstanceRegistryBoundAndReclaimed(t *testing.T) {
	host := NewHostWithProfile(HostProfileDesktopApps)
	reg := host.appInstances
	inst := host.RegisterAppInstance("srv", "tool", 3, "call-1", "ui://x/a.html")
	if len(inst.Token) != 48 {
		t.Fatalf("token length = %d, want 48 hex chars", len(inst.Token))
	}
	if got, ok := host.LookupAppInstance(inst.Token); !ok || got.Server != "srv" {
		t.Fatalf("lookup failed: %+v %v", got, ok)
	}
	for range maxAppInstances + 4 {
		host.RegisterAppInstance("srv", "tool", 3, "call", "ui://x/b.html")
	}
	if reg.Len() > maxAppInstances {
		t.Fatalf("registry exceeded bound: %d", reg.Len())
	}
	if _, ok := host.LookupAppInstance(inst.Token); ok {
		t.Fatal("oldest instance not evicted at capacity")
	}
	other := host.RegisterAppInstance("other", "t", 1, "c", "ui://y/a.html")
	host.appInstances.ReleaseServer("other")
	if _, ok := reg.Lookup(other.Token); ok {
		t.Fatal("release-server did not reclaim the server's instances")
	}
}

func TestAppInstanceReleaseCancelsNestedCalls(t *testing.T) {
	host := NewHostWithProfile(HostProfileDesktopApps)
	inst := host.RegisterAppInstance("srv", "tool", 3, "call", "ui://x/a.html")
	ctx, ok := host.AppInstanceContext(inst.Token)
	if !ok || ctx.Err() != nil {
		t.Fatal("live App instance has no call context")
	}
	host.ReleaseAppInstance(inst.Token)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("releasing App instance did not cancel nested calls")
	}
}

func TestReadResourceForAppReturnsResourceLevelCSP(t *testing.T) {
	host, client, snapshot, _ := startAppsClient(t)
	host.mu.Lock()
	host.clients = append(host.clients, client)
	host.mu.Unlock()
	inst := host.RegisterAppInstance("apps-fixture", "app_rich", snapshot.generation, "call", "ui://app/rich.html")
	if csp, ok := host.AppInstanceResourceDescriptor(inst.Token); !ok || len(csp["connect-src"]) != 1 {
		t.Fatalf("tool resource descriptor = %#v, %v", csp, ok)
	}
	wrong := host.RegisterAppInstance("apps-fixture", "app_rich", snapshot.generation, "call", "ui://app/other.html")
	if _, ok := host.AppInstanceResourceDescriptor(wrong.Token); ok {
		t.Fatal("mismatched resource URI was accepted")
	}
	content, mime, csp, err := client.readResourceWithMime(t.Context(), "ui://app/rich.html")
	if err != nil {
		t.Fatal(err)
	}
	if content != "<html>rich</html>" || mime != "text/html;profile=mcp-app" {
		t.Fatalf("content/mime = %q %q", content, mime)
	}
	if got := csp["connectDomains"]; len(got) != 1 || got[0] != "https://api.resource.example" {
		t.Fatalf("resource CSP = %#v", csp)
	}
}

func TestAppInstanceRegistryFreezesResourceAndBoundsSnapshotMemory(t *testing.T) {
	host := NewHostWithProfile(HostProfileDesktopApps)
	csp := map[string][]string{"connect-src": {"https://api.example.test"}}
	first := host.RegisterAppInstance("srv", "tool", 3, "call-1", "ui://x/a.html")
	if !host.BindAppResource(first.Token, "<html>first</html>", "text/html", "digest-1", csp) {
		t.Fatal("bind first resource failed")
	}
	csp["connect-src"][0] = "https://mutated.example.test"
	snapshot, ok := host.AppResource(first.Token)
	if !ok || snapshot.Digest != "digest-1" || snapshot.Content != "<html>first</html>" {
		t.Fatalf("snapshot = %+v, %v", snapshot, ok)
	}
	if got := snapshot.CSP["connect-src"][0]; got != "https://api.example.test" {
		t.Fatalf("snapshot CSP was not copied: %q", got)
	}

	chunk := strings.Repeat("x", 1<<20)
	for i := range 17 {
		inst := host.RegisterAppInstance("srv", "tool", 3, "call", "ui://x/b.html")
		if !host.BindAppResource(inst.Token, chunk, "text/html", "digest", nil) {
			t.Fatalf("bind resource %d failed", i)
		}
	}
	if host.appInstances.bytes > maxAppResourceRegistryBytes {
		t.Fatalf("snapshot bytes = %d, limit = %d", host.appInstances.bytes, maxAppResourceRegistryBytes)
	}
	if _, ok := host.AppResource(first.Token); ok {
		t.Fatal("oldest snapshot was not evicted by the aggregate memory budget")
	}
	tooLarge := host.RegisterAppInstance("srv", "tool", 3, "call", "ui://x/large.html")
	if host.BindAppResource(tooLarge.Token, strings.Repeat("z", maxAppResourceSnapshotBytes+1), "text/html", "digest", nil) {
		t.Fatal("oversized resource snapshot was accepted")
	}
	metadataBomb := host.RegisterAppInstance("srv", "tool", 3, "call", "ui://x/csp.html")
	if host.BindAppResource(metadataBomb.Token, "<html></html>", "text/html", "digest", map[string][]string{
		"resourceDomains": {strings.Repeat("z", maxAppResourceSnapshotBytes)},
	}) {
		t.Fatal("oversized resource CSP was accepted")
	}
}
