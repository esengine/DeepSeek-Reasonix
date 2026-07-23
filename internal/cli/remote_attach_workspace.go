package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"reasonix/internal/remote/workbench/attach"
	"reasonix/internal/remote/workbench/runtime"
)

// remoteAttachWorkspaceCLI runs `reasonix remote attach-workspace --stdio`.
// An explicit --workspace / REASONIX_ATTACH_WORKSPACE binds the target; when
// absent, attach uses the authenticated remote/initialize workspace DTO.
func remoteAttachWorkspaceCLI(args []string, version string) int {
	workspace := ""
	stdio := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--stdio":
			stdio = true
		case args[i] == "--workspace" && i+1 < len(args):
			workspace = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--workspace="):
			workspace = strings.TrimPrefix(args[i], "--workspace=")
		}
	}
	if !stdio {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote attach-workspace --stdio [--workspace <path>]")
		return 2
	}
	if workspace == "" {
		workspace = strings.TrimSpace(os.Getenv("REASONIX_ATTACH_WORKSPACE"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtimeBinary, binErr := os.Executable()
	if binErr != nil {
		fmt.Fprintln(os.Stderr, "attach-workspace: resolve runtime binary:", binErr)
		return 1
	}
	// The per-workspace runtime is detached from this SSH attach process so
	// controllers survive an unexpected transport cut for the grace window.
	err := attach.Run(ctx, os.Stdin, os.Stdout, attach.Options{
		Workspace:     workspace,
		Version:       version,
		RuntimeBinary: runtimeBinary,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "attach-workspace:", err)
		return 1
	}
	return 0
}

// remoteRuntimeWorkbenchCLI runs a long-lived per-workspace runtime on a Unix socket.
func remoteRuntimeWorkbenchCLI(args []string, version string) int {
	workspace, socket := "", ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--workspace" && i+1 < len(args):
			workspace = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--workspace="):
			workspace = strings.TrimPrefix(args[i], "--workspace=")
		case args[i] == "--socket" && i+1 < len(args):
			socket = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--socket="):
			socket = strings.TrimPrefix(args[i], "--socket=")
		case args[i] == "--version" && i+1 < len(args):
			version = args[i+1]
			i++
		}
	}
	if workspace == "" || socket == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote runtime-workbench --workspace <path> --socket <path>")
		return 2
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := runtime.New(runtime.Options{Workspace: workspace, Version: version})
	defer srv.Close()
	if err := srv.ListenAndServe(ctx, socket); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "runtime-workbench:", err)
		return 1
	}
	return 0
}
