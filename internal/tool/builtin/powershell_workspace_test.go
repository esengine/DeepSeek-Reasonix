package builtin

import (
	"testing"
	"time"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func toolNames(tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return names
}

func containsTool(tools []tool.Tool, name string) bool {
	for _, t := range tools {
		if t.Name() == name {
			return true
		}
	}
	return false
}

func findTool(tools []tool.Tool, name string) tool.Tool {
	for _, t := range tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

func TestWorkspaceToolsExcludesPowerShellByDefault(t *testing.T) {
	// Default workspace, empty enabled list: every built-in except the opt-in
	// powershell tool.
	def := Workspace{}.Tools()
	if !containsTool(def, "bash") {
		t.Fatal("default workspace tools should contain bash")
	}
	if containsTool(def, "powershell") {
		t.Fatal("powershell must not be in the default tool list (prefix byte-stability)")
	}

	// Gate on: the tool appears.
	if gated := (Workspace{PowerShellEnabled: true}).Tools(); !containsTool(gated, "powershell") {
		t.Fatal("PowerShellEnabled=true should include the powershell tool")
	}

	// Explicitly named but gate off: still absent (defense in depth).
	if named := (Workspace{}).Tools("powershell"); containsTool(named, "powershell") {
		t.Fatal("naming powershell without the gate must not enable it")
	}

	// Named and gate on: present, and bound to the workspace (workDir/timeout/
	// shell propagated).
	ws := Workspace{
		Dir:               `C:\ws`,
		BashTimeout:       42 * time.Second,
		PowerShellEnabled: true,
		PowerShell:        sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\ps\pwsh.exe`, MajorVersion: 7},
	}
	named := ws.Tools("powershell")
	tl := findTool(named, "powershell")
	if tl == nil {
		t.Fatal("gated + named powershell should be present")
	}
	p, ok := tl.(powershell)
	if !ok {
		t.Fatalf("workspace powershell tool type = %T, want builtin.powershell", tl)
	}
	if p.workDir != `C:\ws` {
		t.Errorf("workDir = %q, want the workspace dir", p.workDir)
	}
	if p.timeout != 42*time.Second {
		t.Errorf("timeout = %v, want 42s", p.timeout)
	}
	if p.shell.Path != `C:\ps\pwsh.exe` {
		t.Errorf("shell = %+v, want the resolved interpreter", p.shell)
	}
}

func TestConfinePowerShellBindsFields(t *testing.T) {
	spec := sandbox.Spec{Mode: "enforce"}
	sh := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\ps\pwsh.exe`, MajorVersion: 7}
	tl := ConfinePowerShell(spec, sh, SessionDataGuard{}, 60*time.Second)
	p, ok := tl.(powershell)
	if !ok {
		t.Fatalf("ConfinePowerShell type = %T, want builtin.powershell", tl)
	}
	if p.shell.Path != sh.Path || p.timeout != 60*time.Second || !p.sb.Enforce() {
		t.Errorf("ConfinePowerShell bound %+v, want shell/timeout/spec propagated", p)
	}
	// spec.Shell is the bash interpreter and must never leak into the
	// powershell tool.
	specWithBash := spec
	specWithBash.Shell = sandbox.Shell{Kind: sandbox.ShellBash, Path: "/bin/bash"}
	p = ConfinePowerShell(specWithBash, sh, SessionDataGuard{}).(powershell)
	if p.resolved().Kind != sandbox.ShellPowerShell {
		t.Errorf("resolved() = %+v, want the PowerShell interpreter, not spec.Shell", p.resolved())
	}
}
