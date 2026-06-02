package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// autoStartPath returns the executable path to register for auto-start.
func autoStartPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}
	return filepath.Clean(exe), nil
}

// SetAutoStart enables or disables auto-start on user login.
func SetAutoStart(enabled bool) error {
	exe, err := autoStartPath()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return autoStartWindows(exe, enabled)
	case "darwin":
		return autoStartDarwin(exe, enabled)
	case "linux":
		return autoStartLinux(exe, enabled)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// IsAutoStart reports whether auto-start is currently enabled.
func IsAutoStart() bool {
	switch runtime.GOOS {
	case "windows":
		return isAutoStartWindows()
	case "darwin":
		return isAutoStartDarwin()
	case "linux":
		return isAutoStartLinux()
	default:
		return false
	}
}

// --- Windows: HKCU\Software\Microsoft\Windows\CurrentVersion\Run ---
const (
	autoStartRegPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartRegName = "ReasonixDesktop"
)

func autoStartWindows(exe string, enabled bool) error {
	if enabled {
		// Use reg.exe (built-in) to add the Run entry for current user.
		cmd := exec.Command("reg", "add",
			"HKCU\\"+autoStartRegPath,
			"/v", autoStartRegName,
			"/t", "REG_SZ",
			"/d", exe,
			"/f")
		return cmd.Run()
	}
	cmd := exec.Command("reg", "delete",
		"HKCU\\"+autoStartRegPath,
		"/v", autoStartRegName,
		"/f")
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "ERROR") {
		// Key or value didn't exist — that's fine, we're disabling.
		return nil
	}
	return nil
}

func isAutoStartWindows() bool {
	cmd := exec.Command("reg", "query",
		"HKCU\\"+autoStartRegPath,
		"/v", autoStartRegName)
	return cmd.Run() == nil
}

// --- macOS: ~/Library/LaunchAgents/com.reasonix.desktop.plist ---
func autoStartPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.reasonix.desktop.plist")
}

func autoStartDarwin(exe string, enabled bool) error {
	p := autoStartPlistPath()
	if !enabled {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.reasonix.desktop</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
</dict>
</plist>
`, exe)
	return os.WriteFile(p, []byte(plist), 0o644)
}

func isAutoStartDarwin() bool {
	_, err := os.Stat(autoStartPlistPath())
	return err == nil
}

// --- Linux: ~/.config/autostart/reasonix-desktop.desktop ---
func autoStartDesktopPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "autostart", "reasonix-desktop.desktop")
}

func autoStartLinux(exe string, enabled bool) error {
	p := autoStartDesktopPath()
	if !enabled {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	desktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Reasonix Desktop
Exec=%s
Terminal=false
X-GNOME-Autostart-enabled=true
`, exe)
	return os.WriteFile(p, []byte(desktop), 0o644)
}

func isAutoStartLinux() bool {
	_, err := os.Stat(autoStartDesktopPath())
	return err == nil
}
