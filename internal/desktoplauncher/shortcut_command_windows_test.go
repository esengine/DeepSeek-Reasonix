//go:build windows

package desktoplauncher

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLauncherMaintenanceRepairsWithoutStartingDesktop(t *testing.T) {
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	if err := os.WriteFile(launcher, data, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "versions", "v1.38.6", "app", "Reasonix.exe")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("not executable: maintenance must not start a shell"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "Reasonix.lnk")
	script := filepath.Join(root, "shortcut.ps1")
	const source = `param($Link, $Target, $Action)
$ErrorActionPreference = 'Stop'
$shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut($Link)
if ($Action -eq 'create') {
  $shortcut.TargetPath = $Target
  $shortcut.Arguments = '--session "workspace name"'
  $shortcut.Save()
} else {
  $item = (New-Object -ComObject Shell.Application).NameSpace((Split-Path $Link)).ParseName((Split-Path $Link -Leaf))
  @{ Target = $shortcut.TargetPath; Arguments = $shortcut.Arguments; ID = $item.ExtendedProperty('System.AppUserModel.ID') } | ConvertTo-Json -Compress
}
`
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(binary string, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append(os.Environ(), consoleTestEnv+"="+root)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", filepath.Base(binary), err, output)
		}
		return output
	}
	run("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, link, target, "create")
	run(launcher, "--repair-shortcuts", link)
	output := run("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, link, target, "read")
	var got struct{ Target, Arguments, ID string }
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("read shortcut: %v: %s", err, output)
	}
	targetInfo, targetErr := os.Stat(got.Target)
	launcherInfo, launcherErr := os.Stat(launcher)
	if targetErr != nil || launcherErr != nil || !os.SameFile(targetInfo, launcherInfo) || got.ID != "io.reasonix.desktop" || got.Arguments != `--session "workspace name"` {
		t.Fatalf("maintenance shortcut = %+v", got)
	}
}
