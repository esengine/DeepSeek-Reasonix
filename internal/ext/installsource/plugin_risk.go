package installsource

import "reasonix/internal/ext/pluginpkg"

// assessPluginRisk raises a plugin step's risk for each thing the package can
// do on this machine, and says why.
func assessPluginRisk(a *action, pkg pluginpkg.Package, hooks, mcp int) {
	if a.Mode == "link" {
		a.RiskReasons = append(a.RiskReasons, "links a plugin package from a mutable local directory")
	}
	if hooks > 0 {
		a.RiskLevel = RiskHigh
		a.RiskReasons = append(a.RiskReasons, "registers shell hooks that execute during Reasonix sessions")
	}
	if mcp > 0 {
		a.RiskLevel = RiskHigh
		a.RiskReasons = append(a.RiskReasons, "adds MCP servers that can change provider-visible tool schemas")
	}
	if a.Runtime != nil {
		a.RiskLevel = RiskHigh
		a.RiskReasons = append(a.RiskReasons, "FULL TRUST: declares a runtime process that runs inside Reasonix — it can read the full session and environment, bypass permissions, and operate this machine directly. Command: "+hostLiteral(pluginpkg.RuntimeCommandLine(pkg.Manifest.Runtime)))
	}
	if len(pkg.Manifest.Mods) > 0 {
		a.RiskLevel = RiskHigh
		a.RiskReasons = append(a.RiskReasons, "FULL TRUST: a Claude Code mod — its hooks module runs on this machine as you, with no sandbox, when \"Run Claude Code mods\" is on; the listing below is read from the module's syntax tree and is not a guarantee of what it does")
		a.Mod = modPreview(pkg)
	}
}
