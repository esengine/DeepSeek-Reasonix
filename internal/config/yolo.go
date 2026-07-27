package config

import (
	"strings"

	"reasonix/internal/sandboxauth"
)

// ResolveYOLOPolicyConfig resolves user/project precedence while retaining the
// provenance needed to detect a project expansion from false to true.
func ResolveYOLOPolicyConfig(workspace string) sandboxauth.YOLOPolicyConfig {
	root, _ := canonicalCapabilityWorkspace(workspace)
	userValue, userDefined := loadYOLOSetting(userConfigLoadPath())
	projectValue, projectDefined := loadYOLOSetting(capabilityConfigPath(root))
	effective := false
	if userDefined {
		effective = userValue
	}
	if projectDefined {
		effective = projectValue
	}
	return sandboxauth.YOLOPolicyConfig{
		Workspace:        root,
		Effective:        effective,
		ProjectExpansion: projectDefined && projectValue && (!userDefined || !userValue),
	}
}

func loadYOLOSetting(path string) (bool, bool) {
	if strings.TrimSpace(path) == "" {
		return false, false
	}
	var value struct {
		Sandbox struct {
			YOLO *bool `toml:"yolo_auto_approve_capabilities"`
		} `toml:"sandbox"`
	}
	if _, err := decodeTOMLFile(path, &value); err != nil || value.Sandbox.YOLO == nil {
		return false, false
	}
	return *value.Sandbox.YOLO, true
}
