package plugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/secrets"
	"reasonix/internal/tool"
)

// TestChromeDevtoolsMCPLive is an opt-in release smoke test for the real npm
// package and local Chrome. It stays skipped in normal CI because it requires
// network access, npx, and a graphical Chrome installation.
//
// Run with:
//
//	REASONIX_LIVE_CHROME_MCP=1 go test ./internal/plugin \
//	  -run '^TestChromeDevtoolsMCPLive$' -v -count=1 -timeout=3m
func TestChromeDevtoolsMCPLive(t *testing.T) {
	if os.Getenv("REASONIX_LIVE_CHROME_MCP") != "1" {
		t.Skip("set REASONIX_LIVE_CHROME_MCP=1 to run the real Chrome MCP smoke test")
	}
	// The package TestMain redirects HOME to keep normal tests isolated. A login
	// shell under that empty home cannot load the user's Node manager and would
	// incorrectly prepend /usr/local/bin over the invoking shell's PATH. The live
	// test deliberately uses the caller's already-resolved PATH, matching a real
	// desktop process after its user-home shell probe.
	oldShellPATH := stdioShellPATH
	stdioShellPATH = func(context.Context) string { return os.Getenv("PATH") }
	t.Cleanup(func() { stdioShellPATH = oldShellPATH })

	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	defer lifeCancel()
	callCtx, callCancel := context.WithTimeout(lifeCtx, 2*time.Minute)
	defer callCancel()

	workspaceRoot := t.TempDir()
	spec := Spec{
		Name:          "chrome-devtools-live",
		Command:       "npx",
		Args:          []string{"-y", "chrome-devtools-mcp@latest", "--isolated=true"},
		Authorized:    true,
		ProcessMode:   MCPProcessHost,
		StateDir:      t.TempDir(),
		WorkspaceRoot: workspaceRoot,
	}
	host := NewHost()
	defer host.Close()
	resolvedNPX, resolvedEnv, resolveErr := resolveStdioExecutable(callCtx, spec, mergeEnv(secrets.ProcessEnv(), spec.Env))
	if resolveErr != nil {
		t.Fatalf("resolve npx: %v", resolveErr)
	}
	resolvedNode, _ := lookPathInEnv("node", resolvedEnv)
	version := exec.Command(resolvedNode, "--version")
	version.Env = resolvedEnv
	versionOut, versionErr := version.CombinedOutput()
	if versionErr != nil {
		t.Fatalf("resolved node %q: %v: %s", resolvedNode, versionErr, strings.TrimSpace(string(versionOut)))
	}
	t.Logf("step 01/12 runtime: npx=%s node=%s version=%s", resolvedNPX, resolvedNode, strings.TrimSpace(string(versionOut)))
	if spec.ResolvedProcessMode() != MCPProcessHost {
		t.Fatalf("step 02/12 process mode = %q, want host", spec.ResolvedProcessMode())
	}
	t.Log("step 02/12 trusted host process mode selected")

	result, err := host.InstallAndConnect(callCtx, spec)
	if err != nil {
		t.Fatalf("InstallAndConnect: state=%s action=%s err=%v", result.State, result.Action, err)
	}
	if result.State != "ready" || result.ToolCount == 0 {
		t.Fatalf("install result = %+v, want ready with tools", result)
	}
	t.Logf("step 03/12 initialize + tools/list ready: tools=%d", result.ToolCount)

	tools, err := host.ToolsFor(callCtx, spec.Name)
	if err != nil {
		t.Fatalf("ToolsFor: %v", err)
	}
	requiredTools := []string{"list_pages", "new_page", "navigate_page", "wait_for", "take_snapshot", "evaluate_script", "list_console_messages", "list_network_requests", "take_screenshot"}
	for _, rawName := range requiredTools {
		if findLiveMCPTool(tools, rawName) == nil {
			t.Fatalf("step 04/12 required tool %q missing from %v", rawName, toolNames(tools))
		}
	}
	t.Logf("step 04/12 catalog contains all %d required browser tools", len(requiredTools))
	listPages := findLiveMCPTool(tools, "list_pages")
	if listPages == nil {
		t.Fatalf("list_pages missing from %v", toolNames(tools))
	}
	out, err := listPages.Execute(callCtx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("list_pages: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("list_pages returned empty output")
	}
	t.Logf("step 05/12 list_pages=%s", strings.TrimSpace(out))

	newPage := findLiveMCPTool(tools, "new_page")
	if newPage == nil {
		t.Fatalf("new_page missing from %v", toolNames(tools))
	}
	out, err = newPage.Execute(callCtx, json.RawMessage(`{"url":"about:blank"}`))
	if err != nil {
		t.Fatalf("new_page: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("new_page returned empty output")
	}
	t.Logf("step 06/12 new_page=%s", strings.TrimSpace(out))

	pageHTML := `data:text/html,<title>Reasonix%20MCP</title><h1>Reasonix%20MCP%20Ready</h1>`
	out = executeLiveChromeTool(t, callCtx, tools, "navigate_page", map[string]any{"type": "url", "url": pageHTML})
	t.Logf("step 07/12 navigate_page=%s", strings.TrimSpace(out))
	out = executeLiveChromeTool(t, callCtx, tools, "wait_for", map[string]any{"text": []string{"Reasonix MCP Ready"}, "timeout": 10_000})
	if !strings.Contains(out, "Reasonix MCP Ready") {
		t.Fatalf("step 08/12 wait_for output = %q", out)
	}
	t.Log("step 08/12 page content became observable")
	out = executeLiveChromeTool(t, callCtx, tools, "take_snapshot", map[string]any{})
	if !strings.Contains(out, "Reasonix MCP Ready") {
		t.Fatalf("step 09/12 snapshot output = %q", out)
	}
	t.Log("step 09/12 accessibility snapshot captured")
	out = executeLiveChromeTool(t, callCtx, tools, "evaluate_script", map[string]any{
		"function": `() => { console.log("reasonix-mcp-console"); return document.title; }`,
	})
	if !strings.Contains(out, "Reasonix MCP") {
		t.Fatalf("step 10/12 evaluate_script output = %q", out)
	}
	t.Log("step 10/12 evaluate_script returned the document title")
	consoleOut := executeLiveChromeTool(t, callCtx, tools, "list_console_messages", map[string]any{})
	networkOut := executeLiveChromeTool(t, callCtx, tools, "list_network_requests", map[string]any{})
	if !strings.Contains(consoleOut, "reasonix-mcp-console") || strings.TrimSpace(networkOut) == "" {
		t.Fatalf("step 11/12 diagnostics console=%q network=%q", consoleOut, networkOut)
	}
	t.Log("step 11/12 console and network diagnostics are readable")
	screenshotPath := filepath.Join(workspaceRoot, "chrome-devtools-live.png")
	_ = executeLiveChromeTool(t, callCtx, tools, "take_screenshot", map[string]any{"filePath": screenshotPath, "format": "png"})
	info, statErr := os.Stat(screenshotPath)
	if statErr != nil || info.Size() == 0 {
		t.Fatalf("step 12/12 screenshot file: info=%v err=%v", info, statErr)
	}
	if host.client(spec.Name) == nil {
		t.Fatal("step 12/12 shared Chrome MCP client disappeared before host close")
	}
	t.Logf("step 12/12 screenshot persisted (%d bytes); shared client healthy before graceful close", info.Size())
}

func executeLiveChromeTool(t *testing.T, ctx context.Context, tools []tool.Tool, rawName string, args any) string {
	t.Helper()
	candidate := findLiveMCPTool(tools, rawName)
	if candidate == nil {
		t.Fatalf("%s missing from %v", rawName, toolNames(tools))
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal %s args: %v", rawName, err)
	}
	out, err := candidate.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("%s: %v", rawName, err)
	}
	return out
}

func findLiveMCPTool(tools []tool.Tool, rawName string) tool.Tool {
	for _, candidate := range tools {
		meta, ok := candidate.(tool.MCPMetadata)
		if ok && meta.MCPRawToolName() == rawName {
			return candidate
		}
	}
	return nil
}
