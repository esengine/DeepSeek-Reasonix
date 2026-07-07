//go:build windows

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const parentExitTimeout = 2 * time.Minute

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var parentPID uint
	var installer, installDir, relaunch string
	fs := flag.NewFlagSet("reasonix-update-helper", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.UintVar(&parentPID, "parent-pid", 0, "Reasonix process id to wait for before installing")
	fs.StringVar(&installer, "installer", "", "verified NSIS installer path")
	fs.StringVar(&installDir, "install-dir", "", "Reasonix installation directory")
	fs.StringVar(&relaunch, "relaunch", "", "Reasonix executable to start after the installer succeeds")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	logger := newLogger()
	if installer == "" {
		logger.Print("missing --installer")
		return 2
	}
	if parentPID != 0 {
		if err := waitForProcessExit(uint32(parentPID), parentExitTimeout); err != nil {
			logger.Printf("wait for parent process %d: %v", parentPID, err)
			return 1
		}
	}
	if err := runInstaller(installer, installDir); err != nil {
		logger.Printf("run installer: %v", err)
		return 1
	}
	if relaunch != "" {
		if err := startRelaunch(relaunch, installDir); err != nil {
			logger.Printf("relaunch: %v", err)
			return 1
		}
	}
	return 0
}

func newLogger() *log.Logger {
	dir, err := os.UserCacheDir()
	if err == nil {
		dir = filepath.Join(dir, "Reasonix", "updates")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if f, err := os.OpenFile(filepath.Join(dir, "update-helper.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				return log.New(f, "", log.LstdFlags)
			}
		}
	}
	return log.New(os.Stderr, "", log.LstdFlags)
}

func waitForProcessExit(pid uint32, timeout time.Duration) error {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(h)
	waitMS := uint32(timeout / time.Millisecond)
	result, err := windows.WaitForSingleObject(h, waitMS)
	if err != nil {
		return err
	}
	switch result {
	case windows.WAIT_OBJECT_0:
		return nil
	case uint32(windows.WAIT_TIMEOUT):
		return fmt.Errorf("timed out after %s", timeout)
	default:
		return fmt.Errorf("unexpected wait result %d", result)
	}
}

func runInstaller(installer, installDir string) error {
	cmd := exec.Command(installer)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: installerCommandLine(installer, installDir), HideWindow: true}
	return cmd.Run()
}

func startRelaunch(relaunch, installDir string) error {
	cmd := exec.Command(relaunch)
	cmd.Dir = installDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
