//go:build windows

package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/winsandbox"
)

type windowsSandboxPayload struct {
	Spec     Spec `json:"spec"`
	Writable bool `json:"writable"`
}

// Command wraps the shell invocation in Reasonix's hidden Windows sandbox
// helper. The helper applies the native Windows sandbox backend and a Job
// Object before starting the requested command.
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	return windowsSandboxCommand(spec, sh.argv(command), true)
}

// CommandArgs is like Command but accepts the command as raw argv instead of a
// shell command string.
func CommandArgs(spec Spec, args []string) ([]string, bool) {
	return windowsSandboxCommand(spec, args, false)
}

func windowsSandboxCommand(spec Spec, args []string, writable bool) ([]string, bool) {
	// Mirror the darwin/linux wrappers: when the backend (or this binary's
	// helper dispatch) is unavailable, return the argv unwrapped so the bash
	// tool takes its explicit fail-closed / escape-approval path instead of
	// spawning a helper that cannot work.
	if !spec.Enforce() || !Available() {
		return args, false
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return args, false
	}
	payload, err := encodeWindowsSandboxPayload(windowsSandboxPayload{Spec: spec, Writable: writable})
	if err != nil {
		return args, false
	}
	out := []string{exe, WindowsHelperCommand, payload, "--"}
	out = append(out, args...)
	return out, true
}

// Available reports whether Reasonix can reach its bundled Windows sandbox
// helper and the native Windows sandbox backend is available. The helper is
// this same executable relaunched with WindowsHelperCommand, so the entry
// point must have registered its dispatch route (RegisterHelperDispatch);
// without it the relaunch would not reach RunWindowsSandboxHelper at all.
func Available() bool {
	if !helperDispatchRegistered.Load() {
		return false
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return false
	}
	return winsandbox.Available()
}

func encodeWindowsSandboxPayload(payload windowsSandboxPayload) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeWindowsSandboxPayload(s string) (windowsSandboxPayload, error) {
	var payload windowsSandboxPayload
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return payload, err
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return payload, err
	}
	return payload, nil
}

// RunWindowsSandboxHelper is the hidden process entry point used by Command and
// CommandArgs on native Windows.
func RunWindowsSandboxHelper(args []string, stdin *os.File, stdout *os.File, stderr *os.File) int {
	if len(args) < 3 || args[1] != "--" {
		fmt.Fprintln(stderr, "usage: reasonix "+WindowsHelperCommand+" <payload> -- <command> [args...]")
		return 2
	}
	payload, err := decodeWindowsSandboxPayload(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "invalid windows sandbox payload:", err)
		return 2
	}
	child := args[2:]
	if len(child) == 0 || strings.TrimSpace(child[0]) == "" {
		fmt.Fprintln(stderr, "windows sandbox command is required")
		return 2
	}
	result, err := winsandbox.Run(convertWindowsSandboxSpec(payload.Spec, payload.Writable), child, winsandbox.RunOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, WindowsSandboxFailureMarker(args[0]), "windows sandbox:", err)
		return 126
	}
	return result.ExitCode
}

func convertWindowsSandboxSpec(spec Spec, writable bool) winsandbox.Spec {
	return winsandbox.Spec{
		WritableRoots:   append([]string(nil), spec.WriteRoots...),
		ForbidReadRoots: append([]string(nil), spec.ForbidReadRoots...),
		Network:         spec.Network,
		Writable:        writable,
		TempPrefix:      "reasonix-sandbox-",
		LockWait:        spec.WindowsLockWait,
	}
}
