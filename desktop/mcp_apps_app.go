package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"reasonix/internal/control"
	"reasonix/internal/plugin"
)

// MCPAppInstanceView describes one live App surface for the frontend.
type MCPAppInstanceView struct {
	InstanceToken  string `json:"instanceToken"`
	TabID          string `json:"tabId"`
	Server         string `json:"server"`
	Tool           string `json:"tool"`
	OuterURL       string `json:"outerUrl"`
	ResourceQuery  string `json:"resourceQuery"`
	ResourceDigest string `json:"resourceDigest"`
}

func (a *App) mcpRuntimeForTab(tabID string) (*WorkspaceTab, control.SessionAPI, *plugin.Host, error) {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if tab == nil || ctrl == nil {
		return nil, nil, nil, fmt.Errorf("MCP runtime tab is unavailable")
	}
	hoster, ok := ctrl.(interface{ Host() *plugin.Host })
	if !ok || hoster.Host() == nil {
		return nil, nil, nil, fmt.Errorf("tab does not have an MCP runtime")
	}
	return tab, ctrl, hoster.Host(), nil
}

// MCPOpenAppInstance registers a host App instance for a tool result and
// returns the double-iframe sandbox coordinates. The resource itself loads
// only after the frontend posts the init nonce.
func (a *App) MCPOpenAppInstance(server, tool string, generation uint64, callID, resourceURI string) (*MCPAppInstanceView, error) {
	tab, _ := a.activeTabAndCtrl()
	if tab == nil {
		return nil, fmt.Errorf("no active MCP runtime")
	}
	return a.MCPOpenAppInstanceForTab(tab.ID, server, tool, generation, callID, resourceURI)
}

// MCPOpenAppInstanceForTab binds the App to the tab/controller that produced
// its tool result. The resource is read once, validated against the current
// catalog, fingerprinted, and frozen before any iframe can load it.
func (a *App) MCPOpenAppInstanceForTab(tabID, server, tool string, generation uint64, callID, resourceURI string) (*MCPAppInstanceView, error) {
	tab, ctrl, host, err := a.mcpRuntimeForTab(tabID)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(resourceURI, "ui://") {
		return nil, fmt.Errorf("invalid MCP App resource URI")
	}
	inst := host.RegisterAppInstance(server, tool, generation, callID, resourceURI)
	release := func() { host.ReleaseAppInstance(inst.Token) }
	csp, ok := host.AppInstanceResourceDescriptor(inst.Token)
	if !ok {
		release()
		return nil, fmt.Errorf("MCP App resource no longer matches the current tool catalog")
	}
	readCtx, cancel := context.WithTimeout(a.bootContext(), appResourceReadTimeout)
	defer cancel()
	content, mime, resourceCSP, err := host.ReadResourceForApp(readCtx, server, resourceURI)
	if err != nil {
		release()
		return nil, fmt.Errorf("read MCP App resource: %w", err)
	}
	if len(content) > maxAppResourceBytes || !isAppHTMLMimeType(mime) {
		release()
		return nil, fmt.Errorf("MCP App resource is unavailable or exceeds %d bytes", maxAppResourceBytes)
	}
	if len(resourceCSP) > 0 {
		csp = resourceCSP
	}
	digest := resourceDigest(content)
	if !host.BindAppResource(inst.Token, content, mime, digest, csp) {
		release()
		return nil, fmt.Errorf("MCP App instance expired before its resource was bound")
	}
	outer, err := a.appOriginURL(server)
	if err != nil {
		release()
		return nil, err
	}
	a.mcpAppsSandbox.bind(inst.Token, mcpAppBinding{tabID: tab.ID, server: server, host: host, ctrl: ctrl})
	return &MCPAppInstanceView{
		InstanceToken:  inst.Token,
		TabID:          tab.ID,
		Server:         server,
		Tool:           tool,
		OuterURL:       outer,
		ResourceQuery:  "/resource?token=" + inst.Token + "&digest=" + digest,
		ResourceDigest: digest,
	}, nil
}

