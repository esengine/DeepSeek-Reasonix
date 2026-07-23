package capdiag

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderText formats a human-readable report: Summary → Issues → subsystems.
func RenderText(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "reasonix doctor capabilities\n")
	fmt.Fprintf(&b, "root: %s  live: %v  schema: %d\n\n", r.Root, r.Live, r.SchemaVersion)

	fmt.Fprintf(&b, "Summary\n")
	fmt.Fprintf(&b, "  errors=%d warnings=%d infos=%d\n", r.Summary.Errors, r.Summary.Warnings, r.Summary.Infos)
	fmt.Fprintf(&b, "  instructions=%d skills=%d commands=%d hooks=%d plugins=%d mcp=%d\n\n",
		r.Summary.Instructions, r.Summary.Skills, r.Summary.Commands,
		r.Summary.Hooks, r.Summary.Plugins, r.Summary.MCPServers)

	fmt.Fprintf(&b, "Issues (%d)\n", len(r.Issues))
	if len(r.Issues) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, is := range r.Issues {
		fmt.Fprintf(&b, "  [%s] %s", is.Severity, is.Code)
		if is.Name != "" {
			fmt.Fprintf(&b, " %s", is.Name)
		}
		fmt.Fprintf(&b, ": %s\n", is.Message)
		if is.Source != "" {
			fmt.Fprintf(&b, "    source: %s\n", is.Source)
		}
		if is.Remediation != "" {
			fmt.Fprintf(&b, "    fix: %s\n", is.Remediation)
		}
	}
	b.WriteByte('\n')

	// Instructions
	fmt.Fprintf(&b, "Instructions (%d)\n", len(r.Instructions.Docs))
	for _, d := range r.Instructions.Docs {
		fmt.Fprintf(&b, "  %d. [%s] %s\n", d.Order, d.Scope, d.Path)
	}
	if len(r.Instructions.Docs) == 0 {
		b.WriteString("  (none)\n")
	}
	b.WriteByte('\n')

	writeAsset(&b, "Skills", r.Skills)
	writeAsset(&b, "Commands", r.Commands)

	fmt.Fprintf(&b, "Hooks (trusted_project=%v project_defines=%v entries=%d)\n",
		r.Hooks.TrustedProject, r.Hooks.ProjectDefines, len(r.Hooks.Entries))
	for _, s := range r.Hooks.Sources {
		fmt.Fprintf(&b, "  source [%s] %s status=%s hooks=%d\n", s.Scope, s.Path, s.Status, s.HookCount)
	}
	for _, e := range r.Hooks.Entries {
		fmt.Fprintf(&b, "  - %s scope=%s match=%q blocking=%v\n", e.Event, e.Scope, e.Match, e.Blocking)
	}
	b.WriteByte('\n')

	fmt.Fprintf(&b, "Plugins (%d)\n", len(r.Plugins.Packages))
	for _, p := range r.Plugins.Packages {
		fmt.Fprintf(&b, "  - %s enabled=%v status=%s skills=%d commands=%d hooks=%d mcp=%d\n",
			p.Name, p.Enabled, p.Status, p.Skills, p.Commands, p.Hooks, p.MCPServers)
		fmt.Fprintf(&b, "    root: %s\n", p.Root)
	}
	if len(r.Plugins.Packages) == 0 {
		b.WriteString("  (none)\n")
	}
	b.WriteByte('\n')

	fmt.Fprintf(&b, "MCP (%d)\n", len(r.MCP.Servers))
	for _, s := range r.MCP.Servers {
		fmt.Fprintf(&b, "  - %s transport=%s intent=%s source=%s", s.Name, s.Transport, s.StartIntent, s.Source)
		if s.RuntimeStatus != "" {
			fmt.Fprintf(&b, " runtime=%s", s.RuntimeStatus)
		}
		if s.ToolCount > 0 {
			fmt.Fprintf(&b, " tools=%d", s.ToolCount)
		}
		b.WriteByte('\n')
		if s.Error != "" {
			fmt.Fprintf(&b, "    error: %s\n", s.Error)
		}
	}
	if len(r.MCP.Servers) == 0 {
		b.WriteString("  (none)\n")
	}
	return b.String()
}

func writeAsset(b *strings.Builder, title string, a AssetReport) {
	fmt.Fprintf(b, "%s (winners=%d shadowed=%d", title, a.Winners, a.Shadowed)
	if a.Disabled > 0 {
		fmt.Fprintf(b, " disabled=%d", a.Disabled)
	}
	if a.ParseErrors > 0 {
		fmt.Fprintf(b, " errors=%d", a.ParseErrors)
	}
	b.WriteString(")\n")
	for _, r := range a.Roots {
		fmt.Fprintf(b, "  root %s status=%s", r.Path, r.Status)
		if r.Scope != "" {
			fmt.Fprintf(b, " scope=%s", r.Scope)
		}
		b.WriteByte('\n')
	}
	for _, e := range a.Entries {
		if e.Status != "winner" && e.Status != "error" {
			continue // keep text output compact; full list is in JSON
		}
		fmt.Fprintf(b, "  - %s [%s] %s\n", e.Name, e.Status, e.Path)
	}
	b.WriteByte('\n')
}

// RenderJSON writes indented JSON to a string (for tests / copy).
func RenderJSON(r Report) (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}
