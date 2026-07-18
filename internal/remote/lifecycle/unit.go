package lifecycle

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const unitProfilePrefix = "# X-Reasonix-Install-Profile="

type unitRecord struct {
	Contents      []byte
	Profile       InstallProfile
	ExecStartLine string
	File          FileStatus
	Identity      fileIdentity
}

func (m *SystemdManager) renderUnit() ([]byte, error) {
	profileJSON, err := json.Marshal(m.profile)
	if err != nil {
		return nil, fmt.Errorf("encode install profile: %w", err)
	}
	executable, err := systemdQuote(m.binaryPath)
	if err != nil {
		return nil, fmt.Errorf("encode managed binary path: %w", err)
	}
	homeEnvironment, err := systemdEnvironmentQuote("REASONIX_HOME=" + m.profile.ReasonixHome)
	if err != nil {
		return nil, fmt.Errorf("encode Reasonix Home environment: %w", err)
	}
	profileEnvironment, err := systemdEnvironmentQuote("REASONIX_REMOTE_INSTALL_PROFILE=" + m.profile.ID)
	if err != nil {
		return nil, fmt.Errorf("encode install profile environment: %w", err)
	}
	profile := base64.RawURLEncoding.EncodeToString(profileJSON)
	contents := strings.Join([]string{
		"# Managed by Reasonix Remote. Do not edit.",
		unitProfilePrefix + profile,
		"[Unit]",
		"Description=Reasonix Remote Host",
		"",
		"[Service]",
		"Type=simple",
		"Environment=" + homeEnvironment,
		"Environment=" + profileEnvironment,
		"ExecStart=" + executable + " remote serve",
		"Restart=on-failure",
		"UMask=0077",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
	return []byte(contents), nil
}

func systemdQuote(value string) (string, error) {
	return systemdQuotedValue(value, true)
}

// Environment= performs specifier expansion but does not perform the
// ExecStart $VAR/${VAR} word expansion. Preserve literal dollars in the
// recorded Reasonix Home while continuing to escape percent specifiers.
func systemdEnvironmentQuote(value string) (string, error) {
	return systemdQuotedValue(value, false)
}

func systemdQuotedValue(value string, escapeDollar bool) (string, error) {
	if value == "" {
		return `""`, nil
	}
	var encoded strings.Builder
	encoded.Grow(len(value) + 2)
	encoded.WriteByte('"')
	for _, char := range value {
		if unicode.IsControl(char) {
			return "", errors.New("systemd argument contains a control character")
		}
		switch char {
		case '\\', '"':
			encoded.WriteByte('\\')
			encoded.WriteRune(char)
		case '%':
			// Percent specifiers are expanded by systemd even inside quotes.
			encoded.WriteString("%%")
		case '$':
			if escapeDollar {
				// ExecStart expands $VAR and ${VAR}; $$ is the literal-dollar
				// escape and keeps the absolute managed path fixed.
				encoded.WriteString("$$")
			} else {
				encoded.WriteRune(char)
			}
		default:
			encoded.WriteRune(char)
		}
	}
	encoded.WriteByte('"')
	return encoded.String(), nil
}

func (m *SystemdManager) expectedExecStartLine() (string, error) {
	unit, err := m.renderUnit()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(unit), "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			return line, nil
		}
	}
	return "", errors.New("rendered unit is missing ExecStart")
}

