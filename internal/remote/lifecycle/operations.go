package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
)

func (m *SystemdManager) Install(ctx context.Context) (ActionResult, error) {
	return m.withMutationLock(ctx, m.installLocked)
}

func (m *SystemdManager) installLocked(ctx context.Context) (ActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := m.guardUnitProfile(false); err != nil {
		return ActionResult{}, err
	}
	if _, err := m.syncManagedBinary(ctx); err != nil {
		return ActionResult{}, fmt.Errorf("synchronize managed binary: %w", err)
	}
	if err := m.writeUnit(); err != nil {
		return ActionResult{}, err
	}
	if _, err := m.systemctl(ctx, "daemon-reload"); err != nil {
		return ActionResult{}, err
	}
	_, loadedDefinitionExact, loadedDetail, err := m.unitState(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	if !loadedDefinitionExact {
		return ActionResult{}, fmt.Errorf("systemd did not load the exact managed unit after daemon-reload (%s): %w", loadedDetail, ErrUnsafeArtifact)
	}
	if _, err := m.systemctl(ctx, "enable", "--now", service.UnitName); err != nil {
		return ActionResult{}, err
	}
	result := ActionResult{Changed: true}
	if m.probe != nil {
		probe, probeErr := m.probe.Probe(ctx)
		if probeErr == nil && protocol.CompareBuildID(m.cliBuildID, probe.BuildID) != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"active_daemon_requires_restart", SeverityWarning,
				"Install synchronized the managed binary, but the already-running daemon still has a different Build ID.",
				"Run `reasonix remote restart` to activate the managed binary.",
			))
		}
	}
	return result, nil
}

func (m *SystemdManager) Start(ctx context.Context) (ActionResult, error) {
	return m.withMutationLock(ctx, m.startLocked)
}

func (m *SystemdManager) startLocked(ctx context.Context) (ActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	record, err := m.guardUnitProfile(true)
	if err != nil {
		return ActionResult{}, err
	}
	if err := m.requireExactUnit(record); err != nil {
		return ActionResult{}, err
	}
	active, loadedDefinitionExact, loadedDetail, err := m.unitState(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	if active {
		if !loadedDefinitionExact {
			return ActionResult{}, fmt.Errorf("systemd loaded unit does not match the exact managed definition (%s); run install from the matching profile: %w", loadedDetail, ErrUnsafeArtifact)
		}
		result := ActionResult{Changed: false}
		installed, installedErr := m.inspectInstalledIdentity()
		cli := IdentityStatus{Present: true, Valid: true, BuildID: &m.cliBuildID}
		if installedErr != nil || compareIdentity(cli, installed) != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"active_daemon_requires_restart", SeverityWarning,
				"The daemon is already active and the current CLI does not match the trusted managed binary; Start did not replace it.",
				"Run `reasonix remote restart` to synchronize and activate the current CLI.",
			))
		} else {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"already_active", SeverityInfo,
				"The Reasonix Remote daemon is already active; Start made no changes.",
				"",
			))
		}
		if m.probe != nil {
			probe, probeErr := m.probe.Probe(ctx)
			if probeErr != nil {
				result.Diagnostics = append(result.Diagnostics, diagnostic(
					"active_daemon_identity_unavailable", SeverityWarning,
					"The active daemon Build ID could not be read: "+probeErr.Error(),
					"Run `reasonix remote doctor`; Start will not repair an active daemon.",
				))
			} else {
				daemon := IdentityStatus{Present: true, Valid: probe.BuildID.Validate() == nil, BuildID: &probe.BuildID}
				if compareIdentity(installed, daemon) != nil {
					result.Diagnostics = append(result.Diagnostics, diagnostic(
						"active_daemon_requires_restart", SeverityWarning,
						"The active daemon Build ID differs from the trusted managed binary; Start left it running.",
						"Run `reasonix remote restart` to activate the managed binary.",
					))
				}
			}
		}
		return result, nil
	}
	if _, err := m.syncManagedBinary(ctx); err != nil {
		return ActionResult{}, fmt.Errorf("synchronize managed binary: %w", err)
	}
	if _, err := m.systemctl(ctx, "daemon-reload"); err != nil {
		return ActionResult{}, err
	}
	_, loadedDefinitionExact, loadedDetail, err = m.unitState(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	if !loadedDefinitionExact {
		return ActionResult{}, fmt.Errorf("systemd did not load the exact managed unit after daemon-reload (%s): %w", loadedDetail, ErrUnsafeArtifact)
	}
	if _, err := m.systemctl(ctx, "start", service.UnitName); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Changed: true}, nil
}

