package config

import (
	"fmt"
	"os"

	"reasonix/internal/base/fileutil"
)

func (c *Config) userConfigRoots(path string) (Roots, bool) {
	target, err := fileutil.CanonicalWritePath(path)
	if err != nil {
		return Roots{}, false
	}
	for _, roots := range []Roots{c.roots, processRoots()} {
		for _, candidate := range roots.userConfigCandidatePaths() {
			canonical, err := fileutil.CanonicalWritePath(candidate)
			if err == nil && samePath(target, canonical) {
				return roots, true
			}
		}
	}
	return Roots{}, false
}

func (c *Config) renderScopeForPath(path string) RenderScope {
	if _, user := c.userConfigRoots(path); user {
		return RenderScopeUser
	}
	return renderScopeForPath(path)
}

func (c *Config) prepareFirstUserCollaboration(logicalPath, resolved string) error {
	if c.Agent.Collaboration == unavailableInstallationCollaboration {
		return fmt.Errorf("cannot save user configuration with unsupported installation defaults; original metadata retained")
	}
	if c.Agent.Collaboration != "" {
		return nil
	}
	if _, err := os.Lstat(resolved); !os.IsNotExist(err) {
		return nil
	}
	roots, user := c.userConfigRoots(logicalPath)
	if !user {
		return nil
	}
	mode, err := roots.installationCollaboration(true)
	if err != nil {
		return err
	}
	if mode == "auto" {
		c.Agent.Collaboration = "auto"
	}
	return nil
}
