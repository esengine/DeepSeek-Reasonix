package cli

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/daemon"
)

const (
	daemonLaunchdLabel = "com.reasonix.daemon"
	daemonSystemdUnit  = "reasonix-daemon.service"
	daemonWindowsTask  = "ReasonixDaemon"
)

type daemonStartupOptions struct {
	exe     string
	addr    string
	dir     string
	logFile string
}

type daemonStartupSpec struct {
	Platform          string
	TargetPath        string
	Content           string
	InstallCommands   [][]string
	UninstallCommands [][]string
}

func daemonStartupCmd(args []string) int {
	if len(args) < 1 {
		daemonStartupUsage()
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "install", "uninstall", "print":
		return daemonStartupAction(sub, rest)
	case "help", "--help", "-h":
		daemonStartupUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown daemon startup subcommand %q\n\n", sub)
		daemonStartupUsage()
		return 2
	}
}

func daemonStartupAction(action string, args []string) int {
	fs := flag.NewFlagSet("daemon startup "+action, flag.ContinueOnError)
	exe := fs.String("exe", "", "reasonix CLI 路径（默认当前 reasonix 可执行文件）")
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 监听地址")
	dir := fs.String("dir", "", "会话目录（默认用户配置）")
	logFile := fs.String("log-file", "", "daemon 日志文件（默认 <session-dir>/.daemon.log，none 表示关闭文件日志）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	opts, err := resolveDaemonStartupOptions(*exe, *addr, *dir, *logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	spec, err := buildDaemonStartupSpec(runtime.GOOS, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	switch action {
	case "print":
		printDaemonStartupSpec(spec)
	case "install":
		if err := installDaemonStartupSpec(spec); err != nil {
			fmt.Fprintf(os.Stderr, "error: install startup helper: %v\n", err)
			return 1
		}
		fmt.Printf("daemon startup helper installed for %s\n", spec.Platform)
	case "uninstall":
		if err := uninstallDaemonStartupSpec(spec); err != nil {
			fmt.Fprintf(os.Stderr, "error: uninstall startup helper: %v\n", err)
			return 1
		}
		fmt.Printf("daemon startup helper uninstalled for %s\n", spec.Platform)
	}
	return 0
}

func resolveDaemonStartupOptions(exe, addr, dir, logFile string) (daemonStartupOptions, error) {
	exe = strings.TrimSpace(exe)
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return daemonStartupOptions{}, err
		}
	}
	absExe, err := filepath.Abs(exe)
	if err != nil {
		return daemonStartupOptions{}, err
	}
	if info, err := os.Stat(absExe); err != nil {
		return daemonStartupOptions{}, fmt.Errorf("reasonix executable not found: %w", err)
	} else if info.IsDir() {
		return daemonStartupOptions{}, fmt.Errorf("reasonix executable is a directory: %s", absExe)
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = config.SessionDir()
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return daemonStartupOptions{}, err
	}
	resolvedLog := resolveDaemonLogFile(absDir, logFile)
	if resolvedLog != "" {
		if absLog, err := filepath.Abs(resolvedLog); err == nil {
			resolvedLog = absLog
		}
	}
	return daemonStartupOptions{
		exe:     absExe,
		addr:    strings.TrimSpace(addr),
		dir:     absDir,
		logFile: resolvedLog,
	}, nil
}

func buildDaemonStartupSpec(goos string, opts daemonStartupOptions) (daemonStartupSpec, error) {
	args := daemonStartupProgramArgs(opts)
	switch goos {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return daemonStartupSpec{}, err
		}
		path := filepath.Join(home, "Library", "LaunchAgents", daemonLaunchdLabel+".plist")
		domain := "gui/" + strconv.Itoa(os.Getuid())
		return daemonStartupSpec{
			Platform:   "launchd",
			TargetPath: path,
			Content:    launchdPlist(args),
			InstallCommands: [][]string{
				{"launchctl", "bootout", domain, path},
				{"launchctl", "bootstrap", domain, path},
				{"launchctl", "enable", domain + "/" + daemonLaunchdLabel},
				{"launchctl", "kickstart", "-k", domain + "/" + daemonLaunchdLabel},
			},
			UninstallCommands: [][]string{
				{"launchctl", "bootout", domain, path},
			},
		}, nil
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return daemonStartupSpec{}, err
		}
		path := filepath.Join(home, ".config", "systemd", "user", daemonSystemdUnit)
		return daemonStartupSpec{
			Platform:   "systemd --user",
			TargetPath: path,
			Content:    systemdService(args),
			InstallCommands: [][]string{
				{"systemctl", "--user", "daemon-reload"},
				{"systemctl", "--user", "enable", "--now", daemonSystemdUnit},
			},
			UninstallCommands: [][]string{
				{"systemctl", "--user", "disable", "--now", daemonSystemdUnit},
				{"systemctl", "--user", "daemon-reload"},
			},
		}, nil
	case "windows":
		commandLine := windowsCommandLine(args)
		return daemonStartupSpec{
			Platform:   "Windows Scheduled Task",
			TargetPath: daemonWindowsTask,
			InstallCommands: [][]string{
				{"schtasks", "/Create", "/TN", daemonWindowsTask, "/SC", "ONLOGON", "/TR", commandLine, "/F"},
			},
			UninstallCommands: [][]string{
				{"schtasks", "/Delete", "/TN", daemonWindowsTask, "/F"},
			},
		}, nil
	default:
		return daemonStartupSpec{}, fmt.Errorf("startup helper is not supported on %s", goos)
	}
}