func (m *SystemdManager) Stop(ctx context.Context) (ActionResult, error) {
	return m.withMutationLock(ctx, m.stopLocked)
}

func (m *SystemdManager) stopLocked(ctx context.Context) (ActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	record, err := m.guardUnitProfile(true)
	if err != nil {
		return ActionResult{}, err
	}
	if err := m.requireExactUnit(record); err != nil {
		return ActionResult{}, err
	}
	_, loadedDefinitionExact, loadedDetail, err := m.unitState(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	if !loadedDefinitionExact {
		return ActionResult{}, fmt.Errorf("refuse to stop a systemd unit whose loaded definition is not exact (%s); inspect the unit and drop-ins manually: %w", loadedDetail, ErrUnsafeArtifact)
	}
	if _, err := m.systemctl(ctx, "stop", service.UnitName); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Changed: true}, nil
}

func (m *SystemdManager) Restart(ctx context.Context) (ActionResult, error) {
	return m.withMutationLock(ctx, m.restartLocked)
}

func (m *SystemdManager) restartLocked(ctx context.Context) (ActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	record, err := m.guardUnitProfile(true)
	if err != nil {
		return ActionResult{}, err
	}
	if err := m.requireExactUnit(record); err != nil {
		return ActionResult{}, err
	}
	// The running service is deliberately untouched until the copy, fsync,
	// manifest, atomic rename, and post-write identity checks all succeed.
	if _, err := m.syncManagedBinary(ctx); err != nil {
		return ActionResult{}, fmt.Errorf("synchronize managed binary: %w", err)
	}
	if _, err := m.systemctl(ctx, "daemon-reload"); err != nil {
		return ActionResult{}, err
	}
	_, loadedDefinitionExact, loadedDetail, err := m.unitState(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	if !loadedDefinitionExact {
		return ActionResult{}, fmt.Errorf("systemd did not load the exact managed unit after daemon-reload (%s): %w", loadedDetail, ErrUnsafeArtifact)
	}
	if _, err := m.systemctl(ctx, "restart", service.UnitName); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Changed: true}, nil
}

func (m *SystemdManager) Logs(ctx context.Context, options LogsOptions) (LogsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Lines < 0 || options.Lines > 1_000_000 {
		return LogsResult{}, errors.New("log line count must be between 0 and 1000000")
	}
	if err := validateDirectArgument("since", options.Since); err != nil {
		return LogsResult{}, err
	}
	if _, err := m.guardUnitProfile(true); err != nil {
		return LogsResult{}, err
	}
	args := []string{"--user", "--unit", service.UnitName, "--no-pager"}
	if options.Lines > 0 {
		args = append(args, "--lines", strconv.Itoa(options.Lines))
	}
	if strings.TrimSpace(options.Since) != "" {
		args = append(args, "--since", options.Since)
	}
	output, err := m.run(ctx, "journalctl", args...)
	if err != nil {
		return LogsResult{}, err
	}
	return LogsResult{Output: output.Stdout}, nil
}

func (m *SystemdManager) Uninstall(ctx context.Context) (ActionResult, error) {
	return m.withMutationLock(ctx, m.uninstallLocked)
}

