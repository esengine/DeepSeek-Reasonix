// Package lifecycle manages the explicit, user-owned Remote daemon lifecycle.
// It is deliberately separate from Host/daemon runtime code: Desktop and attach
// never call this package, and the platform-neutral Manager contract does not
// expose systemd details as business rules.
package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"reasonix/internal/remote/protocol"
)

const (
	ManifestFormat = "reasonix.remote.managed.v1"
	ManifestName   = "reasonix.manifest.json"
)

var (
	ErrUnsupportedPlatform = errors.New("Reasonix Remote lifecycle is unsupported on this platform")
	ErrNotInstalled        = errors.New("Reasonix Remote is not installed")
	ErrProfileMismatch     = errors.New("Reasonix Remote installation profile mismatch")
	ErrUnsafeArtifact      = errors.New("unsafe Reasonix Remote managed artifact")
	ErrRootUnsupported     = errors.New("Reasonix Remote daemon cannot be managed as root")
)

// Manager is the cross-platform lifecycle boundary. V1 provides a production
// Linux implementation; other platforms return ErrUnsupportedPlatform while
// retaining a compile-stable interface for future backends.
type Manager interface {
	Install(context.Context) (ActionResult, error)
	Start(context.Context) (ActionResult, error)
	Stop(context.Context) (ActionResult, error)
	Restart(context.Context) (ActionResult, error)
	Status(context.Context) (Status, error)
	Doctor(context.Context) (DoctorReport, error)
	Logs(context.Context, LogsOptions) (LogsResult, error)
	Uninstall(context.Context) (ActionResult, error)
}

// InstallProfile binds one systemd unit to the exact resolved Reasonix Home.
// ID is a stable SHA-256 identity of ReasonixHome, not a runtime token profile.
type InstallProfile struct {
	ReasonixHome string `json:"reasonixHome"`
	ID           string `json:"id"`
}

