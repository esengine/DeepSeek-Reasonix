package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
)

var systemdShowProperties = []string{
	"LoadState",
	"UnitFileState",
	"ActiveState",
	"SubState",
	"Result",
	"ExecStart",
	"FragmentPath",
	"DropInPaths",
	"Environment",
	"UMask",
	"Restart",
	"Type",
	"NeedDaemonReload",
	"Transient",
}

func (m *SystemdManager) Status(ctx context.Context) (Status, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	cliBuildID := m.cliBuildID
	status := Status{
		Platform:   runtime.GOOS,
		Profile:    m.profile,
		CLIBuildID: cliBuildID,
		CLI: IdentityStatus{
			Present: true,
			Valid:   true,
			BuildID: &cliBuildID,
		},
	}

	status.ManagedDir = inspectFile(m.managedDir, m.uid)
	status.ManagedDir.Secure = secureOwnedDirectory(status.ManagedDir, 0o700)
	status.ManagedBinary = inspectFile(m.binaryPath, m.uid)
	status.ManagedBinary.Secure = secureRegularExecutable(status.ManagedBinary)
	status.Manifest = inspectFile(m.manifestPath, m.uid)
	status.Manifest.Secure = secureRegularData(status.Manifest)
	status.UnitFile = inspectFile(m.unitPath, m.uid)
	status.UnitFile.Secure = secureRegularData(status.UnitFile)
	status.Socket = inspectFile(m.socketPath, m.uid)
	status.Socket.Secure = secureSocket(status.Socket)

	installed, installedErr := m.inspectInstalledIdentity()
	status.Installed = installed
	if installedErr != nil && !errors.Is(installedErr, ErrNotInstalled) {
		status.Diagnostics = append(status.Diagnostics, diagnostic(
			"installed_identity_invalid", SeverityError,
			"The managed binary identity cannot be trusted: "+installedErr.Error(),
			"Run `reasonix remote restart` only after correcting ownership, modes, and the managed install profile.",
		))
	}

	status.Unit = UnitStatus{Path: m.unitPath}
	unitRecord, unitErr := m.readUnit()
	status.Unit.Exists = unitRecord.File.Exists
	status.Unit.Secure = unitRecord.File.Secure
	if unitErr == nil {
		status.Unit.ReasonixHome = unitRecord.Profile.ReasonixHome
		status.Unit.InstallProfileID = unitRecord.Profile.ID
		status.Unit.ProfileMatch = sameInstallProfile(unitRecord.Profile, m.profile)
		status.Unit.ExecStart = unitRecord.ExecStartLine
		expectedUnit, renderErr := m.renderUnit()
		if renderErr == nil {
			expectedExecStart, execErr := m.expectedExecStartLine()
			if execErr != nil {
				status.Unit.Error = execErr.Error()
			} else {
				status.Unit.ExecStartExact = unitRecord.ExecStartLine == expectedExecStart
			}
			status.Unit.ContentExact = bytes.Equal(unitRecord.Contents, expectedUnit)
		} else {
			status.Unit.Error = renderErr.Error()
		}
		if !status.Unit.ProfileMatch {
			status.Diagnostics = append(status.Diagnostics, diagnostic(
				"install_profile_mismatch", SeverityError,
				fmt.Sprintf("The unit belongs to Reasonix Home %q, but this command resolved %q.", unitRecord.Profile.ReasonixHome, m.profile.ReasonixHome),
				"Use the original Reasonix Home profile or uninstall it before changing profiles.",
			))
		}
		if !status.Unit.ExecStartExact {
			status.Diagnostics = append(status.Diagnostics, diagnostic(
				"unit_exec_start_mismatch", SeverityError,
				"The unit ExecStart is not the exact managed Reasonix binary followed by `remote serve`.",
				"Run `reasonix remote install` from the matching install profile.",
			))
		}
		if !status.Unit.ContentExact {
			status.Diagnostics = append(status.Diagnostics, diagnostic(
				"unit_content_mismatch", SeverityError,
				"The unit differs from the exact Reasonix-managed service definition.",
				"Run `reasonix remote install` from the matching profile to rewrite and reload it.",
			))
		}
	} else if !errors.Is(unitErr, ErrNotInstalled) {
		status.Unit.Error = unitErr.Error()
		status.Diagnostics = append(status.Diagnostics, diagnostic(
			"unit_invalid", SeverityError,
			"The systemd user unit cannot be trusted: "+unitErr.Error(),
			"Inspect the unit and use the matching install profile; doctor will not repair it automatically.",
		))
	}

	showArgs := []string{"show", service.UnitName, "--no-pager"}
	for _, property := range systemdShowProperties {
		showArgs = append(showArgs, "--property="+property)
	}
	showOutput, showErr := m.systemctl(ctx, showArgs...)
	if showErr == nil {
		properties := parseProperties(showOutput.Stdout)
		status.Unit.ManagerAvailable = true
		status.Unit.LoadState = properties["LoadState"]
		status.Unit.UnitFileState = properties["UnitFileState"]
		status.Unit.ActiveState = properties["ActiveState"]
		status.Unit.SubState = properties["SubState"]
		status.Unit.RecentResult = properties["Result"]
		status.Unit.LoadedExecStart = properties["ExecStart"]
		status.Unit.FragmentPath = properties["FragmentPath"]
		status.Unit.DropInPaths = properties["DropInPaths"]
		status.Unit.LoadedEnvironment = properties["Environment"]
		status.Unit.LoadedUMask = properties["UMask"]
		status.Unit.LoadedRestart = properties["Restart"]
		status.Unit.LoadedType = properties["Type"]
		status.Unit.NeedDaemonReload = properties["NeedDaemonReload"]
		status.Unit.Transient = properties["Transient"]
		status.Unit.Enabled = enabledUnitState(status.Unit.UnitFileState)
		status.Unit.Active = status.Unit.ActiveState == "active"
		status.Unit.LoadedExecStartExact = m.loadedExecStartExact(status.Unit.LoadedExecStart)
		status.Unit.LoadedDefinitionExact, _ = m.loadedDefinitionExact(properties)
		if status.Unit.Exists && !status.Unit.LoadedDefinitionExact {
			_, detail := m.loadedDefinitionExact(properties)
			status.Diagnostics = append(status.Diagnostics, diagnostic(
				"loaded_unit_mismatch", SeverityError,
				"The systemd user manager has not loaded the exact managed unit: "+detail,
				"Run `reasonix remote install` from the matching profile to rewrite and reload it.",
			))
		}
	} else {
		if err := contextCommandError(ctx, showErr); err != nil {
			return status, err
		}
		status.Diagnostics = append(status.Diagnostics, diagnostic(
			"systemd_user_manager_unavailable", SeverityError,
			"The systemd user manager could not be queried: "+commandDetail(showErr),
			"Start a supported systemd user session and run `reasonix remote doctor` again.",
		))
	}

	lingerOutput, lingerErr := m.run(ctx, "loginctl", "show-user", uidText(m.uid), "--property=Linger", "--value")
	if lingerErr == nil {
		value := strings.TrimSpace(lingerOutput.Stdout)
		if value == "yes" || value == "no" {
			status.Lingering.Known = true
			status.Lingering.Enabled = value == "yes"
		} else {
			status.Lingering.Error = "unexpected loginctl Linger value " + fmt.Sprintf("%q", value)
		}
	} else {
		if err := contextCommandError(ctx, lingerErr); err != nil {
			return status, err
		}
		status.Lingering.Error = commandDetail(lingerErr)
	}

	if m.probe != nil {
		probe, probeErr := m.probe.Probe(ctx)
		if probeErr == nil {
			status.Daemon.Present = true
			status.Daemon.BuildID = &probe.BuildID
			if err := probe.BuildID.Validate(); err != nil {
				status.Daemon.Error = err.Error()
			} else {
				status.Daemon.Valid = true
			}
		} else {
			if err := contextCommandError(ctx, probeErr); err != nil {
				return status, err
			}
			status.Daemon.Error = probeErr.Error()
		}
	}

	status.Diagnostics = append(status.Diagnostics, identityDiagnostics(status)...)
	if status.Installed.Valid && status.Installed.BuildID != nil {
		installedBuildID := *status.Installed.BuildID
		status.InstalledBuildID = &installedBuildID
	}
	if status.Daemon.Valid && status.Daemon.BuildID != nil {
		daemonBuildID := *status.Daemon.BuildID
		status.DaemonBuildID = &daemonBuildID
	}
	if status.Lingering.Known && !status.Lingering.Enabled {
		status.Diagnostics = append(status.Diagnostics, diagnostic(
			"lingering_disabled", SeverityWarning,
			"systemd user lingering is disabled; the daemon may stop after the last login session ends.",
			"If boot-without-login is required, enable lingering according to the Host administrator policy.",
		))
	}
	return status, nil
}