func (m *SystemdManager) uninstallLocked(ctx context.Context) (ActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	record, err := m.guardUnitProfile(false)
	if err != nil {
		return ActionResult{}, err
	}
	if err := m.guardManagedProfileForRemoval(record.File.Exists); err != nil {
		return ActionResult{}, err
	}
	changed := false
	if record.File.Exists {
		if err := m.requireExactUnit(record); err != nil {
			return ActionResult{}, err
		}
		unitRemoval, err := m.prepareUnitRemoval(record.Identity)
		if err != nil {
			return ActionResult{}, err
		}
		defer unitRemoval.Close()
		_, loadedDefinitionExact, loadedDetail, err := m.unitState(ctx)
		if err != nil {
			return ActionResult{}, err
		}
		if !loadedDefinitionExact {
			return ActionResult{}, fmt.Errorf("refuse to uninstall a systemd unit whose loaded definition is not exact (%s); inspect the unit and drop-ins manually: %w", loadedDetail, ErrUnsafeArtifact)
		}
		if _, err := m.systemctl(ctx, "stop", service.UnitName); err != nil {
			return ActionResult{}, err
		}
		if _, err := m.systemctl(ctx, "disable", service.UnitName); err != nil {
			return ActionResult{}, err
		}
		if err := unitRemoval.Remove(); err != nil {
			return ActionResult{}, fmt.Errorf("remove systemd user unit: %w", err)
		}
		if _, err := m.systemctl(ctx, "daemon-reload"); err != nil {
			return ActionResult{}, err
		}
		changed = true
	}
	removed, err := m.removeManagedArtifacts()
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Changed: changed || removed}, nil
}

func (m *SystemdManager) unitState(ctx context.Context) (active bool, loadedDefinitionExact bool, detail string, err error) {
	args := []string{"show", service.UnitName, "--no-pager"}
	for _, property := range systemdShowProperties {
		args = append(args, "--property="+property)
	}
	output, err := m.systemctl(ctx, args...)
	if err != nil {
		return false, false, "", err
	}
	properties := parseProperties(output.Stdout)
	loadedDefinitionExact, detail = m.loadedDefinitionExact(properties)
	return properties["ActiveState"] == "active", loadedDefinitionExact, detail, nil
}

func (m *SystemdManager) requireExactUnit(record unitRecord) error {
	expectedUnit, err := m.renderUnit()
	if err != nil {
		return err
	}
	if !bytes.Equal(record.Contents, expectedUnit) {
		return fmt.Errorf("unit differs from the exact managed service definition; run install from the matching profile: %w", ErrUnsafeArtifact)
	}
	expected, err := m.expectedExecStartLine()
	if err != nil {
		return err
	}
	if record.ExecStartLine != expected {
		return fmt.Errorf("unit ExecStart is not the exact managed binary command; run install from the matching profile: %w", ErrUnsafeArtifact)
	}
	return nil
}

func (m *SystemdManager) guardManagedProfileForRemoval(unitPresent bool) error {
	if err := m.validateManagedRemovalBoundary(); err != nil {
		return err
	}
	manifest := inspectFile(m.manifestPath, m.uid)
	binary := inspectFile(m.binaryPath, m.uid)
	if !manifest.Exists && !binary.Exists {
		return nil
	}
	if manifest.Exists {
		identity, err := m.inspectInstalledIdentity()
		if err == nil {
			if identity.Profile == nil || !sameInstallProfile(*identity.Profile, m.profile) {
				return ErrProfileMismatch
			}
			return nil
		}
		if errors.Is(err, ErrProfileMismatch) {
			return err
		}
		// A matching trusted unit is sufficient to remove corrupt managed files,
		// but an orphaned corrupt manifest requires manual inspection.
		if unitPresent && secureRegularData(manifest) && (!binary.Exists || secureRegularExecutable(binary)) {
			return nil
		}
		return fmt.Errorf("cannot prove managed artifacts belong to the current install profile: %w", err)
	}
	if binary.Exists && (!unitPresent || !secureRegularExecutable(binary)) {
		return fmt.Errorf("orphaned managed binary has no trusted profile manifest: %w", ErrUnsafeArtifact)
	}
	return nil
}

func (m *SystemdManager) validateManagedRemovalBoundary() error {
	root := inspectFile(m.managedRoot, m.uid)
	if !root.Exists {
		return nil
	}
	if err := validateTrustedDirectory(m.managedRoot, m.uid, true, exactMode(0o700)); err != nil {
		return fmt.Errorf("managed root removal boundary: %w", err)
	}
	bin := inspectFile(m.managedDir, m.uid)
	if !bin.Exists {
		return nil
	}
	if err := validateTrustedDirectory(m.managedDir, m.uid, true, exactMode(0o700)); err != nil {
		return fmt.Errorf("managed bin removal boundary: %w", err)
	}
	return nil
}

func validateDirectArgument(label, value string) error {
	for _, char := range value {
		if unicode.IsControl(char) {
			return fmt.Errorf("%s contains a control character", label)
		}
	}
	return nil
}
