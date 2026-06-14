package cli

import (
	"strings"
	"testing"
)

func TestBuildDaemonStartupSpecDarwin(t *testing.T) {
	spec, err := buildDaemonStartupSpec("darwin", daemonStartupOptions{
		exe:     "/Applications/Reasonix CLI/reasonix",
		addr:    "127.0.0.1:19840",
		dir:     "/var/lib/reasonix sessions",
		logFile: "/var/lib/reasonix sessions/.daemon.log",
	})
	if err != nil {
		t.Fatalf("buildDaemonStartupSpec: %v", err)
	}
	if spec.Platform != "launchd" || !strings.HasSuffix(spec.TargetPath, "Library/LaunchAgents/com.reasonix.daemon.plist") {
		t.Fatalf("unexpected launchd spec: %+v", spec)
	}
	for _, want := range []string{
		"<string>com.reasonix.daemon</string>",
		"<string>/Applications/Reasonix CLI/reasonix</string>",
		"<string>--dir</string>",
		"<string>/var/lib/reasonix sessions</string>",
	} {
		if !strings.Contains(spec.Content, want) {
			t.Fatalf("launchd plist missing %q:\n%s", want, spec.Content)
		}
	}
	if len(spec.InstallCommands) == 0 || spec.InstallCommands[0][0] != "launchctl" {
		t.Fatalf("launchd install commands missing: %+v", spec.InstallCommands)
	}
}

func TestBuildDaemonStartupSpecLinux(t *testing.T) {
	spec, err := buildDaemonStartupSpec("linux", daemonStartupOptions{
		exe:     "/opt/reasonix/bin/reasonix",
		addr:    "127.0.0.1:19840",
		dir:     "/var/lib/reasonix sessions",
		logFile: "",
	})
	if err != nil {
		t.Fatalf("buildDaemonStartupSpec: %v", err)
	}
	if spec.Platform != "systemd --user" || !strings.HasSuffix(spec.TargetPath, ".config/systemd/user/reasonix-daemon.service") {
		t.Fatalf("unexpected systemd spec: %+v", spec)
	}
	if !strings.Contains(spec.Content, "ExecStart=/opt/reasonix/bin/reasonix daemon start") {
		t.Fatalf("systemd service missing ExecStart:\n%s", spec.Content)
	}
	if !strings.Contains(spec.Content, "--dir '/var/lib/reasonix sessions'") || !strings.Contains(spec.Content, "--log-file none") {
		t.Fatalf("systemd service missing quoted args:\n%s", spec.Content)
	}
}

func TestBuildDaemonStartupSpecWindows(t *testing.T) {
	spec, err := buildDaemonStartupSpec("windows", daemonStartupOptions{
		exe:     `C:\Program Files\Reasonix\reasonix.exe`,
		addr:    "127.0.0.1:19840",
		dir:     `C:\Reasonix\sessions`,
		logFile: `C:\Reasonix\sessions\.daemon.log`,
	})
	if err != nil {
		t.Fatalf("buildDaemonStartupSpec: %v", err)
	}
	if spec.Platform != "Windows Scheduled Task" || spec.TargetPath != daemonWindowsTask {
		t.Fatalf("unexpected windows spec: %+v", spec)
	}
	if len(spec.InstallCommands) != 1 {
		t.Fatalf("windows install commands = %+v", spec.InstallCommands)
	}
	command := strings.Join(spec.InstallCommands[0], " ")
	if !strings.Contains(command, "schtasks /Create") || !strings.Contains(command, daemonWindowsTask) ||
		!strings.Contains(command, `"C:\Program Files\Reasonix\reasonix.exe"`) {
		t.Fatalf("windows command missing expected pieces: %s", command)
	}
}