func (m *SystemdManager) Doctor(ctx context.Context) (DoctorReport, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	report := DoctorReport{Status: status, Healthy: true}
	add := func(name string, state CheckState, detail, suggestion string) {
		report.Checks = append(report.Checks, DoctorCheck{Name: name, State: state, Detail: detail, Suggestion: suggestion})
		if state == CheckFail {
			report.Healthy = false
		}
	}
	if status.Unit.ManagerAvailable {
		add("systemd-user-manager", CheckPass, "systemd user manager is queryable", "")
	} else {
		add("systemd-user-manager", CheckFail, "systemd user manager is unavailable", "Use a supported systemd user session.")
	}
	if status.Installed.Valid {
		add("installed-identity", CheckPass, "managed binary matches its Build ID manifest", "")
	} else {
		add("installed-identity", CheckFail, emptyFallback(status.Installed.Error, "managed binary is not installed or valid"), "Run `reasonix remote install` from the intended profile after resolving unsafe artifacts.")
	}
	if status.ManagedDir.Secure {
		add("managed-directory", CheckPass, "managed binary directory is owned by the current user with mode 0700", "")
	} else {
		add("managed-directory", CheckFail, "managed binary directory is absent or unsafe", "Inspect ownership, symlinks, and mode before installing.")
	}
	if status.Unit.Exists && status.Unit.Secure && status.Unit.ProfileMatch && status.Unit.ExecStartExact && status.Unit.ContentExact {
		add("unit-file", CheckPass, "unit profile and ExecStart match the managed installation", "")
	} else {
		add("unit-file", CheckFail, emptyFallback(status.Unit.Error, "unit is absent, unsafe, or belongs to another install profile"), "Use the original profile or uninstall before changing Reasonix Home.")
	}
	if status.Unit.Enabled {
		add("unit-enabled", CheckPass, "unit is enabled", "")
	} else {
		add("unit-enabled", CheckFail, "unit is not enabled", "Run `reasonix remote install` from the matching profile.")
	}
	if status.Unit.Active {
		add("unit-active", CheckPass, "daemon unit is active ("+status.Unit.SubState+")", "")
	} else {
		add("unit-active", CheckFail, "daemon unit is not active; last result is "+emptyFallback(status.Unit.RecentResult, "unknown"), "Run `reasonix remote start` after reviewing the service logs.")
	}
	if status.Unit.LoadedDefinitionExact {
		add("loaded-unit-definition", CheckPass, "systemd loaded the exact managed command, environment, UMask, restart policy, and no drop-ins", "")
	} else {
		add("loaded-unit-definition", CheckFail, "systemd loaded state does not match the exact managed unit", "Run `reasonix remote install` from the matching profile to rewrite and reload the unit.")
	}
	if status.Socket.Secure {
		add("daemon-socket", CheckPass, "daemon socket is owned by the current user with mode 0600", "")
	} else if status.Unit.Active {
		add("daemon-socket", CheckFail, "active service has no trusted 0600 Unix socket", "Inspect `reasonix remote logs`; doctor does not recreate the socket.")
	} else {
		add("daemon-socket", CheckWarning, "daemon socket is absent while the unit is inactive", "Start the service when Remote access is needed.")
	}
	if compareIdentity(status.CLI, status.Installed) == nil {
		add("cli-installed-build", CheckPass, "CLI and installed Build IDs match", "")
	} else {
		add("cli-installed-build", CheckFail, "CLI and installed Build IDs do not match", "Run `reasonix remote restart` to synchronize and activate the current CLI.")
	}
	if compareIdentity(status.Installed, status.Daemon) == nil {
		add("installed-daemon-build", CheckPass, "installed and daemon Build IDs match", "")
	} else {
		add("installed-daemon-build", CheckFail, "installed and daemon Build IDs are unavailable or do not match", "Run `reasonix remote restart` after confirming the intended CLI version.")
	}
	if status.Lingering.Known {
		if status.Lingering.Enabled {
			add("lingering", CheckPass, "systemd user lingering is enabled", "")
		} else {
			add("lingering", CheckWarning, "systemd user lingering is disabled", "Enable it only if required and permitted by Host policy.")
		}
	} else {
		add("lingering", CheckUnknown, emptyFallback(status.Lingering.Error, "lingering state is unknown"), "Query Host login policy manually.")
	}
	return report, nil
}