func (m *SystemdManager) readUnit() (unitRecord, error) {
	contents, status, identity, err := m.readTrustedUnitFile(1 << 20)
	record := unitRecord{File: status, Identity: identity}
	if errors.Is(err, os.ErrNotExist) {
		return record, ErrNotInstalled
	}
	if err != nil {
		return record, fmt.Errorf("read systemd user unit: %w", err)
	}
	record.Contents = contents

	var profileEncoded string
	profileSeen := false
	var execStart string
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, unitProfilePrefix) {
			if profileSeen {
				return record, fmt.Errorf("systemd user unit has duplicate install profiles: %w", ErrUnsafeArtifact)
			}
			profileSeen = true
			profileEncoded = strings.TrimPrefix(line, unitProfilePrefix)
		}
		if strings.HasPrefix(line, "ExecStart=") {
			if execStart != "" {
				return record, fmt.Errorf("systemd user unit has duplicate ExecStart directives: %w", ErrUnsafeArtifact)
			}
			execStart = line
		}
	}
	if err := scanner.Err(); err != nil {
		return record, fmt.Errorf("scan systemd user unit: %w", err)
	}
	if !profileSeen || profileEncoded == "" {
		return record, fmt.Errorf("systemd user unit has no Reasonix install profile: %w", ErrUnsafeArtifact)
	}
	profileJSON, err := base64.RawURLEncoding.DecodeString(profileEncoded)
	if err != nil {
		return record, fmt.Errorf("decode systemd install profile: %w", ErrUnsafeArtifact)
	}
	decoder := json.NewDecoder(bytes.NewReader(profileJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record.Profile); err != nil {
		return record, fmt.Errorf("decode systemd install profile: %w", ErrUnsafeArtifact)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return record, fmt.Errorf("decode systemd install profile: %w", ErrUnsafeArtifact)
	}
	canonicalHome, canonicalErr := cleanAbsoluteHome(record.Profile.ReasonixHome)
	if canonicalErr != nil || canonicalHome != record.Profile.ReasonixHome || record.Profile.ID != profileID(record.Profile.ReasonixHome) {
		return record, fmt.Errorf("systemd install profile is invalid: %w", ErrUnsafeArtifact)
	}
	record.ExecStartLine = execStart
	if execStart == "" {
		return record, fmt.Errorf("systemd user unit has no ExecStart directive: %w", ErrUnsafeArtifact)
	}
	return record, nil
}

func (m *SystemdManager) guardUnitProfile(required bool) (unitRecord, error) {
	record, err := m.readUnit()
	if errors.Is(err, ErrNotInstalled) && !required {
		return record, nil
	}
	if err != nil {
		return record, err
	}
	if !sameInstallProfile(record.Profile, m.profile) {
		return record, fmt.Errorf("unit is installed for Reasonix Home %q (%s), current profile is %q (%s); use the original profile or uninstall it first: %w",
			record.Profile.ReasonixHome, record.Profile.ID, m.profile.ReasonixHome, m.profile.ID, ErrProfileMismatch)
	}
	return record, nil
}

func (m *SystemdManager) writeUnit() error {
	contents, err := m.renderUnit()
	if err != nil {
		return err
	}
	parent := filepath.Dir(m.unitPath)
	if err := ensureTrustedDirectory(parent, m.uid, true, nil); err != nil {
		return fmt.Errorf("systemd user unit directory: %w", err)
	}
	existing := inspectFile(m.unitPath, m.uid)
	if existing.Exists && !secureRegularData(existing) {
		return fmt.Errorf("refuse to replace unsafe systemd user unit: %w", ErrUnsafeArtifact)
	}
	if err := atomicWriteFile(m.unitPath, contents, 0o600); err != nil {
		return fmt.Errorf("write systemd user unit: %w", err)
	}
	record, err := m.readUnit()
	if err != nil {
		return fmt.Errorf("verify systemd user unit: %w", err)
	}
	if !sameInstallProfile(record.Profile, m.profile) || !bytes.Equal(record.Contents, contents) {
		return fmt.Errorf("systemd user unit post-write verification mismatch: %w", ErrUnsafeArtifact)
	}
	expectedExecStart, err := m.expectedExecStartLine()
	if err != nil {
		return err
	}
	if record.ExecStartLine != expectedExecStart {
		return fmt.Errorf("systemd user unit ExecStart mismatch: %w", ErrUnsafeArtifact)
	}
	return nil
}
