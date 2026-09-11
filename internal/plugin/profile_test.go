package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// captureInitializeServer runs an in-memory server whose only tool records the
// client's initialize capabilities, so a test can assert exactly what the
// host declared on the wire.
type capturedInitialize struct {
	mu   sync.Mutex
	caps json.RawMessage
}

func (c *capturedInitialize) capabilities() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps
}

func newCapabilityCaptureServer(t *testing.T, captured *capturedInitialize) *mcpsdk.Server {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "cap-fixture", Version: "1"}, nil)
	server.AddTool(&mcpsdk.Tool{
		Name:        "report_capabilities",
		Description: "reports the client's declared capabilities",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		ss, ok := req.GetSession().(*mcpsdk.ServerSession)
		if !ok || ss.InitializeParams() == nil {
			return nil, fmt.Errorf("no initialize params on session")
		}
		raw, err := json.Marshal(ss.InitializeParams().Capabilities)
		if err != nil {
			return nil, err
		}
		captured.mu.Lock()
		captured.caps = raw
		captured.mu.Unlock()
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}}}, nil
	})
	return server
}

// connectWithProfile drives one real SDK session through the profile-aware
// build path and calls the capture tool.
func connectWithProfile(t *testing.T, profile HostProfile) json.RawMessage {
	t.Helper()
	captured := &capturedInitialize{}
	lifeCtx, cancelLife := context.WithCancel(context.Background())
	transport := &sdkSessionTransport{
		name: "cap-check", spec: Spec{Name: "cap-check", Type: "http"}, profile: profile,
		lifeCtx: lifeCtx, cancel: cancelLife, state: SessionStateConnecting,
	}
	transport.endpointFactory = func(ctx context.Context) (sdkEndpoint, error) {
		clientSide, serverSide := mcpsdk.NewInMemoryTransports()
		server := newCapabilityCaptureServer(t, captured)
		go func() { _ = server.Run(ctx, serverSide) }()
		return sdkEndpoint{transport: clientSide}, nil
	}
	t.Cleanup(transport.close)

	managed, err := transport.acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := invokeSDKMethod(t.Context(), managed.session, "tools/call", map[string]any{
		"name":      "report_capabilities",
		"arguments": map[string]any{},
	}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	raw := captured.capabilities()
	if raw == nil {
		t.Fatal("capture tool never ran")
	}
	return raw
}

func TestProfileDeclaresExpectedClientCapabilities(t *testing.T) {
	cases := []struct {
		profile             HostProfile
		wantElicitationForm bool
		wantElicitationURL  bool
		wantAppsUI          bool
	}{
		{HostProfileCore, false, false, false},
		{HostProfileInteractive, true, true, false},
		{HostProfileDesktopApps, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.profile.String(), func(t *testing.T) {
			raw := connectWithProfile(t, tc.profile)
			var caps struct {
				Elicitation *struct {
					Form json.RawMessage `json:"form"`
					URL  json.RawMessage `json:"url"`
				} `json:"elicitation"`
				Extensions map[string]json.RawMessage `json:"extensions"`
			}
			if err := json.Unmarshal(raw, &caps); err != nil {
				t.Fatalf("parse capabilities %s: %v", raw, err)
			}
			if tc.wantElicitationForm && (caps.Elicitation == nil || len(caps.Elicitation.Form) == 0) {
				t.Errorf("profile %s: elicitation.form not declared: %s", tc.profile, raw)
			}
			if tc.wantElicitationURL && (caps.Elicitation == nil || len(caps.Elicitation.URL) == 0) {
				t.Errorf("profile %s: elicitation.url not declared: %s", tc.profile, raw)
			}
			if !tc.wantElicitationForm && !tc.wantElicitationURL && caps.Elicitation != nil {
				t.Errorf("profile %s: elicitation declared: %s", tc.profile, raw)
			}
			ui, hasUI := caps.Extensions[AppsUIExtensionID]
			if tc.wantAppsUI != hasUI {
				t.Errorf("profile %s: extension %s presence = %v, want %v (%s)", tc.profile, AppsUIExtensionID, hasUI, tc.wantAppsUI, raw)
			}
			if tc.wantAppsUI {
				var settings struct {
					MimeTypes []string `json:"mimeTypes"`
				}
				if err := json.Unmarshal(ui, &settings); err != nil {
					t.Fatalf("parse ui extension settings %s: %v", ui, err)
				}
				if len(settings.MimeTypes) != 1 || settings.MimeTypes[0] != AppsMimeType {
					t.Errorf("ui extension mimeTypes = %v, want [%s]", settings.MimeTypes, AppsMimeType)
				}
			}
		})
	}
}

func TestCoreProfileMatchesLegacyCapabilityBytes(t *testing.T) {
	raw := connectWithProfile(t, HostProfileCore)
	var caps map[string]json.RawMessage
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	if _, ok := caps["elicitation"]; ok {
		t.Errorf("core profile declared elicitation: %s", raw)
	}
	if _, ok := caps["extensions"]; ok {
		t.Errorf("core profile declared extensions: %s", raw)
	}
}

func TestNoBrokerElicitationCancels(t *testing.T) {
	lifeCtx, cancelLife := context.WithCancel(context.Background())
	transport := &sdkSessionTransport{
		name: "no-broker", spec: Spec{Name: "no-broker", Type: "http"}, profile: HostProfileInteractive,
		lifeCtx: lifeCtx, cancel: cancelLife, state: SessionStateConnecting,
	}
	t.Cleanup(transport.close)
	res, err := transport.handleElicitation(t.Context(), &mcpsdk.ElicitRequest{Params: &mcpsdk.ElicitParams{
		Mode: "form", Message: "need input",
	}})
	if err != nil {
		t.Fatalf("handleElicitation: %v", err)
	}
	if res.Action != "cancel" {
		t.Fatalf("no-broker action = %q, want cancel", res.Action)
	}
}