func parseProperties(output string) map[string]string {
	properties := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key == "" {
			continue
		}
		if _, duplicate := properties[key]; duplicate {
			properties["__parse_error__"] = "duplicate systemd property " + key
			continue
		}
		properties[key] = value
	}
	return properties
}

func enabledUnitState(state string) bool {
	return state == "enabled" || state == "enabled-runtime" || state == "linked" || state == "linked-runtime"
}

func (m *SystemdManager) loadedExecStartExact(value string) bool {
	value = strings.TrimSpace(value)
	expected := m.binaryPath + " remote serve"
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") ||
		strings.Count(value, "ignore_errors=") != 1 || strings.Count(value, "argv[]=") != 1 ||
		strings.Contains(value, "} {") || strings.Contains(value, "} ; {") || strings.Contains(value, "};{") {
		return false
	}
	fields := make(map[string]string)
	for _, raw := range strings.Split(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}")), " ; ") {
		raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), ";"))
		if raw == "" {
			continue
		}
		key, fieldValue, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if ok {
			if _, duplicate := fields[key]; duplicate {
				return false
			}
			fields[key] = fieldValue
		}
	}
	return fields["path"] == m.binaryPath && fields["argv[]"] == expected && fields["ignore_errors"] == "no"
}

func (m *SystemdManager) loadedDefinitionExact(properties map[string]string) (bool, string) {
	if parseError := properties["__parse_error__"]; parseError != "" {
		return false, parseError
	}
	if properties["LoadState"] != "loaded" {
		return false, "LoadState=" + emptyFallback(properties["LoadState"], "unknown")
	}
	if properties["FragmentPath"] != m.unitPath {
		return false, fmt.Sprintf("FragmentPath=%q, expected %q", properties["FragmentPath"], m.unitPath)
	}
	if _, present := properties["DropInPaths"]; !present {
		return false, "DropInPaths property is missing"
	}
	if strings.TrimSpace(properties["DropInPaths"]) != "" {
		return false, "DropInPaths is not empty"
	}
	if !m.loadedExecStartExact(properties["ExecStart"]) {
		return false, "ExecStart does not match the managed binary command"
	}
	wantEnvironment := []string{
		"REASONIX_HOME=" + m.profile.ReasonixHome,
		"REASONIX_REMOTE_INSTALL_PROFILE=" + m.profile.ID,
	}
	gotEnvironment, err := parseSystemdWords(properties["Environment"])
	if err != nil {
		return false, "Environment cannot be decoded: " + err.Error()
	}
	sort.Strings(gotEnvironment)
	sort.Strings(wantEnvironment)
	if !equalStrings(gotEnvironment, wantEnvironment) {
		return false, fmt.Sprintf("Environment=%q does not match the install profile", properties["Environment"])
	}
	umask, err := strconv.ParseUint(strings.TrimSpace(properties["UMask"]), 8, 32)
	if err != nil || umask != 0o077 {
		return false, "UMask=" + emptyFallback(properties["UMask"], "unknown") + ", expected 0077"
	}
	if properties["Restart"] != "on-failure" {
		return false, "Restart=" + emptyFallback(properties["Restart"], "unknown") + ", expected on-failure"
	}
	if properties["Type"] != "simple" {
		return false, "Type=" + emptyFallback(properties["Type"], "unknown") + ", expected simple"
	}
	if properties["NeedDaemonReload"] != "no" {
		return false, "NeedDaemonReload=" + emptyFallback(properties["NeedDaemonReload"], "unknown") + ", expected no"
	}
	if properties["Transient"] != "no" {
		return false, "Transient=" + emptyFallback(properties["Transient"], "unknown") + ", expected no"
	}
	return true, ""
}