// ActionResult describes an explicit mutating lifecycle command. Diagnostics
// are informational; an error means the requested transition did not complete.
type ActionResult struct {
	Changed     bool         `json:"changed"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type DiagnosticSeverity string

const (
	SeverityInfo    DiagnosticSeverity = "info"
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityError   DiagnosticSeverity = "error"
)

type Diagnostic struct {
	Code       string             `json:"code"`
	Severity   DiagnosticSeverity `json:"severity"`
	Message    string             `json:"message"`
	Suggestion string             `json:"suggestion,omitempty"`
}

type IdentityStatus struct {
	Present bool              `json:"present"`
	Valid   bool              `json:"valid"`
	BuildID *protocol.BuildID `json:"buildId,omitempty"`
	Profile *InstallProfile   `json:"profile,omitempty"`
	SHA256  string            `json:"sha256,omitempty"`
	Size    int64             `json:"size,omitempty"`
	Error   string            `json:"error,omitempty"`
}

type FileStatus struct {
	Path         string `json:"path"`
	Exists       bool   `json:"exists"`
	Kind         string `json:"kind,omitempty"`
	Mode         uint32 `json:"mode,omitempty"`
	ModeText     string `json:"modeText,omitempty"`
	UID          int64  `json:"uid,omitempty"`
	OwnerKnown   bool   `json:"ownerKnown"`
	OwnerMatches bool   `json:"ownerMatches"`
	Symlink      bool   `json:"symlink"`
	Secure       bool   `json:"secure"`
	Error        string `json:"error,omitempty"`
}

type UnitStatus struct {
	Path                  string `json:"path"`
	Exists                bool   `json:"exists"`
	Secure                bool   `json:"secure"`
	ReasonixHome          string `json:"reasonixHome,omitempty"`
	InstallProfileID      string `json:"installProfileId,omitempty"`
	ProfileMatch          bool   `json:"profileMatch"`
	ManagerAvailable      bool   `json:"managerAvailable"`
	LoadState             string `json:"loadState,omitempty"`
	UnitFileState         string `json:"unitFileState,omitempty"`
	ActiveState           string `json:"activeState,omitempty"`
	SubState              string `json:"subState,omitempty"`
	RecentResult          string `json:"recentResult,omitempty"`
	FragmentPath          string `json:"fragmentPath,omitempty"`
	DropInPaths           string `json:"dropInPaths,omitempty"`
	LoadedEnvironment     string `json:"loadedEnvironment,omitempty"`
	LoadedUMask           string `json:"loadedUMask,omitempty"`
	LoadedRestart         string `json:"loadedRestart,omitempty"`
	LoadedType            string `json:"loadedType,omitempty"`
	NeedDaemonReload      string `json:"needDaemonReload,omitempty"`
	Transient             string `json:"transient,omitempty"`
	Enabled               bool   `json:"enabled"`
	Active                bool   `json:"active"`
	ExecStart             string `json:"execStart,omitempty"`
	ExecStartExact        bool   `json:"execStartExact"`
	ContentExact          bool   `json:"contentExact"`
	LoadedExecStart       string `json:"loadedExecStart,omitempty"`
	LoadedExecStartExact  bool   `json:"loadedExecStartExact"`
	LoadedDefinitionExact bool   `json:"loadedDefinitionExact"`
	Error                 string `json:"error,omitempty"`
}

type LingeringStatus struct {
	Known   bool   `json:"known"`
	Enabled bool   `json:"enabled"`
	Error   string `json:"error,omitempty"`
}

type Status struct {
	Platform         string            `json:"platform"`
	Profile          InstallProfile    `json:"profile"`
	CLIBuildID       protocol.BuildID  `json:"cliBuildId"`
	InstalledBuildID *protocol.BuildID `json:"installedBuildId,omitempty"`
	DaemonBuildID    *protocol.BuildID `json:"daemonBuildId,omitempty"`
	CLI              IdentityStatus    `json:"cli"`
	Installed        IdentityStatus    `json:"installed"`
	Daemon           IdentityStatus    `json:"daemon"`
	ManagedDir       FileStatus        `json:"managedDir"`
	ManagedBinary    FileStatus        `json:"managedBinary"`
	Manifest         FileStatus        `json:"manifest"`
	UnitFile         FileStatus        `json:"unitFile"`
	Socket           FileStatus        `json:"socket"`
	Unit             UnitStatus        `json:"unit"`
	Lingering        LingeringStatus   `json:"lingering"`
	Diagnostics      []Diagnostic      `json:"diagnostics"`
}

type CheckState string

const (
	CheckPass    CheckState = "pass"
	CheckWarning CheckState = "warning"
	CheckFail    CheckState = "fail"
	CheckUnknown CheckState = "unknown"
)

type DoctorCheck struct {
	Name       string     `json:"name"`
	State      CheckState `json:"state"`
	Detail     string     `json:"detail"`
	Suggestion string     `json:"suggestion,omitempty"`
}

type DoctorReport struct {
	Status  Status        `json:"status"`
	Healthy bool          `json:"healthy"`
	Checks  []DoctorCheck `json:"checks"`
}

type LogsOptions struct {
	Lines int
	Since string
}

type LogsResult struct {
	Output string `json:"output"`
}

type CommandOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// CommandRunner executes argv directly. Implementations must never invoke a
// shell; tests use it to prove exact systemctl/journalctl ordering.
type CommandRunner interface {
	Run(context.Context, string, ...string) (CommandOutput, error)
}

type DaemonProbeResult struct {
	BuildID protocol.BuildID
}

// DaemonProbe is the future Unix-socket identity seam. The lifecycle core can
// ship before the socket status request is wired, while tests and the later CLI
// integration still exercise exact daemon Build ID comparisons.
type DaemonProbe interface {
	Probe(context.Context) (DaemonProbeResult, error)
}

// CommandError retains stderr/exit status without losing errors.Is on the
// underlying process error.
type CommandError struct {
	Name   string
	Args   []string
	Output CommandOutput
	Err    error
}

func (e *CommandError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s command failed: %v", e.Name, e.Err)
}

func (e *CommandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
