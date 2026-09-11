package config

// RecoveryCopyAutoCleanupEnabled reports the user-global
// recovery_cleanup.auto_enabled setting (default true). It decodes only that
// table: adding a typed field to Config would push ratcheted
// config.go/render.go over their repolint budgets, and the setting is consumed
// by the desktop recovery-copy sweep only (#8525/#8750/#9109). A settings-UI
// save may drop the manually written section.
func RecoveryCopyAutoCleanupEnabled() bool {
	path := userConfigLoadPath()
	if path == "" {
		return true
	}
	var partial struct {
		RecoveryCleanup struct {
			AutoEnabled *bool `toml:"auto_enabled"`
		} `toml:"recovery_cleanup"`
	}
	if _, err := decodeTOMLFile(path, &partial); err != nil {
		return true
	}
	return partial.RecoveryCleanup.AutoEnabled == nil || *partial.RecoveryCleanup.AutoEnabled
}
