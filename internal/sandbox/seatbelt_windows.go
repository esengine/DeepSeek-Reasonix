//go:build windows

package sandbox

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"reasonix/internal/winsandbox"
)

type windowsSandboxPayload struct {
	Version  int  `json:"version"`
	Spec     Spec `json:"spec"`
	Writable bool `json:"writable"`
}

const windowsSandboxPayloadVersion = 1

// Command wraps the shell invocation in Reasonix's hidden Windows sandbox
// helper. The helper applies the native Windows sandbox backend and a Job
// Object before starting the requested command.
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	// Shells must remain able to read ordinary user files, so they use the
	// restricted-token lane in every constrained preset. Read-only receives no
	// workspace write ACL; workspace access receives only its canonical roots.
	return windowsSandboxCommand(spec, sh.argv(command), true)
}

// CommandArgs is like Command but accepts the command as raw argv instead of a
// shell command string.
func CommandArgs(spec Spec, args []string) ([]string, bool) {
	return windowsSandboxCommand(spec, args, spec.DirectWrites)
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
	payload.Version = windowsSandboxPayloadVersion
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
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return payload, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return payload, fmt.Errorf("multiple JSON values")
		}
		return payload, err
	}
	if payload.Version != windowsSandboxPayloadVersion {
		return payload, fmt.Errorf("unsupported payload version %d", payload.Version)
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
	roots := spec.WriteRoots
	if spec.ReadOnly {
		roots = spec.AppContainerWriteRoots
	}
	readableRoots := append([]string(nil), spec.ReadRoots...)
	return winsandbox.Spec{
		ReadableRoots:       readableRoots,
		WritableRoots:       append([]string(nil), roots...),
		ForbidReadRoots:     append([]string(nil), spec.ForbidReadRoots...),
		ProtectedWriteRoots: append([]string(nil), spec.ProtectedWriteRoots...),
		Network:             spec.Network,
		Writable:            writable,
		ReadOnly:            spec.ReadOnly,
		TempDir:             spec.SessionTemp,
		TempPrefix:          "reasonix-sandbox-",
		LockWait:            spec.WindowsLockWait,
	}
}
