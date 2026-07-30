package capdiag

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/plugin"
)

func lookPath(cmd string) (string, error) {
	return exec.LookPath(cmd)
}

// probeLiveMCP starts automatic-intent servers in an isolated Host, records
// connection results, and always closes the Host (including stdio children).
// Persistence (startup stats / schema cache) is disabled so --live stays
// free of cache/state side effects under Reasonix home.
func probeLiveMCP(rep *MCPReport, cfg *config.Config, root, home string, timeout time.Duration) []Issue {
	var issues []Issue
	if cfg == nil {
		return issues
	}
	if timeout <= 0 {
		timeout = DefaultLiveTimeout
	}
	if timeout < MinLiveTimeout {
		timeout = MinLiveTimeout
	}
	if timeout > MaxLiveTimeout {
		timeout = MaxLiveTimeout
	}

	// Only probe automatic start intent servers.
	var auto []config.PluginEntry
	for _, p := range cfg.Plugins {
		if p.ShouldAutoStart() {
			auto = append(auto, p)
		} else {
			for i := range rep.Servers {
				if rep.Servers[i].Name == p.Name {
					rep.Servers[i].RuntimeStatus = "skipped"
				}
			}
		}
	}
	if len(auto) == 0 {
		return issues
	}

	specs := boot.PluginSpecsForRoot(auto, root)
	ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Duration(len(specs))+timeout)
	defer cancel()

	host, _, _ := plugin.Start(ctx, specs, plugin.StartPolicy{
		PerPluginTimeout: timeout,
		Concurrency:      4,
		AbortOnError:     false,
		SkipPersistence:  true,
	})
	if host == nil {
		return issues
	}
	defer host.Close()

	byName := map[string]int{}
	for i, s := range rep.Servers {
		byName[s.Name] = i
	}

	connected := map[string]bool{}
	for _, s := range host.Servers() {
		connected[s.Name] = true
		tools := make([]MCPToolInfo, 0, len(s.ToolList))
		for _, t := range s.ToolList {
			tools = append(tools, MCPToolInfo{Name: t.Name, ReadOnlyHint: t.ReadOnlyHint, DestructiveHint: t.DestructiveHint})
		}
		if i, ok := byName[s.Name]; ok {
			rep.Servers[i].RuntimeStatus = "probed"
			rep.Servers[i].ToolCount = s.Tools
			rep.Servers[i].Tools = tools
		} else {
			rep.Servers = append(rep.Servers, MCPServerInfo{
				Name: s.Name, Transport: s.Transport, RuntimeStatus: "probed",
				ToolCount: s.Tools, Tools: tools, StartIntent: "automatic",
			})
		}
		if s.HasTools && s.Tools == 0 {
			issues = append(issues, Issue{
				Severity: "warning", Code: "mcp.no_tools", Subsystem: "mcp",
				Name: s.Name, Message: "live probe: MCP server connected but exposes no tools",
				Remediation: "Check server configuration and authentication",
				SettingsTab: "mcp",
			})
		}
	}
	for _, f := range host.Failures() {
		errText := sanitizeErrTextWithPaths(f.Error, root, home)
		if i, ok := byName[f.Name]; ok {
			rep.Servers[i].RuntimeStatus = "failed"
			rep.Servers[i].Error = errText
		}
		issues = append(issues, Issue{
			Severity: "error", Code: "mcp.start_failed", Subsystem: "mcp",
			Name: f.Name, Message: "live probe failed: " + errText,
			Remediation: "Fix command/URL/auth; re-run with --live after changes",
			SettingsTab: "mcp",
		})
	}
	for _, p := range auto {
		if connected[p.Name] {
			continue
		}
		if i, ok := byName[p.Name]; ok {
			if rep.Servers[i].RuntimeStatus == "failed" {
				continue
			}
			if rep.Servers[i].RuntimeStatus == "" {
				rep.Servers[i].RuntimeStatus = "failed"
				rep.Servers[i].Error = "no connection result within timeout"
				issues = append(issues, Issue{
					Severity: "error", Code: "mcp.start_failed", Subsystem: "mcp",
					Name: p.Name, Message: fmt.Sprintf("live probe: no result within %s", timeout),
					Remediation: "Increase --timeout or fix a hanging MCP server",
					SettingsTab: "mcp",
				})
			}
		}
	}
	return issues
}

// HasErrorSeverity reports whether the report contains any error-level issue.
func HasErrorSeverity(r Report) bool {
	for _, is := range r.Issues {
		if is.Severity == "error" {
			return true
		}
	}
	return false
}

// LiveWarningMessage is printed to stderr before CLI --live starts MCP.
func LiveWarningMessage() string {
	return strings.TrimSpace(`warning: --live will start third-party MCP servers in an isolated process.
They may access the network and receive configured environment variables and headers.
Tools are not registered into the agent registry. Startup stats/schema cache writes are disabled.
Host is always closed after the probe.`)
}