func parseSystemdWords(value string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	escaped := false
	flush := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	for _, char := range value {
		if escaped {
			word.WriteRune(char)
			inWord = true
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			inWord = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
			inWord = true
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			inWord = true
			continue
		}
		if char == ' ' || char == '\t' || char == '\n' {
			flush()
			continue
		}
		word.WriteRune(char)
		inWord = true
	}
	if escaped || quote != 0 {
		return nil, errors.New("unterminated escape or quote")
	}
	flush()
	return words, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func secureOwnedDirectory(status FileStatus, exactMode os.FileMode) bool {
	return status.Exists && status.Error == "" && status.Kind == "directory" && status.OwnerMatches && os.FileMode(status.Mode).Perm() == exactMode.Perm()
}

func secureSocket(status FileStatus) bool {
	return status.Exists && status.Error == "" && status.Kind == "socket" && status.OwnerMatches && os.FileMode(status.Mode).Perm() == 0o600
}

func identityDiagnostics(status Status) []Diagnostic {
	var diagnostics []Diagnostic
	if status.Installed.Valid {
		if err := compareIdentity(status.CLI, status.Installed); err != nil {
			diagnostics = append(diagnostics, diagnostic(
				"cli_installed_build_mismatch", SeverityWarning,
				"The current CLI Build ID differs from the managed binary: "+err.Error(),
				"Run `reasonix remote restart` to synchronize and activate this CLI.",
			))
		}
	}
	if status.Installed.Valid && status.Daemon.Present {
		if err := compareIdentity(status.Installed, status.Daemon); err != nil {
			diagnostics = append(diagnostics, diagnostic(
				"installed_daemon_build_mismatch", SeverityWarning,
				"The daemon Build ID differs from the managed binary: "+err.Error(),
				"Run `reasonix remote restart` to activate the verified managed binary.",
			))
		}
	}
	return diagnostics
}

func compareIdentity(expected, actual IdentityStatus) error {
	if !expected.Present || !expected.Valid || expected.BuildID == nil {
		return errors.New("expected Build ID is unavailable")
	}
	if !actual.Present || !actual.Valid || actual.BuildID == nil {
		return errors.New("actual Build ID is unavailable")
	}
	return protocol.CompareBuildID(*expected.BuildID, *actual.BuildID)
}

func contextCommandError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func commandDetail(err error) string {
	var commandErr *CommandError
	if errors.As(err, &commandErr) {
		if stderr := strings.TrimSpace(commandErr.Output.Stderr); stderr != "" {
			return stderr
		}
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func diagnostic(code string, severity DiagnosticSeverity, message, suggestion string) Diagnostic {
	return Diagnostic{Code: code, Severity: severity, Message: message, Suggestion: suggestion}
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