func daemonStartupProgramArgs(opts daemonStartupOptions) []string {
	args := []string{opts.exe, "daemon", "start", "--addr", opts.addr, "--dir", opts.dir}
	if opts.logFile != "" {
		args = append(args, "--log-file", opts.logFile)
	} else {
		args = append(args, "--log-file", "none")
	}
	return args
}

func launchdPlist(args []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("  <key>Label</key>\n  <string>" + daemonLaunchdLabel + "</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, arg := range args {
		b.WriteString("    <string>" + html.EscapeString(arg) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>KeepAlive</key>\n  <false/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func systemdService(args []string) string {
	return fmt.Sprintf(`[Unit]
Description=Reasonix daemon
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=default.target
`, systemdCommandLine(args))
}

func systemdCommandLine(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, startupShellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func windowsCommandLine(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, windowsQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func printDaemonStartupSpec(spec daemonStartupSpec) {
	fmt.Printf("platform: %s\n", spec.Platform)
	fmt.Printf("target: %s\n", spec.TargetPath)
	if spec.Content != "" {
		fmt.Println("\n--- file ---")
		fmt.Print(spec.Content)
	}
	fmt.Println("\n--- install commands ---")
	for _, cmd := range spec.InstallCommands {
		fmt.Println(systemdCommandLine(cmd))
	}
	fmt.Println("\n--- uninstall commands ---")
	for _, cmd := range spec.UninstallCommands {
		fmt.Println(systemdCommandLine(cmd))
	}
}

func installDaemonStartupSpec(spec daemonStartupSpec) error {
	if spec.Content != "" {
		if err := os.MkdirAll(filepath.Dir(spec.TargetPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(spec.TargetPath, []byte(spec.Content), 0o644); err != nil {
			return err
		}
	}
	for i, cmd := range spec.InstallCommands {
		if i == 0 && len(cmd) > 0 && cmd[0] == "launchctl" && len(cmd) > 1 && cmd[1] == "bootout" {
			_ = runDaemonStartupCommand(cmd)
			continue
		}
		if err := runDaemonStartupCommand(cmd); err != nil {
			return err
		}
	}
	return nil
}

func uninstallDaemonStartupSpec(spec daemonStartupSpec) error {
	for _, cmd := range spec.UninstallCommands {
		if err := runDaemonStartupCommand(cmd); err != nil && !strings.Contains(err.Error(), "No such") {
			return err
		}
	}
	if spec.Content != "" {
		if err := os.Remove(spec.TargetPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func runDaemonStartupCommand(argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s: %s", systemdCommandLine(argv), msg)
	}
	return nil
}

func startupShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '=' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func windowsQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func daemonStartupUsage() {
	fmt.Print(`reasonix daemon startup — 安装用户级 daemon 自启动 helper

Usage:
  reasonix daemon startup install   [--exe PATH] [--addr HOST:PORT] [--dir PATH] [--log-file PATH|none]
  reasonix daemon startup uninstall [--exe PATH] [--addr HOST:PORT] [--dir PATH] [--log-file PATH|none]
  reasonix daemon startup print     [--exe PATH] [--addr HOST:PORT] [--dir PATH] [--log-file PATH|none]

Platforms:
  macOS     writes ~/Library/LaunchAgents/com.reasonix.daemon.plist and loads it with launchctl
  Linux     writes ~/.config/systemd/user/reasonix-daemon.service and enables it with systemctl --user
  Windows   creates a current-user ONLOGON scheduled task named ReasonixDaemon
`)
}
