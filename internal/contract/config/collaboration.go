package config

import (
	"fmt"
	"slices"
	"strings"
)

func (c *Config) CollaborationMode() (string, error) {
	if c == nil {
		return "legacy", nil
	}
	mode := strings.ToLower(strings.TrimSpace(c.Agent.Collaboration))
	switch mode {
	case "", unavailableInstallationCollaboration:
		return "legacy", nil
	case "legacy", "auto", "shared", "isolated":
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported agent.collaboration %q", c.Agent.Collaboration)
	}
}

func (c *Config) AutoIntegrationEnabled() bool {
	return c == nil || c.Agent.AutoIntegrate == nil || *c.Agent.AutoIntegrate
}

func (c *Config) SetCollaborationMode(value string) error {
	copy := *c
	copy.Agent.Collaboration = strings.ToLower(strings.TrimSpace(value))
	if copy.Agent.Collaboration == "" {
		copy.Agent.Collaboration = "legacy"
	}
	if _, err := copy.CollaborationMode(); err != nil {
		return err
	}
	c.Agent.Collaboration = copy.Agent.Collaboration
	return nil
}

func (c *Config) UsesMutationTransactions() bool {
	mode, err := c.CollaborationMode()
	return err == nil && mode != "legacy"
}

func (h heldScope) narrowCollaboration(c *Config) {
	if c.AutoIntegrationEnabled() != h.autoIntegrate {
		c.ignoreProject("agent.auto_integrate", "", ProjectUserOnly)
		value := h.autoIntegrate
		c.Agent.AutoIntegrate = &value
	}
	if c.Agent.VerificationRuntime != h.verificationRuntime {
		c.ignoreProject("agent.verification_runtime", "", ProjectUserOnly)
		c.Agent.VerificationRuntime = h.verificationRuntime
	}
	if !slices.Equal(c.Agent.IsolationCopyPaths, h.isolationCopyPaths) {
		c.ignoreProject("agent.isolation_copy_paths", "", ProjectUserOnly)
		c.Agent.IsolationCopyPaths = slices.Clone(h.isolationCopyPaths)
	}
	if c.Agent.Collaboration != h.collaboration {
		c.ignoreProject("agent.collaboration", c.Agent.Collaboration, ProjectUserOnly)
		c.Agent.Collaboration = h.collaboration
	}
}
