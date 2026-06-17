//go:build windows

package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/sandbox"
)

func TestBashCancelKillsWindowsChildProcessTree(t *testing.T) {
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("powershell not found")
	}
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "child.pid")
	quotedPIDFile := strings.ReplaceAll(pidFile, "'", "''")
	command := fmt.Sprintf(
		"$p = Start-Process -FilePath powershell -ArgumentList '-NoProfile','-NonInteractive','-Command','Start-Sleep -Seconds 120' -PassThru; "+
			"Set-Content -LiteralPath '%s' -Value $p.Id; "+
			"Start-Sleep -Seconds 120",
		quotedPIDFile,
	)
	args, _ := json.Marshal(map[string]any{"command": command})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, runErr := (bash{
			shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershell},
		}).Execute(ctx, args)
		done <- runErr
	}()

	childPID := waitForWindowsPIDFile(t, pidFile)
	cancel()
	select {
	case err = <-done:
	case <-time.After(40 * time.Second):
		killWindowsPID(childPID)
		t.Fatal("cancel did not interrupt bash within 40s")
	}
	if err == nil {
		t.Fatal("expected cancel to return an error")
	}
	for i := 0; i < 50; i++ {
		if !windowsProcessAlive(childPID) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	killWindowsPID(childPID)
	t.Fatalf("child process %d survived bash cancel", childPID)
}

func waitForWindowsPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for child pid file %s", path)
	return 0
}

func windowsProcessAlive(pid int) bool {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", fmt.Sprintf("if (Get-Process -Id %d -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }", pid))
	return cmd.Run() == nil
}

func killWindowsPID(pid int) {
	_ = exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
}