// MCPAppResourceDigest returns the SHA-256 bound to a live instance's frozen
// resource snapshot.
func (a *App) MCPAppResourceDigest(instanceToken string) (string, error) {
	binding, ok := a.mcpAppsSandbox.binding(instanceToken)
	if !ok || binding.host == nil {
		return "", fmt.Errorf("unknown app instance")
	}
	snapshot, ok := binding.host.AppResource(instanceToken)
	if !ok {
		return "", fmt.Errorf("unknown app instance")
	}
	return snapshot.Digest, nil
}

func (a *App) MCPAppResourceDigestForTab(tabID, instanceToken string) (string, error) {
	binding, ok := a.mcpAppsSandbox.binding(instanceToken)
	if !ok || binding.tabID != tabID {
		return "", fmt.Errorf("app instance does not belong to tab")
	}
	return a.MCPAppResourceDigest(instanceToken)
}

// MCPCloseAppInstance reclaims an instance (tab closed, component unmounted).
func (a *App) MCPCloseAppInstance(instanceToken string) {
	if binding, ok := a.mcpAppsSandbox.release(instanceToken); ok && binding.host != nil {
		binding.host.ReleaseAppInstance(instanceToken)
	}
}

func (a *App) MCPCloseAppInstanceForTab(tabID, instanceToken string) error {
	binding, ok := a.mcpAppsSandbox.binding(instanceToken)
	if !ok {
		return nil
	}
	if binding.tabID != tabID {
		return fmt.Errorf("app instance does not belong to tab")
	}
	a.MCPCloseAppInstance(instanceToken)
	return nil
}

func validatedAppLink(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("MCP App links must use a credential-free http(s) URL")
	}
	return u, nil
}

// MCPOpenAppLink opens a ui/open-link target after per-origin confirmation.
// The frontend asks first and shows the confirmation; this only routes to the
// system browser.
func (a *App) MCPOpenAppLink(rawURL string) error {
	u, err := validatedAppLink(rawURL)
	if err != nil {
		return err
	}
	runtime.BrowserOpenURL(a.ctx, u.String())
	return nil
}

// MCPOpenAppLinkForTab independently validates the instance/tab binding and
// URL after the frontend's per-origin confirmation. No WebView-supplied grant
// can authorize file:, javascript:, credentials, or a cross-tab instance.
func (a *App) MCPOpenAppLinkForTab(tabID, instanceToken, rawURL string) error {
	binding, ok := a.mcpAppsSandbox.binding(instanceToken)
	if !ok || binding.tabID != tabID || binding.host == nil {
		return fmt.Errorf("app instance does not belong to tab")
	}
	if _, ok := binding.host.LookupAppInstance(instanceToken); !ok {
		return fmt.Errorf("app instance expired")
	}
	return a.MCPOpenAppLink(rawURL)
}

// MCPAppCallTool routes an App-initiated tools/call through the controller's
// gated channel: same server as the instance, visibility includes "app",
// catalog generation unchanged, and the ordinary permission policy decides.
func (a *App) MCPAppCallTool(instanceToken, toolName string, args json.RawMessage) (string, error) {
	binding, ok := a.mcpAppsSandbox.binding(instanceToken)
	if !ok || binding.ctrl == nil {
		return "", fmt.Errorf("unknown app instance")
	}
	caller, ok := binding.ctrl.(interface {
		MCPAppCallTool(instanceToken, toolName string, args json.RawMessage) (string, error)
	})
	if !ok {
		return "", fmt.Errorf("runtime does not support app tool calls")
	}
	return caller.MCPAppCallTool(instanceToken, toolName, args)
}

func (a *App) MCPAppCallToolForTab(tabID, instanceToken, toolName string, args json.RawMessage) (string, error) {
	binding, ok := a.mcpAppsSandbox.binding(instanceToken)
	if !ok || binding.tabID != tabID {
		return "", fmt.Errorf("app instance does not belong to tab")
	}
	return a.MCPAppCallTool(instanceToken, toolName, args)
}
