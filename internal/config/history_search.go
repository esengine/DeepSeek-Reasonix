package config

// HistorySearchMaxMB returns the user-global history_search.max_mb setting
// (0 when unset or unreadable). It decodes only that table: adding a typed
// field to Config would push ratcheted config.go/render.go over their
// repolint budgets, and the setting is consumed by internal/historycatalog
// only (#8717). A settings-UI save may drop the manually written section.
func HistorySearchMaxMB() int {
	path := userConfigLoadPath()
	if path == "" {
		return 0
	}
	var partial struct {
		HistorySearch struct {
			MaxMB int `toml:"max_mb"`
		} `toml:"history_search"`
	}
	if _, err := decodeTOMLFile(path, &partial); err != nil {
		return 0
	}
	return partial.HistorySearch.MaxMB
}
