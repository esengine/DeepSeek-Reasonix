//go:build windows

package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// exitWait bounds how long the swap waits for the application to go. Past it
// the swap proceeds and its own retries meet whatever still holds a file.
const exitWait = 2 * time.Minute

// StartTreeHandoff copies helper out of the install it is about to replace and
// starts it on h. The copy is what lets the swap replace the helper's own file.
// The caller then ends the application; the helper waits for that.
func StartTreeHandoff(h TreeHandoff, helper string) error {
	plan, err := WriteTreeHandoff(h)
	if err != nil {
		return err
	}
	copied := strings.TrimRight(h.StagingDir, `\/`) + ".helper.exe"
	if err := copyExecutable(helper, copied); err != nil {
		return fmt.Errorf("update: copy the swap helper: %w", err)
	}
	cmd := exec.Command(copied, treeHandoffArg, plan)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("update: start the swap helper: %w", err)
	}
	return cmd.Process.Release()
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func waitForExit(pids []int) {
	deadline := time.Now().Add(exitWait)
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			continue // already gone, or never ours to wait on
		}
		left := time.Until(deadline)
		if left > 0 {
			_, _ = windows.WaitForSingleObject(h, uint32(left.Milliseconds()))
		}
		_ = windows.CloseHandle(h)
	}
}

func relaunch(path string) error {
	if path == "" {
		return nil
	}
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// afterTreeInstalled brings the installer's own record of the version along,
// so Apps & features does not go on naming the build the swap replaced. It
// finds that record by where it says it installed, never by product name.
func afterTreeInstalled(h TreeHandoff) {
	const uninstall = `Software\Microsoft\Windows\CurrentVersion\Uninstall`
	root, err := registry.OpenKey(registry.CURRENT_USER, uninstall, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return
	}
	want := filepath.Clean(h.InstallDir)
	for _, name := range names {
		k, err := registry.OpenKey(root, name, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			continue
		}
		loc, _, err := k.GetStringValue("InstallLocation")
		if err == nil && strings.EqualFold(filepath.Clean(loc), want) {
			_ = k.SetStringValue("DisplayVersion", strings.TrimPrefix(h.Version, "v"))
		}
		k.Close()
	}
}

// TreeHandoffSupported reports that this platform swaps staged trees.
func TreeHandoffSupported() bool { return true }
