// Package configsummary projects the Host's global Reasonix configuration into
// the frozen, read-only Remote V1 host/configSummary result. The projection is
// deliberately much smaller than config.Config: credentials, provider and MCP
// connection data, local paths, skill bodies, and diagnostics never enter the
// value that is serialized or hashed.
package configsummary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/remote/protocol"
)

const (
	userDisplayPath      = "<reasonix-home>/config.toml"
	workspaceDisplayPath = "<workspace>/reasonix.toml"
)

// Provider is the narrow daemon dependency for host/configSummary. Keeping the
// protocol result behind this interface lets daemon composition replace the
// read-only source in tests without exposing config.Config to the transport.
type Provider interface {
	Summary(context.Context) (protocol.HostConfigSummaryResult, error)
}

// Service reloads the Host-global config on every query. Host-side config edits
// therefore become visible without restarting the daemon, while the result
// remains a presentation-only snapshot with no writable config handle.
type Service struct {
	capabilities protocol.Capabilities
	read         func(context.Context) (sourceState, error)
}

var _ Provider = (*Service)(nil)

type sourceState struct {
	userConfigPresent    bool
	safeMode             bool
	memoryCompilerActive bool
}

// New constructs the production Host projection using only read-only config
// inspection. Capabilities are frozen at daemon construction and are included
// only as safe feature availability booleans.
func New(capabilities protocol.Capabilities) (*Service, error) {
	if err := capabilities.Validate(); err != nil {
		return nil, fmt.Errorf("config summary capabilities: %w", err)
	}
	return &Service{capabilities: capabilities, read: readProductionState}, nil
}

// Summary returns a deterministic safe projection. Its revision is a digest of
// exactly the fields returned to the peer; changing a credential, endpoint,
// command, raw path, or other hidden value cannot change the revision and turn
// it into a secret-correlating oracle.
func (s *Service) Summary(ctx context.Context) (protocol.HostConfigSummaryResult, error) {
	if s == nil || s.read == nil {
		return protocol.HostConfigSummaryResult{}, errors.New("config summary service is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return protocol.HostConfigSummaryResult{}, err
	}
	state, err := s.read(ctx)
	if err != nil {
		return protocol.HostConfigSummaryResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return protocol.HostConfigSummaryResult{}, err
	}

	result := safeProjection(s.capabilities, state)
	revision, err := projectionRevision(result)
	if err != nil {
		return protocol.HostConfigSummaryResult{}, err
	}
	result.Revision = revision
	return result, nil
}

func safeProjection(capabilities protocol.Capabilities, state sourceState) protocol.HostConfigSummaryResult {
	userActive := state.userConfigPresent && !state.safeMode
	workspaceActive := !state.safeMode
	memoryAvailable := capabilities.Features.Memory
	memoryCompilerAvailable := memoryAvailable && state.memoryCompilerActive
	researchAvailable := capabilities.Features.Research

	return protocol.HostConfigSummaryResult{
		EffectiveScopes: []protocol.EffectiveScope{
			{Name: "built-in", Active: true},
			{Name: "user", Active: userActive},
			{Name: "workspace", Active: workspaceActive},
		},
		DisplayPaths: []protocol.ConfigDisplayPath{
			{Scope: "user", DisplayPath: userDisplayPath},
			{Scope: "workspace", DisplayPath: workspaceDisplayPath},
		},
		FeatureStates: []protocol.FeatureState{
			{Feature: "memory", Available: memoryAvailable, Summary: availabilitySummary(memoryAvailable)},
			{Feature: "memoryCompiler", Available: memoryCompilerAvailable, Summary: memoryCompilerSummary(memoryAvailable, state.memoryCompilerActive)},
			{Feature: "research", Available: researchAvailable, Summary: availabilitySummary(researchAvailable)},
		},
		CLIHints: []protocol.CLIHint{
			{Label: "Configure Host", Command: "reasonix setup"},
			{Label: "Inspect Remote status", Command: "reasonix remote status"},
			{Label: "Diagnose Remote Host", Command: "reasonix remote doctor"},
		},
	}
}

func availabilitySummary(available bool) string {
	if available {
		return "available on this Host"
	}
	return "unavailable on this Host"
}

func memoryCompilerSummary(memoryAvailable, enabled bool) string {
	switch {
	case !memoryAvailable:
		return "memory is unavailable on this Host"
	case enabled:
		return "enabled by Host configuration"
	default:
		return "disabled by Host configuration"
	}
}

func projectionRevision(result protocol.HostConfigSummaryResult) (protocol.CatalogRevision, error) {
	// Revision must not recursively hash itself.
	result.Revision = ""
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode safe config summary projection: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return protocol.CatalogRevision("host_config_" + hex.EncodeToString(digest[:])), nil
}

func readProductionState(ctx context.Context) (sourceState, error) {
	if err := ctx.Err(); err != nil {
		return sourceState{}, err
	}
	selected, present, err := selectUserConfigPath()
	if err != nil {
		return sourceState{}, err
	}
	safeMode := config.SafeModeRequested()
	cfg := config.Default()
	if present && !safeMode {
		// This is the strict read-only config API: it performs no migrations or
		// writes. The returned config may contain secrets in memory, but only the
		// single normalized memory-compiler boolean is copied below.
		cfg, err = config.LoadForEditReadOnlyWithoutCredentialsStrict(selected)
		if err != nil {
			return sourceState{}, errors.New("read Host user configuration")
		}
	}
	if err := ctx.Err(); err != nil {
		return sourceState{}, err
	}
	return sourceStateFromConfig(cfg, present, safeMode), nil
}

func sourceStateFromConfig(cfg *config.Config, userConfigPresent, safeMode bool) sourceState {
	return sourceState{
		userConfigPresent:    userConfigPresent,
		safeMode:             safeMode,
		memoryCompilerActive: cfg.MemoryCompilerEnabled(),
	}
}

func selectUserConfigPath() (string, bool, error) {
	paths := make([]string, 0, 1+len(config.LegacyUserConfigPaths()))
	if path := strings.TrimSpace(config.UserConfigPath()); path != "" {
		paths = append(paths, path)
	}
	paths = append(paths, config.LegacyUserConfigPaths()...)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		switch {
		case err == nil && info.Mode().IsRegular():
			return path, true, nil
		case err == nil:
			return "", false, errors.New("Host user configuration is not a regular file")
		case os.IsNotExist(err):
			continue
		default:
			return "", false, errors.New("inspect Host user configuration")
		}
	}
	return "", false, nil
}
