//go:build windows

package serve

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
)

const windowsFolderPicker = `[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
Add-Type -AssemblyName System.Windows.Forms
$dialog = [System.Windows.Forms.FolderBrowserDialog]::new()
$dialog.Description = '选择 Reasonix Studio 工作区'
$dialog.ShowNewFolderButton = $true
if ($args.Count -gt 0 -and (Test-Path -LiteralPath $args[0] -PathType Container)) { $dialog.SelectedPath = $args[0] }
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($dialog.SelectedPath) }
$dialog.Dispose()`

func pickLocalFolder(ctx context.Context, startIn string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-WindowStyle", "Hidden", "-Command", windowsFolderPicker, startIn)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func pickLocalFolderAvailable() bool { return true }
