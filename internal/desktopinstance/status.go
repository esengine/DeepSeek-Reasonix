// Package desktopinstance coordinates installed Desktop process ownership.
package desktopinstance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/installlayout"
)

const StatusLimit = 16 * 1024
const QuitRequest = "--reasonix-lifecycle-request=quit"

type Code string

const (
	UnknownOwner         Code = "unknown_owner"
	ConfirmationRequired Code = "confirmation_required"
	Cancelled            Code = "cancelled"
	ExitTimeout          Code = "exit_timeout"
	StartupFailed        Code = "startup_failed"
	OtherInstallation    Code = "other_installation"
)

type Error struct {
	Code   Code
	Detail string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Detail }

// ExitCode preserves a machine-readable distinction for silent installers.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var failure *Error
	if errors.As(err, &failure) {
		switch failure.Code {
		case Cancelled:
			return 1602
		case ConfirmationRequired, OtherInstallation, UnknownOwner, ExitTimeout:
			return 1618
		case StartupFailed:
			return 1603
		}
	}
	return 1
}

type Status struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Product         string `json:"product"`
	PID             uint32 `json:"pid"`
	Version         string `json:"version"`
	Generation      string `json:"generation"`
	HomeKey         string `json:"homeKey"`
	Lifecycle       string `json:"lifecycle"`
	Service         string `json:"service"`
	ServicePID      uint32 `json:"servicePID"`
	Visible         bool   `json:"visible"`
	RendererVersion string `json:"rendererVersion"`
	Healthy         bool   `json:"healthy"`
}

func DecodeStatus(data []byte, pid uint32) (Status, error) {
	var s Status
	if len(data) > StatusLimit {
		return s, errors.New("shell status exceeds limit")
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.SchemaVersion != 1 || s.Product != "com.reasonix.desktop" || s.PID != pid || s.Generation == "" || len(s.HomeKey) != 64 || s.Version == "" {
		return s, errors.New("unverified shell status identity")
	}
	if _, err := hex.DecodeString(s.HomeKey); err != nil {
		return s, err
	}
	switch s.Lifecycle {
	case "starting", "ready", "failed", "quitting", "done":
	default:
		return s, errors.New("unknown shell lifecycle")
	}
	switch s.Service {
	case "starting", "restarting", "ready", "failed", "exited":
	default:
		return s, errors.New("unknown service state")
	}
	return s, nil
}

func (s Status) Ready(version string) bool {
	return s.Lifecycle == "ready" && s.Service == "ready" && s.ServicePID != 0 && s.Visible && s.Healthy && s.RendererVersion == s.Version && (version == "" || s.Version == version)
}

func ProfileKey(profile string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.ReplaceAll(profile, "/", `\`))))
	return hex.EncodeToString(sum[:])
}

// ImageRole accepts only installed product paths, never a process-name match.
func ImageRole(root, image string) string {
	rel, err := filepath.Rel(root, image)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.ToLower(filepath.ToSlash(rel)), "/")
	if len(parts) >= 3 && parts[0] == "versions" {
		if installlayout.ValidateVersionName(parts[1]) != nil {
			return ""
		}
		parts = parts[2:]
	}
	switch strings.Join(parts, "/") {
	case "app/reasonix.exe":
		return "shell"
	case "reasonix-desktop.exe":
		return "service"
	case "app/resources/service/reasonix-desktop.exe":
		return "service"
	}
	return ""
}

func outcome(code Code, detail string, args ...any) error {
	return &Error{code, fmt.Sprintf(detail, args...)}
}
